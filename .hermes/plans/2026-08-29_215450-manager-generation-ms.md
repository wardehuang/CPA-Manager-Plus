# Manager generation_ms 全链路修复计划

**Goal:** 让 CPA Manager Plus 摄取、持久化并向实时监控输出 CPA 已提供的 `generation_ms`，使 TPS 与实时守护使用同一生成窗口。

**Architecture:** 保留现有 `latency_ms` 和 `ttft_ms` 语义；新增独立可空 `generation_ms`。旧记录保持 NULL，前端继续回退到 `latency_ms - ttft_ms`；新记录优先使用明确生成窗口。不得用回退值回填数据库。

**Constraints:** 不修改当前工作区已有的 `LOCAL_VERSION`、wxai worker、`main.go`、`build_and_deploy.py` 改动；不运行 tests；不 commit；不部署。

---

## Task 1：摄取 generation_ms

- 修改 `apps/manager-server/internal/usage/event.go`
  - 从 `generation_ms` / `generationMs` 读取可空正整数。
  - 写入 `usage.Event.GenerationMS`。
  - `BuildPayload` 将该字段复制到兼容 usage detail。
- 修改 `apps/manager-server/internal/usage/import.go`
  - 原始事件和兼容 usage 导入都保留 `generation_ms`。

## Task 2：SQLite schema 与写入

- 修改 `apps/manager-server/internal/repository/sqlite/migrate.go`
  - 新建表定义加入 `generation_ms integer`。
  - `ensureUsageEventColumns` 为既有数据库增加该列。
- 修改 `apps/manager-server/internal/repository/usageevent/repository.go`
  - InsertBatch 写入 `generation_ms`。
  - ListRecent 查询并恢复 `GenerationMS`。

## Task 3：实时监控与兼容输出

- 修改 `apps/manager-server/internal/repository/usageevent/stream.go`
  - compatible usage 查询、scan、JSONL export 保留 `generation_ms`。
- 修改 `apps/manager-server/internal/repository/usagemonitoring/event_page_read.go`
  - 实时事件页查询 `generation_ms`。
- 修改 `apps/manager-server/internal/repository/usageevent/analytics.go`
  - `EventPageItem` 增加可空 GenerationMS。
- 修改 `apps/manager-server/internal/service/monitoring/service.go`
  - API EventRow 输出 `generation_ms`。
- 修改 `apps/manager-server/internal/repository/usageevent/raw_event.go` 与 `apps/manager-server/internal/service/monitoring/raw_event.go`
  - 原始事件详情输出该指标。

## Task 4：协议证据核验

- 对比降智请求 `a656f723` 与正常 TPS 请求的原始 SSE。
- 统计 `response.output_text.delta`、`response.output_item.done`、`response.completed`。
- 判断“正文仅终态出现”是否能作为独立降智信号，明确误报风险。

## Task 5：验证

- `gofmt` 修改的 Go 文件。
- `git diff --check`。
- 构建 manager-server 和 WebUI 类型检查/构建；不运行 tests。
- 核对 `git diff` 未覆盖已有用户改动。
- 不 commit，不部署。
