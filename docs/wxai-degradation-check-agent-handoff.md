# wXAi 降智检测核心算法

本文只描述语言无关的检测算法与判断逻辑。

## 1. 固定探针

向 `https://cli-chat-proxy.grok.com/v1/responses` 发起一次流式请求：

```json
{
  "model": "grok-4.5",
  "input": "用中文回答：17 × 23 等于多少？只输出计算过程和答案。",
  "stream": true,
  "reasoning": {
    "effort": "high",
    "summary": "detailed"
  },
  "max_output_tokens": 96,
  "temperature": 0
}
```

请求体不包含 `tools`。

期望答案：`391`。

## 2. 流式数据采集

持续读取 SSE，直到：

- 成功：`response.completed` 或 `[DONE]`。
- 失败：`response.incomplete`、`response.failed`、`error`、网络错误、超时或流未完整结束。

采集以下数据：

- `firstTokenMs`：从请求开始到首个非空生成 delta。
- `totalMs`：从请求开始到流结束。
- `outputTokens`：最终 usage 中的 `output_tokens`。
- `reasoningTokens`：最终 usage 中的 `output_tokens_details.reasoning_tokens`。
- `modelAnswer`：拼接全部 `response.output_text.delta`。
- `thinkingDelta`：是否出现 reasoning/thinking delta。

可计为首生成的事件：

- `response.output_text.delta`
- `response.reasoning_summary_text.delta`
- `response.reasoning_text.delta`
- `response.refusal.delta`
- `response.function_call_arguments.delta`
- `response.custom_tool_call_input.delta`

## 3. 指标计算

```text
generationMs = max(1, totalMs - firstTokenMs)
TPS = outputTokens × 1000 / generationMs
answerMatched = modelAnswer 包含 "391"
```

注意：`outputTokens` 已包含 reasoning tokens，计算 TPS 时不能再次加上 `reasoningTokens`。

`thinkingDelta=true` 的条件：

- 出现 `response.reasoning_summary_text.delta`；或
- 出现 `response.reasoning_text.delta`；或
- 事件类型或 `delta.type` 为 `thinking_delta`。

## 4. 最终判断

判断顺序：

```text
如果命中 free-usage-exhausted 类错误：
    classification = quota_exhausted
    qualityLevel = quota_exhausted

否则，如果发生 HTTP 错误、网络错误、超时、SSE 失败或流不完整：
    classification = unknown
    qualityLevel = unknown

否则，如果 TPS >= 1000：
    classification = suspected_degradation
    qualityLevel = hard
    reason = hard_tps

否则，如果 TPS >= 500：
    classification = suspected_degradation
    qualityLevel = soft
    reason = soft_tps

否则：
    classification = normal
    qualityLevel = healthy
    reason = within_threshold
```

## 5. 两个辅助信号

`thinkingDelta` 和 `answerMatched` **只展示，不参与最终分类**。

原因：

- 没有 thinking delta，不代表模型没有推理。
- 有 thinking delta，不代表推理质量正常。
- 答案是否包含 `391` 只能验证这道固定题，不能替代速度异常判断。

因此当前核心判定信号只有：

1. 请求是否成功。
2. 是否额度耗尽。
3. TPS 是否达到软、硬阈值。
