#!/bin/zsh
set -euo pipefail

readonly mcp_root="/Users/oswin/ola/ps-devtools-mcp"

exec "$mcp_root/scripts/run-ps-sg-dev-001-mcp.exp" \
  /home/ecs-user/webroot/ps-devtools-mcp/scripts/run-stdio-service.sh
