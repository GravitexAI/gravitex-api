# Interactions Task Settlement Reliability Design

## Goal

Make `/v1beta/interactions` asynchronous tiered billing recoverable and idempotent, persist missing-usage diagnostics, and prevent mixed task/token expressions from silently omitting token charges.

## Scope

- Apply the new settlement lifecycle only to tasks created from `/v1beta/interactions` with a tiered billing snapshot.
- Preserve Gemini pass-through, Vertex request adaptation, channel routing/affinity, and all non-Interactions billing semantics.
- Preserve the existing charge when upstream usage required by the configured expression is unavailable; do not fabricate values or reject the request for missing completion usage.
- Keep submission-time expression, request input, and pricing inputs immutable for later retries.

## Design

### 1. Durable settlement journal and recovery

- Add a primary-database settlement record keyed uniquely by task identity. It stores the frozen expression/snapshot reference, terminal task result needed for settlement, computed actual quota, outcome, attempt metadata, and settlement/log delivery states.
- A task reaching terminal success becomes eligible for settlement but is not considered billing-complete until the settlement journal records the committed outcome. A recovery worker/query retries pending or interrupted Interactions settlements independently of the normal active-task poll filter.
- Financial mutation must be one database transaction: apply wallet/subscription/token and user/channel quota deltas, update the task's final quota and durable settlement outcome, and enqueue the corresponding billing-log event. The unique task key and transaction make retries idempotent: either all financial/task changes commit once or none do.
- Do not perform non-idempotent external/log-database writes inside the financial transaction. Persist a log outbox event with a stable operation ID in the same transaction, then deliver it to the configured log database. Delivery must deduplicate by operation ID; retry delivery after process failure without repeating financial mutations.
- Failed/cancelled task refunds continue through their existing path unless code inspection shows that this exact Interactions tiered flow bypasses its idempotency guard; any required correction remains endpoint-scoped.

### 2. Durable missing-usage outcome

- When a successful task lacks a required usage fact or token usage cannot be interpreted, commit a terminal `usage_unavailable_preconsume_retained` settlement outcome. Keep the pre-consumed quota unchanged and store the diagnostic reason and missing fact keys.
- Emit an administrator-visible billing audit record even when the quota delta is zero. Keep expression internals and usage diagnostics out of customer-facing metadata. The record must identify the task/request, configured-vs-upstream source, and retained quota.
- If valid usage arrives on a later recovery attempt before settlement is finalized, evaluate the frozen expression and commit the normal final quota instead of retaining the pre-consume.

### 3. Mixed usage/token expression semantics

- If an Interactions expression references both `u(...)` and token variables (`p`, `c`, `len`, `cr`, `cc`, `cc1h`, `img`, `img_o`, `ai`, `ao`), provide validated completion token parameters as well as usage facts to the same expression evaluation.
- Expressions containing `u(...)` retain task-expression conversion semantics: expression output is USD per request and is multiplied by `QuotaPerUnit` and group ratio. Any token-price component must therefore use explicit `/ 1_000_000` scaling in the expression.
- At submission, continue the bounded established pre-consume reserve where task usage is not yet available. Do not treat absent token inputs as zero actual usage at completion.
- Add validation/tests for both mixed expressions and usage-only/token-only expressions. No expression that the engine accepts may silently discard a referenced variable family.

## Data and compatibility

- This design adds persistent settlement/outbox state and therefore changes the primary database schema. Use GORM models/migrations compatible with SQLite, MySQL >= 5.7.8, and PostgreSQL >= 9.6.
- Verify a fresh database and an upgrade from the latest released schema, run startup/migration twice, and test the separately configured log database/outbox delivery path where applicable.
- Preserve old task rows with no Interactions settlement journal and all non-Interactions legacy billing paths.
- Never expose settlement journal internals, request credentials, or admin diagnostics in customer task responses.

## Failure handling

- Transaction failure leaves balances, task quota, settlement outcome, and log outbox unchanged; retry is safe.
- Log sink failure leaves the committed financial settlement intact and the outbox pending; retry only log delivery.
- Invalid/unavailable usage commits a durable retained-preconsume outcome and admin audit without a user-facing error.
- Quota arithmetic uses checked/saturating project helpers; negative charges and overflow remain impossible.

## Acceptance criteria

1. A simulated interruption before the financial transaction commits can be retried and results in exactly one charge/refund and one outbox event.
2. A simulated interruption after the financial transaction commits cannot reapply the delta; pending log delivery can be retried without another balance change.
3. Missing token or required task usage retains the pre-consume, persists its reason/fact keys, and produces an admin-only audit even at zero delta.
4. A later valid completion usage can settle from the frozen submission snapshot; changed live pricing does not affect it.
5. Mixed `u()` plus token expressions include both variable families and use task/USD quota conversion; token-only and usage-only expressions retain their established conversions.
6. Gemini, Vertex, other endpoints, non-tiered Interactions pricing, task routing/affinity, and existing billing sources retain their existing behavior.
7. Required SQLite, MySQL, and PostgreSQL schema/transaction verification passes before claiming compatibility.
