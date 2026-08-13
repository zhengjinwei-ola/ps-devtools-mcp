#!/usr/bin/env bash
set -euo pipefail

readonly install_root="${PS_MCP_INSTALL_ROOT:-/home/ecs-user/webroot/ps-devtools-mcp}"
readonly env_file="${PS_MCP_ENV_FILE:-$install_root/configs/service.env}"
readonly service_name="${PS_MCP_SUPERVISOR_SERVICE:-ps-devtools-mcp}"
readonly health_url="${PS_MCP_HEALTH_URL:-http://127.0.0.1:18080/healthz}"
readonly lock_file="${PS_MCP_TOKEN_LOCK_FILE:-$install_root/configs/service.env.lock}"

usage() {
	echo "usage: $0 <username>" >&2
	exit 64
}

[[ $# -eq 1 ]] || usage
username=$1
[[ "$username" =~ ^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$ ]] || {
	echo "username must be 1-64 characters: letters, digits, dot, underscore, or hyphen" >&2
	exit 64
}

for command in flock jq openssl supervisorctl curl; do
	command -v "$command" >/dev/null || {
		echo "required command is missing: $command" >&2
		exit 69
	}
done

[[ -r "$env_file" && -w "$env_file" ]] || {
	echo "environment file must be readable and writable: $env_file" >&2
	exit 77
}

umask 077
exec 9>"$lock_file"
flock -x 9

token_json=$(sed -n "s/^PS_MCP_AUTH_TOKENS_JSON='\(.*\)'$/\1/p" "$env_file")
[[ -n "$token_json" ]] || {
	echo "PS_MCP_AUTH_TOKENS_JSON is missing or not in the expected single-quoted format" >&2
	exit 78
}
jq -e 'type == "object"' >/dev/null <<<"$token_json" || {
	echo "PS_MCP_AUTH_TOKENS_JSON is not a JSON object" >&2
	exit 78
}
if jq -e --arg username "$username" 'has($username)' >/dev/null <<<"$token_json"; then
	echo "token already exists for username: $username" >&2
	exit 65
fi

token=$(openssl rand -hex 32)
updated_json=$(jq -c --arg username "$username" --arg token "$token" '. + {($username): $token}' <<<"$token_json")
tmp_file=$(mktemp "$install_root/configs/service.env.XXXXXX")
backup_file=$(mktemp "$install_root/configs/service.env.backup.XXXXXX")
cleanup() { rm -f "$tmp_file" "$backup_file"; }
trap cleanup EXIT

cp -p "$env_file" "$backup_file"
awk -v replacement="PS_MCP_AUTH_TOKENS_JSON='$updated_json'" '
	/^PS_MCP_AUTH_TOKENS_JSON=/ { print replacement; replaced=1; next }
	{ print }
	END { if (!replaced) exit 1 }
' "$env_file" >"$tmp_file"
chmod 0600 "$tmp_file"
mv "$tmp_file" "$env_file"

if ! supervisorctl restart "$service_name" >/dev/null || ! curl --fail --silent --show-error --max-time 5 "$health_url" >/dev/null; then
	cp -p "$backup_file" "$env_file"
	supervisorctl restart "$service_name" >/dev/null || true
	echo "service restart or health check failed; token configuration was rolled back" >&2
	exit 70
fi

printf 'username=%s\ntoken=%s\n' "$username" "$token"
