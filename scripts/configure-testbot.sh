#!/usr/bin/env bash
set -euo pipefail

readonly install_root="${PS_MCP_INSTALL_ROOT:-/home/ecs-user/webroot/ps-devtools-mcp}"
readonly env_file="${PS_MCP_ENV_FILE:-$install_root/configs/service.env}"
readonly service_name="${PS_MCP_SUPERVISOR_SERVICE:-ps-devtools-mcp}"
readonly health_url="${PS_MCP_HEALTH_URL:-http://127.0.0.1:18081/healthz}"
readonly testbot_config="$install_root/config/testbot.test.json"
readonly lock_file="${PS_MCP_TESTBOT_LOCK_FILE:-$install_root/configs/service.env.lock}"

for command in flock supervisorctl curl; do
	command -v "$command" >/dev/null || { echo "required command not found: $command" >&2; exit 69; }
done
[[ -f "$env_file" && -f "$testbot_config" ]] || { echo "service environment or TestBot config is missing" >&2; exit 66; }

read -r -p "TestBot mobile area: " area
read -r -p "TestBot mobile: " mobile
read -r -s -p "TestBot password: " password
echo
[[ "$area" =~ ^[0-9]{1,4}$ ]] || { echo "invalid mobile area" >&2; exit 64; }
[[ "$mobile" =~ ^[0-9]{6,20}$ ]] || { echo "invalid mobile" >&2; exit 64; }
[[ -n "$password" && "$password" != *"'"* ]] || { echo "invalid password" >&2; exit 64; }

exec 9>"$lock_file"
flock -x 9
tmp_file=$(mktemp "$install_root/configs/service.env.XXXXXX")
backup_file=$(mktemp "$install_root/configs/service.env.backup.XXXXXX")
trap 'rm -f "$tmp_file" "$backup_file"' EXIT
cp "$env_file" "$backup_file"
chmod 600 "$backup_file"
while IFS= read -r line || [[ -n "$line" ]]; do
	case "$line" in
	PS_MCP_TESTBOT_CONFIG=* | PS_TESTBOT_AREA=* | PS_TESTBOT_MOBILE=* | PS_TESTBOT_PASSWORD=*) continue ;;
	esac
	printf '%s\n' "$line"
done <"$env_file" >"$tmp_file"
printf "PS_MCP_TESTBOT_CONFIG='%s'\n" "$testbot_config" >>"$tmp_file"
printf "PS_TESTBOT_AREA='%s'\n" "$area" >>"$tmp_file"
printf "PS_TESTBOT_MOBILE='%s'\n" "$mobile" >>"$tmp_file"
printf "PS_TESTBOT_PASSWORD='%s'\n" "$password" >>"$tmp_file"
chmod 600 "$tmp_file"
mv "$tmp_file" "$env_file"

if ! supervisorctl restart "$service_name" >/dev/null || ! curl --fail --silent --show-error --max-time 5 "$health_url" >/dev/null; then
	cp "$backup_file" "$env_file"
	chmod 600 "$env_file"
	supervisorctl restart "$service_name" >/dev/null || true
	echo "service restart or health check failed; TestBot configuration was rolled back" >&2
	exit 70
fi
echo "TestBot configuration updated; service is healthy"
