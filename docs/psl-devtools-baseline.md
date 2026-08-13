# PSL DevTools MCP

本地或远程 MCP Server，为 AI 客户端提供受限的 PSL 测试环境只读工具。

## 工具

推荐优先使用语义工具：

- `get_test_user_snapshot`：按 UID 聚合非敏感用户状态、VIP 和有界背包数据。
- `inspect_test_redis`：一次返回精确 key 的类型、TTL 和有界内容。
- `list_test_log_sources` / `search_test_logs`：通过远端只读后端查询白名单日志。
- `trace_test_logs`：在最多八个指定日志 source 中按 trace ID 串联证据。
- `call_test_readonly_api`：调用管理员预登记的测试环境只读 HTTP endpoint。
- `list_test_runtime_sources` / `get_test_service_runtime`：查询测试机二进制构建/VCS 信息、配置摘要和白名单配置值。

`query_test_db` 和 `query_test_redis` 是语义工具未覆盖时的底层受限入口。

### `get_test_user_snapshot`

固定读取 `xs_user_profile`、`xs_user_vip`、`xs_user_commodity`、
`xs_user_commodity_spu` 和 `xs_user_prop_card` 的诊断字段。不会返回姓名、头像、
手机号、凭证、扩展 JSON 或操作人。各 section 支持部分失败，背包分类分别限制行数。

### `inspect_test_redis`

先查询 `TYPE` 和 `TTL`，再按 string/list/hash/set/zset 使用有界命令读取内容。
扫描结果会返回 `next_cursor` 和 `truncated`，不自动继续无界扫描。

### `query_test_db`

输入：

```json
{
  "statement": "SELECT id, num FROM bbc_rank_award WHERE id = 5934 LIMIT 1",
  "engine": 3
}
```

- `engine=1`：xianshi 数据库。
- `engine=3`：config 数据库。
- 只允许一条 `SELECT`、`SHOW`、`DESCRIBE`、`DESC`、`EXPLAIN` 或只读 `WITH`。
- `SELECT` 和 `WITH` 必须包含数字 `LIMIT`。

### `query_test_redis`

输入：

```json
{
  "command": "GET vip:user:10001"
}
```

- 只开放有界只读命令，包括单值查询、计数、成员判断、最多 500 项的范围查询和带 `COUNT <= 500` 的扫描。
- 拒绝所有写命令，以及 `KEYS`、`HGETALL`、`SMEMBERS`、`INFO` 等可能返回无界结果的命令。
- Redis key 和参数不得包含空格；不支持引号转义或多命令。

## 安全模型

- MCP 在请求离开开发机前执行保守的只读校验，测试环境接口会再次校验。
- SQL 结果由服务端限制为最多 500 行；MCP 响应体上限为 2 MiB。
- 审计日志只记录语句哈希、耗时、行数和错误状态，不记录 SQL、Redis key 或返回内容。
- 本工具只面向测试环境，不提供通用 Shell、任意 URL 或写操作入口。
- 日志工具只代理运维配置的远端日志 MCP，客户端不能提供命令或文件路径。
- HTTP 工具只能选择配置文件中的 endpoint，方法、query key 和 body 字段均为白名单；禁止跨域重定向。
- 运行信息从二进制 Go build info 和精确配置文件读取，不执行被检查的服务二进制。

## 可选后端配置

统一日志和运行信息工具需要配置远端日志 MCP 子进程：

```text
PS_LOG_MCP_COMMAND=/Users/oswin/ola/ps-devtools-mcp/scripts/run-ps-sg-dev-001-mcp.exp
PS_LOG_MCP_ARGS_JSON=["/home/ecs-user/webroot/psl-test-logs-mcp/bin/psl-test-logs-mcp","--config=/home/ecs-user/webroot/psl-test-logs-mcp/configs/sources.json"]
```

远端日志 MCP 按首次日志或运行信息请求懒加载，之后在当前 PSL DevTools MCP
进程内复用同一连接。DB、Redis 和只读 HTTP 请求不会等待跳板机连接初始化。

只读 HTTP endpoint 使用 `PS_MCP_READONLY_API_CONFIG` 指向绝对路径 JSON 配置；
格式见 `config/readonly-apis.example.json`。`config/readonly-apis.test.json` 已登记并验证
`gk-external-ping` 与 `gk-commodity-ping`，未确认无副作用的业务接口不会预先加入。
未配置时工具会返回明确错误，不会接受临时 URL。

## 构建

```bash
go build -o bin/ps-devtools-mcp ./cmd/ps-devtools-mcp
```

远程部署默认通过 `http://127.0.0.1/gk/v1/external/testEnvQuery` 访问同机测试环境入口，
避免服务端经公网地址回环。可用 `PS_TEST_QUERY_URL` 覆盖默认测试环境入口。

### 本地直连测试 MySQL

配置以下环境变量后，`query_test_db` 和 `get_test_user_snapshot` 会直连测试 MySQL；
Redis 仍使用测试环境只读查询接口。未设置 `PS_TEST_DB_HOST` 时自动保留原有接口模式：

```bash
export PS_TEST_DB_HOST='test-db.example.internal'
export PS_TEST_DB_PORT='3306'
export PS_TEST_DB_USER='readonly'
export PS_TEST_DB_PASSWORD='use-a-secret-manager-or-local-env'
export PS_TEST_DB_XIANSHI_DATABASE='xianshi'
export PS_TEST_DB_CONFIG_DATABASE='config'
```

密码只从环境变量读取，不应写入仓库、MCP 参数、日志或诊断报告。直连客户端对每次查询使用
只读事务，保持 SQL 白名单，并限制为 10 秒、500 行和 2 MiB。为避免高权限本地凭据跨库，
直连模式拒绝 schema 或别名限定的标识符；通过 `engine` 选择数据库并使用未限定表名和列名。
仍建议为 MCP 创建数据库级只读账号，不依赖高权限账号长期运行。

## 本地 stdio 模式

stdio 是默认模式，stdout 专用于 MCP，审计日志写入 stderr：

完整的本地统一入口使用 `scripts/run-local-unified-mcp.sh`。启动器从 macOS Keychain
读取测试数据库密码，并注入直连 DB、远端日志后端和只读 HTTP 白名单配置。首次使用时通过
交互式提示保存密码，不要把密码写进命令参数：

```bash
security add-generic-password -U -a root -s psl-test-db-password \
  -l 'PSL Test DB Password' -w
```

不同 MCP 客户端的配置文件位置不同，核心配置如下：

```json
{
  "mcpServers": {
    "psl-devtools": {
      "command": "/Users/oswin/ola/ps-devtools-mcp/scripts/run-local-unified-mcp.sh"
    }
  }
}
```

只需要 DB/Redis 基础能力或不使用 macOS Keychain 时，也可以显式启动底层二进制：

```bash
bin/ps-devtools-mcp --transport=stdio
```

### 通过跳板机启动 005 测试机上的 MCP

`scripts/run-ps-sg-dev-001-mcp.exp` 复用 `~/.ssh/login_inner` 的现有认证，
自动选择 `005: ps-sg-dev-001 (192.168.35.221)`，校验目标 IP 后启动远端
stdio MCP。脚本不保存密码、MFA Secret 或一次性验证码：

```bash
scripts/run-ps-sg-dev-001-mcp.exp /opt/ps-devtools-mcp/bin/ps-devtools-mcp --transport=stdio
```

远端可执行文件需要预先部署并赋予最低必要权限。启动器只接受路径、flag、
地址和环境参数所需的安全字符，不支持管道、重定向或任意 Shell 表达式。
由于 GateShell 强制使用 PTY，启动器仅向本地 stdout 转发完整的单行 JSON 对象；
远端日志、终端控制序列和其他非协议输出都会被丢弃。
设置 `PS_MCP_LAUNCHER_DEBUG=1` 会显示跳板登录和菜单输出，仅用于连接诊断，
不得作为 MCP 的正常启动方式。

## 远程 HTTP 模式

HTTP 模式使用 stateless Streamable HTTP，并强制配置 Bearer Token：

```bash
export PS_MCP_AUTH_TOKEN='replace-with-a-long-random-token'
bin/ps-devtools-mcp --transport=http --listen=127.0.0.1:8080
```

团队部署推荐为每位用户配置独立的具名 Token：

```bash
export PS_MCP_AUTH_TOKENS_JSON='{"alice":"replace-with-alice-token","bob":"replace-with-bob-token"}'
bin/ps-devtools-mcp --transport=http --listen=127.0.0.1:8080
```

JSON key 是只用于服务端审计日志的稳定用户标识，value 是实际 Bearer Token。名称和值都不能为空，
不同名称不能共用相同 Token。删除某个条目并重启服务即可单独撤销该用户。旧的
`PS_MCP_AUTH_TOKEN` 仍保持兼容，也可以在迁移期间与具名 Token 同时配置；同时配置时
`legacy` 名称由旧变量保留，并且旧 Token 不能与具名 Token 重复。

环境变量形式：

```bash
export PS_MCP_TRANSPORT=http
export PS_MCP_LISTEN_ADDR=127.0.0.1:8080
export PS_MCP_AUTH_TOKEN='replace-with-a-long-random-token'
export PS_MCP_MAX_CONCURRENT=16
bin/ps-devtools-mcp
```

生产化运行时应从权限受限的环境文件或密钥系统注入 `PS_MCP_AUTH_TOKENS_JSON`，不要把
真实 Token 写入仓库、进程参数或共享聊天。每个 Token 建议使用至少 32 字节的密码学随机值，
例如在安全终端中执行 `openssl rand -hex 32` 后直接存入密钥系统。

端点：

- MCP：`POST /mcp`，需要 `Authorization: Bearer <token>`。
- 健康检查：`GET /healthz`，不需要 token。

远程客户端配置的核心信息：

```json
{
  "url": "https://mcp-test.example.com/mcp",
  "headers": {
    "Authorization": "Bearer ${PS_MCP_AUTH_TOKEN}"
  }
}
```

服务默认只监听 loopback。跨机器部署时可改为 `--listen=:8080`，但必须通过 Nginx、Ingress 或其他可信反向代理提供 HTTPS；不要把裸 HTTP MCP 直接暴露到公网。Token 只能通过环境变量或密钥系统注入，不能提交到 Git。

Codex 配置示例：

```toml
[mcp_servers.psl-devtools]
url = "https://mcp-test.example.com/mcp"
bearer_token_env_var = "PS_MCP_AUTH_TOKEN"
```

Claude Code 项目配置示例（每位用户在本机设置自己的环境变量）：

```json
{
  "mcpServers": {
    "psl-devtools": {
      "type": "http",
      "url": "https://mcp-test.example.com/mcp",
      "headers": {
        "Authorization": "Bearer ${PS_MCP_AUTH_TOKEN}"
      }
    }
  }
}
```

### 添加团队 Token

测试机安装目录提供管理员脚本，传入稳定用户名即可生成 Token、原子更新环境文件、重启
Supervisor 服务并执行健康检查：

```bash
/home/ecs-user/webroot/ps-devtools-mcp/scripts/add-auth-token.sh alice
```

成功时仅输出便于复制的结果：

```text
username=alice
token=<64 位十六进制 Token>
```

用户名只允许 1–64 位字母、数字、点、下划线或连字符，且首字符必须是字母或数字。
脚本拒绝覆盖已有用户名；需要轮换时先明确撤销旧 Token，再重新添加，避免意外中断用户。
脚本使用文件锁避免并发覆盖，并在服务重启或健康检查失败时恢复原配置。
