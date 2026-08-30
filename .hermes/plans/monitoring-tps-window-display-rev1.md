# Monitoring TPS 窗口展示实施计划 rev.1

## 修订摘要 rev.1

- 保留无 `generation_ms` 时的兼容公式：`latency_ms - ttft_ms`。
- 有 `generation_ms` 时仍以其作为 TPS 分母。
- 页面区分“生成窗口”与“估算窗口”，不让两种来源共享模糊标签。
- `ttft_ms` 不改语义，仅将展示文案改为“上游首字节”。

## 目标口径

1. 有 `generation_ms`：`TPS = (output_tokens + reasoning_tokens) / generation_ms`。
2. 无 `generation_ms`：`TPS = (output_tokens + reasoning_tokens) / (latency_ms - ttft_ms)`。
3. 实时列表展示 TPS 实际使用的窗口及来源。
4. TPS tooltip 展示可复算公式。

## 修改范围

1. `apps/web/src/features/monitoring/model/eventRows.ts`
   - 单一 helper 同时计算 TPS、窗口毫秒数、窗口来源。
   - 返回 `generationMs`、`tpsWindowMs`、`tpsWindowSource`。
2. `apps/web/src/features/monitoring/model/types.ts`
   - 扩展 `MonitoringEventRow` 类型。
3. `apps/web/src/features/monitoring/components/RealtimeEventsPanel.tsx`
   - “首字”改为“上游首字节”。
   - 增加“生成窗口”或“估算窗口”行。
   - TPS 增加公式 tooltip。
4. `apps/web/src/i18n/locales/{zh-CN,zh-TW,en,ru}.json`
   - 同步全部文案。
5. 更新相关测试夹具和断言，但按用户规则不运行测试。

## 不修改

- 不修改 `ttft_ms`、`generation_ms` 后端 schema。
- 不修改 Core executor 或 Realtime Guard。
- 不提交、不部署。
