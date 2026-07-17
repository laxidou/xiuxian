# 无尽仙途 — 连续世界文字 Web MVP

一个以时间为基础资源、运行在单一连续二维世界中的多人文字修仙游戏。角色离线后仍继续增长修为、消耗寿元和沿轨迹移动；Web 与 MCP API Key 操作同一个永久角色，并进入同一权威命令顺序。

## 一键启动

```bash
docker compose up --build
```

打开 <http://localhost>；也可以访问 <https://localhost>（本地 Caddy 使用自己的开发 CA，首次访问可能需要信任证书）。生产部署应把 `COOKIE_SECURE` 保持为 `true`。完整拓扑包括：

- `web`：React / TypeScript / Vite 纯文字响应式客户端
- `game-server`：基于 Go-Kratos 的唯一世界权威和 HTTP / gRPC / SSE API
- `mcp-gateway`：无状态 MCP JSON-RPC 入口，只持有到 authority 的客户端连接
- `worker`：使用 `FOR UPDATE SKIP LOCKED` 消费 PostgreSQL Outbox
- `postgres`：账号、世界快照、规范化领域表、事件和 Outbox 的权威存储
- `redis`：可重建缓存、期限与实时投影的运行边界（不是权威存储）
- `caddy`：本地 HTTPS、Web/API/MCP 统一入口

数据库变更由 `migrate` 一次性服务使用 goose 执行，生产进程不会运行 `AutoMigrate`。

## 已实现的游戏循环

- 公开注册、永久唯一角色名、一账号一角色、bcrypt 密码、可过期 HTTP-only 会话
- 可轮换和立即撤销的角色级 MCP API Key（服务端只保存 SHA-256 哈希）
- 32 级版本化规则表、惰性修为/年龄/境界/寿元派生、确定性寿尽边界
- 千分之一世界单位的规范坐标、持续移动、转向、停止、突破后动态速度、单调世界边界
- 主动神识扫描、5 秒限频、确定排序/截断、高境界扫描通知、隐藏机缘信号
- 范围内交谈请求、接受/拒绝/忽略/关闭、消息显式标记为不可信角色内容
- 整数分钟传功、跌境寿元校验、幂等重试、严格高境界同坐标夺功、分数修为守恒
- 寿尽死亡、机缘生成与隐藏、精确坐标绑定、24 小时线性参悟、转世与永久身份保留
- 可补读游标的 SSE、角色事件与跨世历史、PostgreSQL 同步快照和事务 Outbox
- MCP 工具：状态、边界、扫描、移动/停止、交谈列表/请求、传功、夺功、转世、近期事件

这是一套功能型 MVP，不声称已经通过 1,000 在线角色容量测试。

## 本地开发

后端：

```bash
go test ./...
go vet ./...
```

Web：

```bash
cd web
npm install
npm run typecheck
npm run build
```

完整验收（Go、Web、契约可复现、Compose 与故障恢复冒烟）：

```bash
scripts/check.sh
```

Protobuf 契约位于 `api/proto/xiuxian/v1/world.proto`，Buf 配置位于仓库根目录。规则引擎使用毫秒修为单位（自然经过 1 毫秒即增加 1 内部修为单位，60,000 单位为 1 点修为）；坐标使用千分之一世界单位，避免夺功和机缘发现依赖浮点相等。

后端按 Kratos 分层组织：

- `internal/service`：Proto 生成接口与兼容 HTTP/SSE 的传输适配
- `internal/biz`：上下文感知的世界用例和仓储接口
- `internal/data`：PostgreSQL / 内存仓储实现
- `internal/server`：Kratos HTTP、gRPC、中间件和路由装配
- `cmd/game-server/wire_gen.go`：Wire 生成的依赖注入图

生成 Go/gRPC/Kratos HTTP、OpenAPI、TypeScript 契约和 Wire 注入代码：

```bash
scripts/generate-contracts.sh
git diff --exit-code
```

## MCP

在 Web 的“MCP 代理权限”中轮换 Key，然后以 `Authorization: Bearer xiu_...` 调用：

```text
POST https://localhost/mcp
```

网关实现 MCP `initialize`、`tools/list` 和 `tools/call`。交谈消息始终作为 `trusted: false` 的角色内容返回，代理不得把它们拼接为系统指令。

## 测试接缝

- `internal/rules`：纯规则 worked examples，包括突破/寿尽同毫秒、动态速度、传功边界、组合感应半径和机缘转化。
- `internal/api`：公开 HTTP 黑盒场景，包括并发重名、会话、移动、扫描、传功/夺功、死亡/转世、机缘、MCP Key 和 authority 重启恢复。
- `internal/server`：Kratos 生成路由与兼容 REST 路由共同运行的传输测试。
- `cmd/mcp-gateway`：MCP 工具发现与 authority 代理行为。

完整 Compose 冒烟应确认 `world_snapshots` 存在一行，并且 `outbox` 行最终被 Worker 标记完成。

部署配置、数据库备份恢复和常见故障处理见 [运行手册](docs/operations.md)。
