# 运行手册

## 启动与验收

开发环境使用一条命令启动完整拓扑：

```bash
docker compose up -d --build
```

迁移由一次性的 `migrate` 服务执行。`game-server` 仅在迁移成功后启动；迁移进程会在 PostgreSQL 刚进入健康状态但跨容器 TCP 尚未就绪时进行有界重试。

查看状态与日志：

```bash
docker compose ps
docker compose logs --tail=200 game-server mcp-gateway worker migrate
```

运行完整验收：

```bash
scripts/check.sh
```

仅运行 Compose 黑盒与故障恢复冒烟：

```bash
scripts/smoke-compose.sh
```

## 配置

| 变量 | 服务 | 用途 |
| --- | --- | --- |
| `DATABASE_URL` | migrate、game-server、worker | PostgreSQL 权威存储 |
| `REDIS_URL` | game-server、worker | 可重建期限与事件投影 |
| `GAME_SERVER_ADDRESS` | game-server | HTTP 监听地址 |
| `GAME_SERVER_GRPC_ADDRESS` | game-server、mcp-gateway | 内部 gRPC 地址 |
| `MCP_GATEWAY_ADDRESS` | mcp-gateway | MCP HTTP 监听地址 |
| `COOKIE_SECURE` | game-server | 生产必须保持 `true` |
| `WORKER_TOKEN` | game-server、worker | 内部寿尽结算鉴权；两端必须一致 |
| `APP_VERSION` | game-server | Web 健康状态展示的版本文本 |

生产环境必须替换 Compose 示例中的数据库密码和 Worker Token，并通过 HTTPS 暴露 Web/API/MCP。MCP Key 只在轮换时显示一次，数据库仅保存摘要。

## 健康检查

- `GET /api/v1/healthz`：经 Caddy 验证 Web 到 game-server 的 HTTP 路径和版本。
- game-server 容器 `/healthz`：authority HTTP 进程。
- mcp-gateway 容器 `/healthz`：会调用标准 gRPC Health，只有 game-server gRPC 为 `SERVING` 时才健康。
- PostgreSQL 使用 `pg_isready`；Redis 使用 `PING`。

Redis 故障是可降级故障，PostgreSQL 故障是权威状态故障。Redis 清空后重启 Worker 会从 `roles`/`lives` 重建 `world:death_deadlines`。

## 数据库迁移

新增迁移放在 `migrations/`，使用 goose 顺序编号。应用迁移：

```bash
docker compose run --rm migrate
```

回滚前必须先备份并在副本验证。生产进程不会运行 `AutoMigrate`。

## 备份与恢复

备份 PostgreSQL：

```bash
docker compose exec -T postgres pg_dump -U xiuxian -d xiuxian -Fc > xiuxian.dump
```

恢复到空数据库：

```bash
docker compose stop game-server worker mcp-gateway
docker compose exec -T postgres dropdb -U xiuxian --if-exists xiuxian
docker compose exec -T postgres createdb -U xiuxian xiuxian
docker compose exec -T postgres pg_restore -U xiuxian -d xiuxian --clean --if-exists < xiuxian.dump
docker compose exec -T redis redis-cli FLUSHDB
docker compose start game-server worker mcp-gateway
```

恢复后确认 `world_snapshots` 存在、`outbox` 最终无未完成行，并运行 `scripts/smoke-compose.sh`。Redis 不需要备份。

## 常见故障

### migrate 退出

查看 `docker compose logs migrate postgres`。连接失败会自动重试 30 秒；持续失败通常表示 `DATABASE_URL`、凭据或网络配置错误。

### MCP 健康检查失败

确认 game-server 的 `:9090` 已监听，并查看两端日志。MCP 网关不连接数据库，authority gRPC 不可用时会返回 503。

### Outbox 积压

```bash
docker compose exec -T postgres psql -U xiuxian -d xiuxian -c \
  "SELECT count(*) AS pending, min(available_at) AS oldest FROM outbox WHERE completed_at IS NULL;"
```

检查 Worker 日志中的 `outbox` 错误。重启 Worker 是安全的，领取使用 `FOR UPDATE SKIP LOCKED`，已提交状态仍以 PostgreSQL 为准。

### Redis 数据丢失

```bash
docker compose exec -T redis redis-cli FLUSHDB
docker compose restart worker
docker compose exec -T redis redis-cli zcard world:death_deadlines
```

期限成员会带角色状态版本；过期版本在 authority 结算时被忽略。
