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
assert_eq $'psl-be-partystar\npsl-be-room' "$(allowed_services)"
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

registered_service_processes() {
	printf '%s\n' room.http room.rpc room.cmd.room
}
selectors=(rpc http cmd.room http)
processes=()
resolve_processes
assert_eq $'room.cmd.room\nroom.http\nroom.rpc' "$(printf '%s\n' "${processes[@]}")"
assert_eq $'psl-be-partystar\npsl-be-room' "$(allowed_services)"

if (map_selector 'room.cmd../../escape' >/dev/null 2>&1); then
	printf 'path-like room CMD selector was unexpectedly accepted\n' >&2
	exit 1
fi

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
