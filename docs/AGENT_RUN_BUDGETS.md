# Native Agent run budgets (v0.7 development)

Open **Agent limits** above the composer in TL Studio's expanded Agent panel.
Preferences are stored locally by Agent identity (tool rounds and token cap) and by
selected Provider (model request and stream idle timeout), then sent with each run.

| Control | Default | Allowed values / server cap |
| --- | --- | --- |
| Tool rounds | 24 per run | 1–100; UI presets 24, 50, 100 |
| Model request timeout | 4 minutes direct / 6 minutes routed heavy turn | 30–1200 seconds |
| Stream idle timeout | Disabled | 10–300 seconds; UI presets 30, 60, 120 |
| Approximate model tokens | No optional cap | 1–10,000,000; UI presets 200K, 1M, 2M |

**Run safety:** The hard per-round cap remains 16 tool calls; repeating the same
tool-call arguments too often remains blocked. A sustained 12-round alternating
two-pattern cycle is also rejected before further tool execution.

**On exhaustion:** The Agent issues one final model request **without tools**
and explicitly records that the task is not verified complete. The checkpoint
describes completed work and remaining work; earlier tool outcomes remain
persisted. If the model summary fails or returns no text, TL Studio saves a
deterministic fallback checkpoint instead. Starting another user instruction
begins a new run with its own budget.

**Timeout semantics:** Request timeout is a wall-clock deadline for one model
turn, not the entire task. Idle timeout applies to OpenAI Chat Completions,
OpenAI Responses and Anthropic Messages SSE transports: the watchdog resets
upon receipt of streamed lines. Gemini's JSON transport and account/CLI
bridges use the overall request deadline, not the SSE idle watchdog. A provider
selected through a router uses the selected router's timeout preference for
that run, including fallbacks.

**Token counting:** An optional token cap uses usage reported by the provider
(input + output, falling back to reasoning when output is absent). Providers
that omit usage cannot be reliably bounded by token count. The tool-round
hard cap applies regardless of usage reporting. A final checkpoint may itself
consume additional tokens.

Implementation: session run command fields `maxToolRounds`,
`modelTimeoutSeconds`, `streamIdleTimeoutSeconds`, `maxRunTokens`.
The Go session command validates upper bounds regardless of browser settings.

Regression coverage includes Go Native Agent tests, HTTP provider payload
contract tests, and the workbench browser smoke test.
