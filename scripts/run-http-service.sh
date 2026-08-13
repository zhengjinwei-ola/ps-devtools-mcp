#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
install_root=$(CDPATH= cd -- "$script_dir/.." && pwd)
env_file=${PS_MCP_ENV_FILE:-"$install_root/configs/service.env"}

if [ ! -r "$env_file" ]; then
	echo "PSL DevTools MCP environment file is not readable: $env_file" >&2
	exit 78
fi

# Keep secrets out of process arguments and Supervisor configuration. The
# deployment must restrict this file to the service account.
set -a
. "$env_file"
set +a

exec "$install_root/bin/ps-devtools-mcp" --transport=http
