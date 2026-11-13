#!/usr/bin/env bash

set -Eeuo pipefail

readonly TEST_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=../scripts/deploy-server.sh
source "$TEST_DIR/../scripts/deploy-server.sh"

assert_eq() {
	local expected=$1 actual=$2
	[[ "$expected" == "$actual" ]] || {
		printf 'expected <%s>, got <%s>\n' "$expected" "$actual" >&2
		exit 1
	}
}

service="psl-be-partystar"
configure_service
assert_eq "go.ps_http" "$(map_selector http)"
assert_eq "go.ps_rpc" "$(map_selector rpc)"
assert_eq "go.ps_cmd.activity" "$(map_selector cmd.activity)"
assert_eq "go.ps_cmd.user_exp" "$(map_selector go.ps_cmd.user_exp)"

if (map_selector 'cmd../../escape' >/dev/null 2>&1); then
	printf 'path-like CMD selector was unexpectedly accepted\n' >&2
	exit 1
fi

if (map_selector 'unrelated.service' >/dev/null 2>&1); then
	printf 'unrelated Supervisor process was unexpectedly accepted\n' >&2
	exit 1
fi

selectors=(rpc http cmd.activity http)
processes=()
resolve_processes
assert_eq $'go.ps_cmd.activity\ngo.ps_http\ngo.ps_rpc' "$(printf '%s\n' "${processes[@]}")"
assert_eq $'psl-be-partystar\npsl-be-room\nps-devtools-mcp\noversea-server' "$(allowed_services)"
assert_eq "3" "$keep_backups"
assert_eq "url.git@github.com:.insteadOf=https://github.com/" "$GITHUB_SSH_REWRITE"
assert_eq "ssh -i /home/ecs-user/.ssh/id_rsa -o IdentitiesOnly=yes -o BatchMode=yes" "$DEPLOY_GIT_SSH_COMMAND"
assert_eq $'config\ni18n\npublic\ntemplate' "$(printf '%s\n' "${asset_directories[@]}")"

service="psl-be-room"
configure_service
assert_eq "/home/ecs-user/gitroot/psl-be-room" "$repository"
assert_eq "/home/ecs-user/webroot/psl-be-room" "$target_dir"
assert_eq $'room_http\nroom_rpc\nroom_cmd' "$(printf '%s\n' "${artifact_names[@]}")"
assert_eq "room.http" "$(map_selector http)"
assert_eq "room.rpc" "$(map_selector rpc)"
assert_eq "room.cmd.room" "$(map_selector cmd.room)"
assert_eq "room.cmd.special.refresh" "$(map_selector room.cmd.special.refresh)"

supervisorctl() {
	printf '%s\n' \
		"go.ps_http RUNNING pid 1, uptime 0:00:01" \
		"room.http RUNNING pid 2, uptime 0:00:01" \
		"room.rpc RUNNING pid 3, uptime 0:00:01" \
		"room.cmd.room RUNNING pid 4, uptime 0:00:01"
}
assert_eq $'room.cmd.room\nroom.http\nroom.rpc' "$(registered_service_processes)"

selectors=(rpc http cmd.room http)
processes=()
resolve_processes
assert_eq $'room.cmd.room\nroom.http\nroom.rpc' "$(printf '%s\n' "${processes[@]}")"

if (map_selector 'room.cmd../../escape' >/dev/null 2>&1); then
	printf 'path-like room CMD selector was unexpectedly accepted\n' >&2
	exit 1
fi

service="ps-devtools-mcp"
configure_service
assert_eq "/home/ecs-user/gitroot/ps-devtools-mcp" "$repository"
assert_eq "/home/ecs-user/webroot/ps-devtools-mcp" "$target_dir"
assert_eq "ps-devtools-mcp" "$(map_selector ps-devtools-mcp)"

supervisorctl() {
	printf '%s\n' "ps-devtools-mcp RUNNING pid 111, uptime 0:00:01"
}
assert_eq "ps-devtools-mcp" "$(registered_service_processes)"

selectors=(ps-devtools-mcp)
processes=()
resolve_processes
assert_eq "ps-devtools-mcp" "$(printf '%s\n' "${processes[@]}")"

service="oversea-server"
configure_service
assert_eq "/home/ecs-user/gitroot/psl-be-oversea-server" "$repository"
assert_eq "/home/ecs-user/webroot/psl-be-oversea-server" "$target_dir"
assert_eq "" "$branch"
assert_eq "xs.admin" "$(map_selector xs.admin)"
assert_eq "xs.room.proc" "$(map_selector xs.room.proc)"

# Homogeneous supervisor groups register members like xs.admin:01; registered
# process discovery must collapse them back to the group name.
supervisorctl() {
	printf '%s\n' \
		"go.ps_http RUNNING pid 111, uptime 0:00:01" \
		"xs.admin:01 RUNNING pid 222, uptime 0:00:01" \
		"xs.admin:02 RUNNING pid 223, uptime 0:00:01" \
		"xs.cmd:01 RUNNING pid 224, uptime 0:00:01" \
		"xs.room.proc RUNNING pid 225, uptime 0:00:01"
}
assert_eq $'xs.admin\nxs.cmd\nxs.room.proc' "$(registered_service_processes)"

selectors=(xs.admin xs.cmd)
processes=()
resolve_processes
assert_eq $'xs.admin\nxs.cmd' "$(printf '%s\n' "${processes[@]}")"

if (map_selector 'xs../../escape' >/dev/null 2>&1); then
	printf 'path-like oversea selector was unexpectedly accepted\n' >&2
	exit 1
fi
if (map_selector 'http' >/dev/null 2>&1); then
	printf 'bare selector without xs. prefix was unexpectedly accepted for oversea\n' >&2
	exit 1
fi

# run_sea_command deploys by git pull in the webroot repository; a missing
# repository must abort before any worker group restart.
tmpdir=$(mktemp -d)
(
	target_dir="$tmpdir/missing"
	if (run_sea_command >/dev/null 2>&1); then
		printf 'run_sea_command unexpectedly succeeded without a webroot repository\n' >&2
		exit 1
	fi
)
mkdir -p "$tmpdir/.git"
git() {
	git_pull_args="$*"
}
(
	target_dir="$tmpdir"
	git_pull_args=""
	run_sea_command >/dev/null
	assert_eq "-C $tmpdir pull" "$git_pull_args"
)
rm -rf "$tmpdir"
unset -f git

(
	action=""
	service=""
	selectors=()
	parse_args status psl-be-partystar rpc
	assert_eq "status" "$action"
	assert_eq "psl-be-partystar" "$service"
	assert_eq "rpc" "${selectors[0]}"
)

restart_calls=0
restart_mode="succeed_on_fifth"
supervisorctl() {
	case "$1" in
		restart)
			((restart_calls += 1))
			[[ "$restart_mode" != "always_fail" ]] || return 1
			((restart_calls >= 5))
			;;
		status)
			if [[ "$restart_mode" != "always_fail" ]] && ((restart_calls >= 5)); then
				printf '%s\n' "go.ps_rpc RUNNING pid 123, uptime 0:00:01"
			else
				printf '%s\n' "go.ps_rpc BACKOFF Exited too quickly"
			fi
			;;
	esac
}
sleep() { :; }
processes=(go.ps_rpc)
restart_and_verify >/dev/null
assert_eq "5" "$restart_calls"

failure_output=$(
	restart_mode="always_fail"
	restart_calls=0
	processes=(go.ps_rpc)
	restart_and_verify 2>&1
) && {
	printf 'restart unexpectedly succeeded after all attempts failed\n' >&2
	exit 1
}
[[ "$failure_output" == *"restarting go.ps_rpc (5/5)"* ]] || {
	printf 'fifth restart attempt was not observed\n' >&2
	exit 1
}
[[ "$failure_output" == *"did not reach RUNNING after 5 restart attempts"* ]] || {
	printf 'retry exhaustion error was not observed\n' >&2
	exit 1
}

printf 'deploy-server tests passed\n'
