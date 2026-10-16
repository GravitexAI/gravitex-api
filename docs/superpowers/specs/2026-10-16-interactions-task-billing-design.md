# Interactions Task Billing Design

## Goal

Make model pricing behavior complete and internally consistent for requests entering the `/v1beta/interactions` task path, including models configured for tiered expression billing, without changing other endpoints or breaking existing model-specific billing.

## Confirmed Scope and Constraints

- Scope is requests entering `/v1beta/interactions` and then the shared task submission/completion pipeline. The Vertex resource-shaped Interactions route is included only where it shares this same pipeline.
- Gemini's Interactions request remains pass-through and Vertex request adaptation remains unchanged. Billing selection must not depend on the upstream request format or model allowlists.
- Audit model families by their billing behavior, not by maintaining an exhaustive model-name pricing allowlist. Cover native GenerateContent text/image models, Omni/Veo and other asynchronous task models, Lyria, and generic Vertex task models.
- Existing fixed-price, model-ratio, per-second video, video-token-ratio/completion-ratio, per-call, and adapter `OtherRatios` behavior remains the default when the model is not configured for tiered expressions.
- When a model is configured for `tiered_expr`, apply its expression consistently to applicable pre-consumption, final settlement, refunds/differences, and logs. Do not silently substitute a different price formula.
- If upstream usage is unavailable or cannot be normalized, do not invent usage and do not reject a model solely for missing usage. Preserve the path's established settlement behavior and make the billing source/usage limitation visible in internal logs.
- Preserve the existing snapshot principle: the expression and relevant pricing inputs used at submission must remain available for asynchronous settlement even if the administrator later edits pricing.
- No changes to billing semantics for non-Interactions endpoints.

## Current Code Findings

1. `middleware.NativeInteractions` maps the endpoint into `/v1/video/generations`; `controller.NativeInteractionsSubmit` delegates to `RelayTask`.
2. `RelayTask` computes its initial task charge using `ModelPriceHelperPerCall`. That helper recognizes fixed price, existing video price families, and model ratio, but does not dispatch on `billing_setting.GetBillingMode`.
3. The ordinary relay `ModelPriceHelper` does dispatch tiered expressions and creates a `BillingSnapshot`; the task helper does not currently create an equivalent snapshot for this Interactions route.
4. Native GenerateContent task settlement has a custom token-usage path that calculates from `ModelRatio`/completion ratios. It does not settle from a tiered expression snapshot.
5. Generic asynchronous task settlement can consume `TaskBillingContext.TieredSnapshot` usage facts, but the Interactions submission path must be audited for snapshot construction, usage-fact provenance, and correct expression semantics before relying on that mechanism.
6. Task billing logs already include an optional tiered snapshot. The Interactions path must ensure logs describe the actual pricing mode and calculation rather than only the preliminary estimate.

## Design

### 1. Endpoint-scoped billing dispatch

Add tiered-expression handling at the Interactions task billing boundary. Leave shared non-task pricing helpers and all non-Interactions routing behavior unchanged. Existing model-specific pricing remains authoritative whenever the selected model's effective pricing mode is not `tiered_expr`.

### 2. Expression input and pre-consumption

- Capture the original Interactions request as the request input for `param(...)`/header-aware expressions; do not evaluate against an internal placeholder body used only for channel selection.
- For token expressions, construct a conservative estimate from the available prompt-token estimate and requested output limit, reusing the established tiered pre-consumption fallback when the request omits an output limit.
- For usage-fact expressions, use only validated facts exposed by the selected adapter/plugin. Reuse existing declared usage schemas and bounds where present. Do not infer provider usage facts from model names.
- Snapshot the expression, model identity, group ratio, request input needed by the expression, and estimate at submission so retries and later settlement use a stable billing contract.
- Keep quota conversion, clamp auditing, pre-consumption, and funding-source handling on the existing safe billing primitives.

### 3. Completion settlement

- For successful synchronous GenerateContent interactions, normalize upstream token usage—including image/audio/cache modality fields where present—and evaluate token expressions against actual usage.
- For asynchronous task interactions, evaluate usage-fact expressions only from validated completion facts returned by the existing adapter/polling contract. When a configured expression's inputs are unavailable, preserve the established pre-consumption/settlement behavior; do not fabricate zero usage or silently run a different expression.
- Preserve existing per-second/video-token/per-call settlement precedence for models not configured as tiered. Tiered mode is an explicit model pricing choice and must be handled before the legacy fallback for that request.
- On failed/cancelled tasks, retain existing refund/idempotency semantics. Do not evaluate successful-usage settlement for failed tasks.

### 4. Logs and observability

- Persist the effective tiered snapshot and matched tier/usage inputs for the Interactions task lifecycle.
- Distinguish configured-expression settlement from upstream usage-based settlement, and record when an expression could not be evaluated due to unavailable usage.
- Keep user-visible errors free of admin-only pricing internals; put diagnostic details in admin-only log metadata and correlated gateway logs.
- Ensure logs, quota deltas, task rows, and wallet/subscription/token settlement all reflect the same final quota.

## Compatibility and Boundaries

- Do not modify `/v1/chat/completions`, `/v1/responses`, image/audio endpoints, or existing video/task endpoints' billing behavior.
- Do not change Gemini pass-through, Vertex conversion, model routing, channel affinity, or the channel retry policy as part of this work.
- Do not make an allowlist a prerequisite for tiered billing. Model pricing mode plus available validated usage determines the billing path.
- Do not bypass billing failures by inserting a model ratio or fixed-price fallback when tiered billing is configured.
- Preserve old task rows without tiered snapshots and preserve their current settlement behavior.

## Required Verification

- Regression tests prove a tiered Interactions model with no legacy ratio/fixed-price entry no longer fails with `model_price_error` solely because the task helper ignored `tiered_expr`.
- Table-driven tests cover fixed-price, ratio, tiered token expressions, usage-fact expressions where a schema exists, missing usage, and existing per-second/video-token/per-call pricing.
- Exercise Gemini, Vertex, and generic task channel selections while asserting that billing mode selection is channel-independent and request adaptation is unchanged.
- Verify final quota, refund/difference behavior, persisted task billing snapshot, billing-process logs, and admin-only diagnostic metadata.
- Run relevant relay/helper, task billing/polling, controller/native Interactions, and regression tests for non-Interactions task and relay pricing paths.
- Do not claim database compatibility unless the project-required SQLite/MySQL/PostgreSQL verification is run; this design expects no schema change.

## Open Implementation Detail

Before implementation, inventory which Interactions adapters provide normalized token usage versus validated usage facts, and choose the narrowest shared task-billing interfaces that support those sources without changing unrelated task platforms. This inventory must not change the approved behavior or scope above.
