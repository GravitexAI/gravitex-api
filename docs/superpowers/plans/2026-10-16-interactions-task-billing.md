# Interactions Task Billing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `/v1beta/interactions` honor configured tiered-expression pricing end-to-end while preserving existing pricing behavior for all other modes and endpoints.

**Architecture:** Add endpoint-scoped tiered billing dispatch at the shared task submit boundary, and persist the expression snapshot needed by asynchronous settlement. Reuse existing billing-expression evaluation, quota-safety, task settlement, and log infrastructure; leave Gemini/Vertex request adaptation and all non-Interactions pricing paths unchanged.

**Tech Stack:** Go, Gin, GORM task metadata, `pkg/billingexpr`, existing relay/task billing services, testify.

**Spec:** `docs/superpowers/specs/2026-10-16-interactions-task-billing-design.md`

## Global Constraints

- Limit billing behavior changes to requests marked `NativeInteractions` in the shared task submission/completion pipeline.
- Keep legacy fixed-price, ratio, video per-second, video-token/completion-ratio, per-call, and adapter `OtherRatios` behavior when effective pricing mode is not `tiered_expr`.
- Do not fabricate missing upstream usage, reject solely because usage is absent, or silently substitute a different price formula for configured tiered pricing.
- Preserve the pricing snapshot across asynchronous task settlement and keep existing old-task behavior when no snapshot is present.
- Use `common.*` for JSON marshal/unmarshal; use checked quota conversion and saturation logging for computed quota.
- Do not change schemas or claim three-database compatibility unless the required SQLite/MySQL/PostgreSQL matrix is run.
- Preserve unrelated dirty workspace changes; do not stage or commit unrelated files.

## Review Focus

- Tiered token expression with explicit zero or omitted output limit uses the established bounded estimate; test absent vs explicit zero and expected estimate.
- `param(...)` expression sees the original Interactions body, not Vertex's internal placeholder; test a request-dependent tier match.
- Missing/unparseable completion usage retains the established charge/refund behavior and emits an internal diagnostic; test no fabricated usage and no gateway-side rejection.
- Async settlement after price configuration changes uses the submission snapshot; test snapshot expression and final quota remain stable.
- Non-Interactions task pricing and all non-tiered Interactions billing retain prior behavior; test representative legacy fixed/ratio/video pricing and ordinary task helper behavior.

---

### Task 1: Add endpoint-scoped tiered pre-consumption

**Files:**
- Modify: `relay/relay_task.go`
- Modify: `relay/helper/price.go` only if a narrowly scoped reusable expression-pricing entry point is needed; do not change `ModelPriceHelperPerCall` semantics for other callers.
- Test: `relay/relay_task_test.go` (or existing task relay tests) and `relay/helper/price_test.go`

**Interfaces:**
- Consumes: `RelayInfo.NativeInteractions`, `billing_setting.GetBillingMode`, existing `billingexpr` request resolution/evaluation and checked quota helpers.
- Produces: An Interactions-only initial tiered estimate plus `BillingSnapshot` attached to relay/task billing state; non-tiered requests continue through `ModelPriceHelperPerCall` unchanged.

- [ ] **Step 1: Write failing tests** proving a tiered Interactions model without a legacy ratio/fixed-price entry gets a valid bounded pre-consume estimate; a request-dependent tier sees original request input; omitted output limit uses existing tiered fallback and explicit zero is preserved.
- [ ] **Step 2: Run the focused tests** and confirm they fail because task billing currently dispatches only through `ModelPriceHelperPerCall`.
- [ ] **Step 3: Implement Interactions-only tiered dispatch** before the legacy helper call. Resolve expression inputs from the original request body and headers, obtain prompt estimate/output limit using established helpers and bounds, create the immutable submission snapshot, and attach quota-clamp metadata through existing audit paths.
- [ ] **Step 4: Run focused tests** and confirm tiered pre-consumption passes while non-Interactions and non-tiered cases still use the legacy helper.

### Task 2: Persist snapshot in submitted task billing context

**Files:**
- Modify: `controller/relay.go`
- Test: `controller/lyria_task_state_test.go` or focused `controller/relay_test.go`

**Interfaces:**
- Consumes: Snapshot produced at Task 1 and current `buildSubmittedTask` construction of `model.TaskBillingContext`.
- Produces: Persisted task billing context containing tiered snapshot and the existing model/group/other-ratio inputs; legacy task rows remain compatible.

- [ ] **Step 1: Write failing tests** proving an Interactions submitted task persists the snapshot and pricing context, while a non-Interactions submitted task does not acquire a tiered snapshot merely because of shared task code.
- [ ] **Step 2: Run the tests** and confirm the Interactions snapshot is currently omitted.
- [ ] **Step 3: Copy the snapshot into `TaskBillingContext`** only for the Interactions tiered path, preserving existing serialized fields and per-call flags.
- [ ] **Step 4: Run controller task-state tests** and confirm persisted context plus old-task nil-snapshot behavior.

### Task 3: Settle tiered Interactions usage consistently

**Files:**
- Modify: `relay/relay_task.go`
- Modify: `service/task_billing.go`
- Modify: `service/task_polling.go` only where current snapshot settlement cannot consume the validated completion data
- Test: `relay/relay_adaptor_lyria_test.go` (native GenerateContent usage) and `service/task_billing_test.go`

**Interfaces:**
- Consumes: Persisted `TaskBillingContext.TieredSnapshot`, normalized `TaskInfo` usage and adapter-validated `UsageFacts`.
- Produces: One tiered settlement result used for quota delta/refund and logging; failed/cancelled tasks keep current refund/idempotency behavior.

- [ ] **Step 1: Write failing tests** for synchronous GenerateContent token settlement, async usage-fact settlement where a declared schema exists, unavailable usage, failed task refund, and unchanged non-tiered video/per-call paths.
- [ ] **Step 2: Run focused relay/service tests** and confirm tiered snapshot is not used consistently by current GenerateContent/task settlement paths.
- [ ] **Step 3: Apply the snapshot expression to normalized successful usage** using existing expression APIs and safe quota conversion. For usage expressions, accept only validated adapter facts. For missing required inputs, preserve established preconsume/settlement behavior and attach an internal reason; never synthesize zero facts or fall back to another formula.
- [ ] **Step 4: Run focused tests** and verify final quota, difference/refund, failure idempotency, and legacy pricing behavior.

### Task 4: Align task billing logs and diagnostics

**Files:**
- Modify: `service/task_billing.go`
- Modify: `service/task_billing_test.go`
- Modify: correlated logging code only if required to expose the existing admin-only diagnostic metadata.

**Interfaces:**
- Consumes: Final settlement result and snapshot/fact-evaluation status from Task 3.
- Produces: Task logs that identify configured-expression settlement vs upstream usage settlement and include matched tier/validated inputs or a clear internal “usage unavailable” reason, without exposing pricing internals to user-facing errors.

- [ ] **Step 1: Write failing assertions** for expression billing source, matched tier/facts, missing-usage diagnostic, and equality between logged quota and settled quota.
- [ ] **Step 2: Run focused log tests** and confirm current log representation is incomplete for Interactions tiered billing.
- [ ] **Step 3: Record the effective snapshot and settlement provenance** in the existing task log metadata/admin-only fields; preserve log redaction and existing billing-process format for legacy paths.
- [ ] **Step 4: Run task billing log tests** and verify quota, task row, and log agree.

### Task 5: Verify endpoint and channel boundaries

**Files:**
- Test: `controller/native_interactions_test.go`
- Test: `middleware/native_interactions_test.go`
- Test: existing relay/helper, service, controller, middleware, router regression suites as needed

**Interfaces:**
- Consumes: Completed endpoint-scoped tiered behavior.
- Produces: Regression evidence that billing dispatch is independent of Gemini/Vertex channel and model allowlists, while request adaptation and other endpoints remain unchanged.

- [ ] **Step 1: Add/extend table tests** for Gemini pass-through and Vertex adaptation with tiered billing, plus a generic/allowlist-external Vertex model; assert same billing-mode selection and unchanged request conversion.
- [ ] **Step 2: Run the focused boundary tests** and verify channel/model selection does not gate tiered billing.
- [ ] **Step 3: Run regression suites** for helper/task pricing, task billing/polling, controller/native Interactions, middleware/router, and non-Interactions relay pricing.
- [ ] **Step 4: Run `git diff --check` and inspect the complete diff** to confirm unrelated dirty files and paths remain untouched.

## Verification Commands

Run focused tests first, then the broader relevant suites. Exact package filters may be narrowed to existing test names, but do not omit the boundary categories above:

```bash
GOCACHE=/private/tmp/gravitex-go-build-cache go test ./relay/helper ./relay ./service ./controller ./middleware ./router -run 'Test.*(Tiered|NativeInteraction|ModelPriceHelperPerCall)' -count=1
GOCACHE=/private/tmp/gravitex-go-build-cache go test ./relay/helper ./relay ./service ./controller ./middleware ./router -count=1
git diff --check
```

No DB schema change is planned. If implementation changes database behavior or schema unexpectedly, stop and complete the required fresh/upgrade SQLite, MySQL, and PostgreSQL verification before claiming completion.
