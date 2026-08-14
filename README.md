# PS DevTools MCP

面向 004 旧服务测试机（`ps-sg-dev-001`）的受限运维 MCP。提供：

- `query_test_db`：查询 `xianshi`（engine 1）或 `config`（engine 3），只允许单条有界只读 SQL；
- `query_test_redis` / `inspect_test_redis`：查询 004 本机 Redis，只开放有界只读命令；
- `list_test_log_sources` / `search_test_logs` / `trace_test_logs`：通过同机日志 MCP 查询白名单日志并脱敏；
- `get_test_user_snapshot`：读取固定的非敏感用户、VIP 与背包诊断字段。
- `list_test_deploy_services` / `list_test_deploy_processes` / `plan_test_deployment`：查看白名单部署目标；
- `deploy_test_service`：构建并部署白名单测试服务的指定 Supervisor 进程。

服务不提供 Shell、任意文件路径或任意 URL。唯一写操作 `deploy_test_service` 只能调用 004 本机固定路径的部署脚本，并由脚本再次校验主机、服务、进程和目录白名单。数据库密码和 MCP Token 只能放在权限受限的部署环境文件中，禁止提交到 Git。

## 架构

```text
Claude/Codex -> GateShell 或 HTTPS -> ps-devtools-mcp (004)
                                      |- MySQL RDS（只读事务）
                                      |- 127.0.0.1:6379（只读命令白名单）
                                      `- 本机日志 MCP（路径白名单 + 脱敏）
```

日志能力复用 `psl-test-logs-mcp` 的安全后端；在 004 使用本仓库的 `config/log-sources.004.example.json`，不要开放 `/home/ecs-user/log/*.log` 这种全局 glob。

## 构建与测试

```bash
go test ./...
go vet ./...
go build -o bin/ps-devtools-mcp ./cmd/ps-devtools-mcp
```

## 004 部署环境

`configs/service.env` 至少配置：

```text
PS_TEST_DB_HOST=<test-rds-host>
PS_TEST_DB_PORT=3306
PS_TEST_DB_USER=<readonly-user>
PS_TEST_DB_PASSWORD=<secret>
PS_TEST_DB_XIANSHI_DATABASE=xianshi
PS_TEST_DB_CONFIG_DATABASE=config
PS_TEST_REDIS_ADDRESS=127.0.0.1:6379
PS_TEST_REDIS_DATABASE=0
PS_LOG_MCP_COMMAND=/home/ecs-user/webroot/psl-test-logs-mcp/bin/psl-test-logs-mcp
PS_LOG_MCP_ARGS_JSON='["--config=/home/ecs-user/webroot/psl-test-logs-mcp/configs/sources.json"]'
```

HTTP 模式还需配置：

```text
PS_MCP_TRANSPORT=http
PS_MCP_LISTEN_ADDR=127.0.0.1:18081
PS_MCP_AUTH_TOKENS_JSON='{"alice":"<64-hex-token>"}'
```

部署账号应只有目标日志读取权限，数据库账号应为数据库级只读账号。Redis 即使当前无密码，客户端仍只开放代码白名单中的查询命令。

## 本地 stdio 注册

本机启动器通过 GateShell 选择 004，并在远端加载 `service.env`：

```json
{
  "mcpServers": {
    "ps-devtools": {
      "command": "/Users/oswin/ola/ps-devtools-mcp/scripts/run-local-unified-mcp.sh"
    }
  }
}
```

## 004 HTTPS 注册

004 复用 `dev.partystar.cloud` 的现有 TLS 虚拟主机，并通过精确路径
`/ps-devtools/mcp` 暴露本服务，避免影响该域名下已有项目路由：

```text
Codex -> https://dev.partystar.cloud/ps-devtools/mcp
      -> Nginx -> http://127.0.0.1:18081/mcp
      -> ps-devtools-mcp
```

部署文件：

- `deploy/supervisor/ps-devtools-mcp.conf`：HTTP MCP 常驻进程；
- `deploy/nginx/ps-devtools-mcp.location.conf`：加入现有 HTTPS `server` 块的精确路由。

部署前确认 `configs/service.env` 权限为 `0600`，并已设置
`PS_MCP_TRANSPORT=http`、`PS_MCP_LISTEN_ADDR=127.0.0.1:18081` 和
`PS_MCP_AUTH_TOKENS_JSON`。先执行 `nginx -t`，通过后再 reload；不得将
`18081` 的裸 HTTP 端口暴露到公网。

Codex 使用环境变量提供 Bearer Token：

```toml
[mcp_servers.ps-devtools-remote]
url = "https://dev.partystar.cloud/ps-devtools/mcp"
bearer_token_env_var = "PS_DEVTOOLS_REMOTE_TOKEN"
```

## 安全限制

- SQL：仅 `SELECT`、`SHOW`、`DESCRIBE`、`DESC`、只读 `EXPLAIN/WITH`；`SELECT/WITH` 强制数字 `LIMIT`。
- Redis：拒绝写命令和 `KEYS`、`HGETALL`、`SMEMBERS`、`INFO` 等无界命令；范围/扫描最多 500 项。
- 日志：source 与路径只由服务端配置；查询按字面量匹配，限制文件数、扫描量、返回条数和输出大小，并对敏感内容脱敏。
- HTTP MCP：必须使用 Bearer Token，默认只监听 loopback，公网访问必须经过可信 HTTPS 反向代理。

从 `psl-devtools-mcp` 继承的基线说明保留在 `docs/psl-devtools-baseline.md`，仅供实现追溯，不能作为 004 部署配置使用。
