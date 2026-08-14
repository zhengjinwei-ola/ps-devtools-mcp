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
assert_eq "psl-be-partystar" "$(allowed_services)"
assert_eq "3" "$keep_backups"
assert_eq $'config\ni18n\npublic\ntemplate' "$(printf '%s\n' "${asset_directories[@]}")"

printf 'deploy-server tests passed\n'
