#!/usr/bin/env bash

set -Eeuo pipefail

readonly EXPECTED_HOST_IP="192.168.35.221"
readonly GIT_ROOT="/home/ecs-user/gitroot"
readonly WEB_ROOT="/home/ecs-user/webroot"
readonly SUPERVISOR_CONF_DIR="/home/ecs-user/.local/etc/supervisor/conf.d"
readonly RELEASE_ROOT="/home/ecs-user/releases"
readonly DEFAULT_BRANCH="dev"
readonly DEFAULT_KEEP_BACKUPS=3
readonly GITHUB_SSH_REWRITE='url.git@github.com:.insteadOf=https://github.com/'
readonly DEPLOY_GIT_SSH_COMMAND='ssh -i /home/ecs-user/.ssh/id_rsa -o IdentitiesOnly=yes -o BatchMode=yes'

action="deploy"
service=""
run_tests=true
dry_run=false
keep_backups=$DEFAULT_KEEP_BACKUPS
build_dir=""
backup_dir=""
target_dir=""
repository=""
branch="$DEFAULT_BRANCH"
after_copy_hook=""
deployed=false
declare -a selectors=()
declare -a processes=()
declare -a artifact_names=()
declare -a asset_directories=()
declare -a new_supervisor_configs=()

usage() {
	cat <<'EOF'
Usage:
  deploy-server.sh list
  deploy-server.sh processes <service>
  deploy-server.sh plan <service> <process> [process ...]
  deploy-server.sh deploy <service> <process> [process ...] [--skip-tests] [--keep-backups N]
  deploy-server.sh <service> <process> [process ...] [--skip-tests] [--keep-backups N]

Examples:
  deploy-server.sh list
  deploy-server.sh processes psl-be-partystar
  deploy-server.sh plan psl-be-partystar http cmd.activity
  deploy-server.sh psl-be-partystar http
  deploy-server.sh deploy psl-be-partystar rpc cmd.user_exp

The service allowlist and service-specific conventions are defined in this
script. The deploy action only runs on the PSL 004 test host.
EOF
}

log() {
	printf '[deploy-server] %s\n' "$*"
}

die() {
	printf '[deploy-server] ERROR: %s\n' "$*" >&2
	exit 1
}

allowed_services() {
	printf '%s\n' "psl-be-partystar"
}

configure_service() {
	case "$service" in
		psl-be-partystar)
			repository="$GIT_ROOT/psl-be-partystar"
			target_dir="$WEB_ROOT/psl-be-partystar"
			branch="$DEFAULT_BRANCH"
			after_copy_hook="/home/ecs-user/sh/consul.init.sh"
			artifact_names=(http rpc cmd)
			asset_directories=(config i18n public template)
			;;
		*)
			die "service is not allowlisted: $service"
			;;
	esac
}

parse_args() {
	(($# > 0)) || {
		usage
		exit 64
	}

	case "$1" in
		list)
			action="list"
			shift
			;;
		processes | plan | deploy)
			action="$1"
			shift
			;;
		-h | --help)
			usage
			exit 0
			;;
		*)
			action="deploy"
			;;
	esac

	if [[ "$action" == "list" ]]; then
		(($# == 0)) || die "list does not accept additional arguments"
		return
	fi

	(($# > 0)) || die "$action requires a service"
	service=$1
	shift

	while (($# > 0)); do
		case "$1" in
			--skip-tests)
				run_tests=false
				;;
			--keep-backups)
				(($# >= 2)) || die "--keep-backups requires a value"
				keep_backups=$2
				[[ "$keep_backups" =~ ^[1-9][0-9]*$ ]] || die "--keep-backups must be a positive integer"
				shift
				;;
			-h | --help)
				usage
				exit 0
				;;
			-*)
				die "unknown option: $1"
				;;
			*)
				selectors+=("$1")
				;;
		esac
		shift
	done

	if [[ "$action" != "processes" ]]; then
		((${#selectors[@]} > 0)) || die "$action requires at least one process"
	fi
}

prune_old_backups() {
	local service_release_dir=$1 retain=$2 entry
	local -a backups=()

	[[ "$service_release_dir" == "$RELEASE_ROOT/"* ]] || die "backup directory escaped the approved release root"
	while IFS= read -r entry; do
		[[ -n "$entry" ]] && backups+=("${entry#* }")
	done < <(find "$service_release_dir" -mindepth 1 -maxdepth 1 -type d -name 'backup.*' -printf '%T@ %p\n' | sort -nr)

	((${#backups[@]} <= retain)) && return 0
	for entry in "${backups[@]:retain}"; do
		[[ "$entry" == "$service_release_dir/backup."* ]] || die "refusing to prune unexpected backup path: $entry"
		log "removing expired backup: $entry"
		rm -rf -- "$entry"
	done
}

assert_test_host() {
	local host_ips

	host_ips=$(hostname -I 2>/dev/null || true)
	[[ " $host_ips " == *" $EXPECTED_HOST_IP "* ]] ||
		die "refusing to run outside the PSL 004 test host ($EXPECTED_HOST_IP)"
	[[ "$repository" == "$GIT_ROOT/"* ]] || die "repository escaped the approved Git root"
	[[ "$target_dir" == "$WEB_ROOT/"* ]] || die "target escaped the approved web root"
}

registered_service_processes() {
	case "$service" in
		psl-be-partystar)
			{ supervisorctl status 2>/dev/null || true; } |
				awk '$1 == "go.ps_http" || $1 == "go.ps_rpc" || $1 ~ /^go\.ps_cmd\./ {print $1}' |
				sort -u
			;;
	esac
}

map_selector() {
	local suffix

	case "$service:$1" in
		psl-be-partystar:http) printf '%s\n' "go.ps_http" ;;
		psl-be-partystar:rpc) printf '%s\n' "go.ps_rpc" ;;
		psl-be-partystar:cmd.*)
			suffix=${1#cmd.}
			[[ "$suffix" =~ ^[A-Za-z0-9_-]+$ ]] || die "invalid CMD process selector: $1"
			printf 'go.ps_cmd.%s\n' "$suffix"
			;;
		psl-be-partystar:go.ps_http | psl-be-partystar:go.ps_rpc)
			printf '%s\n' "$1"
			;;
		psl-be-partystar:go.ps_cmd.*)
			suffix=${1#go.ps_cmd.}
			[[ "$suffix" =~ ^[A-Za-z0-9_-]+$ ]] || die "invalid Supervisor process name: $1"
			printf 'go.ps_cmd.%s\n' "$suffix"
			;;
		*) die "unsupported process selector for $service: $1" ;;
	esac
}

resolve_processes() {
	local selector process existing duplicate
	local -a resolved=()

	for selector in "${selectors[@]}"; do
		if [[ "$selector" == "all" ]]; then
			while IFS= read -r process; do
				[[ -n "$process" ]] && resolved+=("$process")
			done < <(registered_service_processes)
		else
			process=$(map_selector "$selector")
			resolved+=("$process")
		fi
	done

	((${#resolved[@]} > 0)) || die "no process resolved"
	processes=()
	while IFS= read -r process; do
		[[ -n "$process" ]] || continue
		duplicate=false
		if ((${#processes[@]} > 0)); then
			for existing in "${processes[@]}"; do
				if [[ "$existing" == "$process" ]]; then
					duplicate=true
					break
				fi
			done
		fi
		[[ "$duplicate" == true ]] || processes+=("$process")
	done < <(printf '%s\n' "${resolved[@]}" | sort)
}

supervisor_process_exists() {
	local output

	output=$(supervisorctl status "$1" 2>&1 || true)
	[[ -n "$output" && "$output" != *"no such process"* ]]
}

find_partystar_cmd_config() {
	local process=$1 file declared_program

	while IFS= read -r file; do
		declared_program=$(sed -n 's/^\[program:\([^]]*\)\]$/\1/p' "$file" | head -1)
		if [[ "$declared_program" == "$process" ]]; then
			printf '%s\n' "$file"
			return 0
		fi
	done < <(find "$build_dir/deploy" -maxdepth 2 -type f -path '*/cmd*/*.conf' -print | sort)

	return 1
}

render_new_supervisor_config() {
	local process=$1 source_file target_file

	case "$service:$process" in
		psl-be-partystar:go.ps_cmd.*)
			source_file=$(find_partystar_cmd_config "$process") ||
				die "new process has no matching repository Supervisor config: $process"
			target_file="$SUPERVISOR_CONF_DIR/$process.conf"
			[[ ! -e "$target_file" ]] || die "refusing to overwrite unregistered Supervisor config: $target_file"
			sed \
				-e 's#/home/ecs-user/webroot/partystar#/home/ecs-user/webroot/psl-be-partystar#g' \
				-e 's/config_prod\.toml/config_dev.toml/g' \
				-e '/^environment[[:space:]]*=[[:space:]]*APP_VERSION=/d' \
				"$source_file" >"$backup_dir/$process.conf.new"
			grep -qxF "[program:$process]" "$backup_dir/$process.conf.new" ||
				die "rendered config has an unexpected program name: $process"
			grep -qF "directory=$target_dir" "$backup_dir/$process.conf.new" ||
				die "rendered config has an unexpected target directory: $process"
			new_supervisor_configs+=("$process")
			;;
		*)
			die "automatic Supervisor registration is not supported for: $process"
			;;
	esac
}

prepare_source() {
	local revision

	# The MCP service is non-interactive. Use the test host's deploy SSH key for
	# GitHub remotes without permanently rewriting the repository configuration.
	GIT_SSH_COMMAND="$DEPLOY_GIT_SSH_COMMAND" \
		git -c "$GITHUB_SSH_REWRITE" -C "$repository" fetch --prune origin "$branch"
	revision=$(git -C "$repository" rev-parse "origin/$branch^{commit}")
	build_dir=$(mktemp -d "/tmp/deploy-server.$service.${revision:0:12}.XXXXXX")
	rmdir "$build_dir"
	git -C "$repository" worktree add --detach "$build_dir" "$revision"
	GIT_SSH_COMMAND="$DEPLOY_GIT_SSH_COMMAND" \
		git -c "$GITHUB_SSH_REWRITE" -C "$build_dir" submodule update --init --recursive
	printf '%s\n' "$revision" >"$build_dir/.deploy-revision"
	log "source revision: $revision"
}

build_source() {
	case "$service" in
		psl-be-partystar)
			(
				cd "$build_dir"
				export PATH="/usr/local/go/bin:/home/ecs-user/go/bin:$PATH"
				export GOPATH="/home/ecs-user/go"
				export GOMODCACHE="$GOPATH/pkg/mod"
				export GOCACHE="/home/ecs-user/.cache/go-build"
				if [[ "$run_tests" == true ]]; then
					go test ./...
				fi
				# CI_PULL_REQUEST is only used by reviewdog, but the legacy Makefile
				# otherwise evaluates a gh command even for the build target.
				CGO_ENABLED=0 GOOS=linux GOARCH=amd64 make build CI_PULL_REQUEST=
			)
			;;
	esac
}

prepare_backup() {
	local name

	mkdir -p "$RELEASE_ROOT/$service"
	backup_dir=$(mktemp -d "$RELEASE_ROOT/$service/backup.XXXXXX")
	for name in bin "${asset_directories[@]}"; do
		[[ -d "$target_dir/$name" ]] && cp -a "$target_dir/$name" "$backup_dir/$name"
	done
	[[ -f "$target_dir/.deploy-revision" ]] && cp "$target_dir/.deploy-revision" "$backup_dir/.deploy-revision"
	log "backup: $backup_dir"
}

install_file_atomically() {
	local source_file=$1 target_file=$2 temporary_file

	temporary_file="${target_file}.deploy.$$"
	cp -a "$source_file" "$temporary_file"
	mv -f "$temporary_file" "$target_file"
}

install_release() {
	local name

	deployed=true
	for name in "${artifact_names[@]}"; do
		[[ -f "$build_dir/bin/$name" ]] || die "missing build artifact: bin/$name"
		install_file_atomically "$build_dir/bin/$name" "$target_dir/bin/$name"
	done
	for name in "${asset_directories[@]}"; do
		[[ -d "$build_dir/$name" ]] || continue
		mkdir -p "$target_dir/$name"
		cp -a "$build_dir/$name/." "$target_dir/$name/"
	done
	cp "$build_dir/.deploy-revision" "$target_dir/.deploy-revision"

	if [[ -n "$after_copy_hook" ]]; then
		[[ -f "$after_copy_hook" ]] || die "after-copy hook not found: $after_copy_hook"
		/bin/sh "$after_copy_hook"
	fi
}

register_new_processes() {
	local process

	for process in "${processes[@]}"; do
		if ! supervisor_process_exists "$process"; then
			render_new_supervisor_config "$process"
		fi
	done

	for process in "${new_supervisor_configs[@]}"; do
		cp "$backup_dir/$process.conf.new" "$SUPERVISOR_CONF_DIR/$process.conf"
	done

	if ((${#new_supervisor_configs[@]} > 0)); then
		supervisorctl reread
		for process in "${new_supervisor_configs[@]}"; do
			supervisorctl update "$process"
		done
	fi
}

restart_and_verify() {
	local process status attempt

	for process in "${processes[@]}"; do
		log "restarting $process"
		supervisorctl restart "$process"
		for attempt in 1 2 3 4 5; do
			sleep 2
			status=$(supervisorctl status "$process" 2>&1 || true)
			[[ "$status" == *" RUNNING "* ]] && break
			log "waiting for $process ($attempt/5): $status"
		done
		printf '%s\n' "$status"
		[[ "$status" == *" RUNNING "* ]] || die "$process did not reach RUNNING"
	done
}

rollback() {
	local name process config_file

	[[ "$deployed" == true && -d "$backup_dir" ]] || return 0
	log "restoring $service from $backup_dir"
	for name in bin "${asset_directories[@]}"; do
		if [[ -d "$backup_dir/$name" ]]; then
			rm -rf "$target_dir/$name"
			cp -a "$backup_dir/$name" "$target_dir/$name"
		fi
	done
	if [[ -f "$backup_dir/.deploy-revision" ]]; then
		cp "$backup_dir/.deploy-revision" "$target_dir/.deploy-revision"
	else
		rm -f "$target_dir/.deploy-revision"
	fi
	for process in "${new_supervisor_configs[@]}"; do
		config_file="$SUPERVISOR_CONF_DIR/$process.conf"
		[[ -f "$config_file" ]] && rm -f "$config_file"
	done
	if ((${#new_supervisor_configs[@]} > 0)); then
		supervisorctl reread >/dev/null 2>&1 || true
		for process in "${new_supervisor_configs[@]}"; do
			supervisorctl update "$process" >/dev/null 2>&1 || true
		done
	fi
	if [[ -n "$after_copy_hook" && -f "$after_copy_hook" ]]; then
		/bin/sh "$after_copy_hook" >/dev/null 2>&1 || true
	fi
	for process in "${processes[@]}"; do
		supervisorctl restart "$process" >/dev/null 2>&1 || true
	done
}

cleanup() {
	local exit_code=$?

	trap - EXIT ERR
	set +e
	((exit_code == 0)) || rollback
	if [[ -n "$build_dir" && -d "$build_dir" ]]; then
		git -C "$repository" worktree remove --force "$build_dir" >/dev/null 2>&1 || true
	fi
	exit "$exit_code"
}

print_plan() {
	log "service: $service"
	log "environment: 004 test only"
	log "source: origin/$branch"
	log "repository: $repository"
	log "target: $target_dir"
	log "tests: $run_tests"
	log "backup retention: $keep_backups"
	printf '[deploy-server] processes:\n'
	printf '  - %s\n' "${processes[@]}"
}

main() {
	parse_args "$@"
	if [[ "$action" == "list" ]]; then
		allowed_services
		exit 0
	fi

	configure_service
	assert_test_host
	if [[ "$action" == "processes" ]]; then
		registered_service_processes
		exit 0
	fi

	resolve_processes
	print_plan
	if [[ "$action" == "plan" ]]; then
		exit 0
	fi

	exec 9>"/tmp/deploy-server.$service.lock"
	flock -n 9 || die "another deployment is running for $service"
	prepare_source
	build_source
	log "phase=deploying"
	prepare_backup
	install_release
	register_new_processes
	restart_and_verify
	# The release is healthy at this point. Backup retention is housekeeping and
	# must not roll back a successful deployment if cleanup itself fails.
	deployed=false
	log "deployment completed: $(<"$build_dir/.deploy-revision")"
	prune_old_backups "$RELEASE_ROOT/$service" "$keep_backups"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
	trap cleanup EXIT
	main "$@"
fi
