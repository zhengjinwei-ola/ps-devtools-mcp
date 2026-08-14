# 004 测试机统一部署命令

`deploy-server.sh` 是 PSL 旧服务在 004 测试机上的统一部署入口。服务白名单和少量历史兼容规则直接维护在脚本中，不需要为每个服务额外创建 YAML。

## 安装位置

源码位于：

```text
ps-devtools-mcp/scripts/deploy-server.sh
```

验证完成后安装到 004：

```text
/home/ecs-user/sh/deploy-server.sh
```

安装属于测试环境写操作，必须单独批准，不能通过文档示例自动执行。

## 命令

列出白名单服务：

```bash
./sh/deploy-server.sh list
```

列出服务当前已注册的 Supervisor 进程：

```bash
./sh/deploy-server.sh processes psl-be-partystar
```

预览部署计划：

```bash
./sh/deploy-server.sh plan psl-be-partystar http cmd.activity
```

部署一个或多个进程：

```bash
./sh/deploy-server.sh psl-be-partystar http
./sh/deploy-server.sh deploy psl-be-partystar rpc cmd.user_exp
```

默认只保留每个服务最近 3 份成功部署备份。可在特殊情况下显式调整：

```bash
./sh/deploy-server.sh deploy psl-be-partystar http --keep-backups 5
```

`psl-be-partystar` 支持以下选择器：

- `http`：`go.ps_http`
- `rpc`：`go.ps_rpc`
- `cmd.<name>`：`go.ps_cmd.<name>`
- 精确的 `go.ps_http`、`go.ps_rpc` 或 `go.ps_cmd.<name>`
- `all`：所有已注册的上述 Partystar 进程

## 部署规则

- 只允许在 004 测试机 `192.168.35.221` 运行。
- 当前白名单只有 `psl-be-partystar`。
- 固定构建 `origin/dev` 的精确 commit。
- 使用临时 Git worktree，不修改服务器常驻仓库的工作副本。
- 默认先运行 `go test ./...`，历史测试暂时阻塞时可显式传入 `--skip-tests`。
- 使用仓库锁定的 submodule commit，不执行远程 submodule 漂移更新。
- 编译后统一同步二进制、配置、i18n、public 和 template 资源。
- 已注册进程复用 004 当前 Supervisor 配置，不拆分或覆盖历史聚合配置。
- 新增的 `go.ps_cmd.*` 进程从仓库对应配置生成 004 测试配置并注册。
- 仓库删除的配置不会自动删除或停止 004 上的进程。
- 部署失败时恢复文件备份、移除本次新增的 Supervisor 配置并尝试恢复进程。
- 部署成功后删除超出保留数量的旧 `backup.*` 目录；失败时不执行清理。

## 接入其他服务

接入新服务时修改脚本中的三个受限位置：

1. `allowed_services` 增加服务名；
2. `configure_service` 增加仓库、目标目录、构建产物和固定 hook；
3. 增加该服务的进程选择器与 Supervisor 配置发现约定。

不接受命令行传入任意仓库路径、目标目录、构建命令或 hook，避免 AI 接入后退化为远程任意命令执行器。
