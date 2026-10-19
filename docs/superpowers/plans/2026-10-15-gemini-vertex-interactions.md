# Gemini / Vertex Interactions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans or superpowers:subagent-driven-development to implement this plan task-by-task.

**Goal:** Add channel-aware support for `POST /v1beta/interactions` (and its Interaction lifecycle operations) for Gemini and Vertex without changing existing model/channel behavior outside this endpoint.

**Architecture:** Split generic Interactions requests from the current video-task body conversion while retaining existing Lyria/Omni/Veo compatibility branches. After channel selection, Gemini forwards the Interaction protocol unchanged and Vertex adapts the upstream resource URL/authentication. Normalize official usage into existing billing concepts; settle with an Interaction-scoped funding path, persist async settlement/log delivery state, and leave shared billing/log writers unchanged for other channels.

**Tech Stack:** Go 1.22+, Gin, GORM v2, existing `BillingSession`/funding abstractions, existing task and log models, Gemini/Vertex channel adapters.

**Spec:** `docs/superpowers/specs/2026-10-15-gemini-vertex-interactions-channel-adaptation.md`

## Global Constraints

- Do not introduce a model allowlist as endpoint admission; choose upstream adaptation only after channel selection.
- Preserve the existing Lyria/Omni/Veo Interaction behavior and all non-Interaction routes and billing semantics.
- Missing or unparseable usage uses the existing task configured-estimate/pre-consume fallback and is logged as estimated; it is not zero usage and does not create a pending-reconciliation billing state.
- Never let invalid or unbounded upstream usage create a negative charge; use checked quota conversion and record saturation in admin-only log metadata.
- Do not change `SettleBilling`, `BillingSession`, `RecordConsumeLog`, or `CreateLog` behavior globally; add Interaction-scoped logic.
- Use `common.*` JSON wrappers for marshal/unmarshal in business code.
- If schema changes are needed, verify real SQLite, MySQL >= 5.7.8, and PostgreSQL >= 9.6; test fresh and representative upgrade databases and run migrations twice.
- Keep Gemini public request/response semantics unchanged; Vertex uses the official `v1beta1/projects/{project}/locations/global/interactions` upstream path.
- Do not expose `other.admin_info`, credentials, or raw authorization headers to customers.

## Review Focus

- Legacy Lyria/Omni/Veo requests still take their existing path and retain response/log classification.
- A model not in the current Interaction model list reaches the selected Gemini or Vertex channel adapter rather than being rejected by that list.
- Vertex resource path construction uses the configured project and fixed global location; caller-controlled project/location cannot escape channel scope.
- Missing, explicit-zero, malformed, negative, overflowed, aggregate-only, modality, cached, thought, and tool usage remain distinguishable and never double-charge.
- Duplicate terminal polling, duplicate SSE terminal events, log persistence failure, and process restart do not duplicate funding effects; any uncovered upstream duplicate-submit ambiguity is represented separately from usage fallback.

---

### Task 1: Route and classify generic Interactions without changing legacy branches

**Files:**
- Modify: `middleware/native_interactions.go`
- Modify: `router/video-router.go` only if route registration needs a distinct handler
- Test: `middleware/native_interactions_test.go` and/or focused controller route tests

**Interfaces:**
- Produces: middleware context values for original path, exact raw request body, execution mode, and `native_interactions_generic`; existing legacy context keys remain unchanged.

- [ ] **Step 1: Add failing behavior tests** for raw body/path preservation on generic text, image/audio input and unknown model; assert Lyria/Omni/Veo still use the current conversion path.
- [ ] **Step 2: Run the targeted middleware tests** and verify the generic cases fail before implementation while legacy fixtures pass.
- [ ] **Step 3: Implement channel-neutral generic request classification** that does not use a model directory to reject requests; retain model checks only to preserve existing specialized behavior.
- [ ] **Step 4: Run targeted middleware and router tests** and assert `/v1/video/generations` and `/v1/videos` behavior is unchanged.

Boundary review note: generic requests keep both the raw Interactions payload and the prior task conversion/error. The selected Gemini/Vertex channel consumes the raw payload; non-target channels restore their old converted payload or validation error. The selection must be applied on every retry, not only in pre-controller middleware. Native Interaction fetch uses a separate raw-response marker so the legacy Lyria and Seedance response modes do not change.

### Task 2: Add channel-aware Gemini passthrough and Vertex transport adaptation

**Files:**
- Create or modify: focused Interaction transport adapter under `relay/channel/`
- Modify: `controller/native_interactions.go`, `controller/relay.go`, and relay selection only where required to dispatch the generic mode
- Test: adapter/controller tests with HTTP test servers

**Interfaces:**
- Consumes: Task 1 context contract and selected `RelayInfo.ChannelType` plus mapped upstream model.
- Produces: an upstream Interaction request preserving raw JSON and method semantics; Gemini URL remains `/v1beta/interactions`; Vertex URL is built from configured project at `/v1beta1/projects/{project}/locations/global/interactions`.

- [ ] **Step 1: Add failing adapter tests** for Gemini unchanged body/path, Vertex resource URL/auth, mapping, caller location/project isolation, and preservation of upstream Interaction response bytes.
- [ ] **Step 2: Run adapter tests** and verify the new channel cases fail without affecting existing Lyria/Omni/Veo tests.
- [ ] **Step 3: Implement an Interaction-specific transport dispatch after channel selection**; do not alter ordinary Gemini GenerateContent or Vertex GenerateContent adapters.
- [ ] **Step 4: Add GET/cancel/continuation path tests** proving background operations stay bound to the channel and project selected at creation.
- [ ] **Step 5: Run controller, relay, router, and middleware targeted suites.**

### Task 3: Normalize Interaction usage and preserve configured-estimate fallback

**Files:**
- Create: Interaction usage normalization in a focused `relay/` or `service/` file
- Modify: `relay/common/relay_info.go` or `TaskInfo` only if normalized fields cannot remain local to Interaction billing
- Test: focused usage normalization and billing tests

**Interfaces:**
- Produces: `InteractionUsage` with explicit availability/source, aggregate input/output, modality splits, cached/thought/tool detail, and terminal status; parser distinguishes missing from explicit zero.
- Billing fallback: when no trustworthy normalized usage exists, use the existing `ModelPriceHelperPerCall` estimate/pre-consume as final configured estimate and mark the source in billing/log metadata.

- [ ] **Step 1: Add table tests** for official total/modality/cache/thought/tool fields, aggregate-only versions, explicit zero, missing usage, malformed values, negatives, overflow, and inconsistent totals.
- [ ] **Step 2: Run the normalization tests** and verify unsupported/invalid values cannot be treated as actual usage.
- [ ] **Step 3: Implement normalization** without deriving thought/tool usage from unexplained residuals or adding overlapping aggregate/subcomponent values.
- [ ] **Step 4: Add quota tests** proving trusted usage uses checked conversion and missing/unusable usage retains the configured estimate without a pending billing state.
- [ ] **Step 5: Run focused relay usage and quota tests.**

### Task 4: Implement Interaction-scoped source-aware settlement and persistence

**Files:**
- Create: Interaction settlement service/journal/outbox model files as required by the chosen schema
- Modify: `model/main.go`, `model/task.go`, `service/funding_source.go` or narrowly scoped callers only when needed
- Test: settlement service/model tests and migration tests

**Interfaces:**
- Settlement key uniquely identifies one gateway Interaction charge; journal records funding source, pre-consumed amount, final quota/source, token/user/channel/model snapshot, and independent funding/log-delivery states.
- Existing shared helpers retain their current behavior; Interaction settlement calls source-specific wallet/subscription effects and updates usage/token statistics once.

- [ ] **Step 1: Add failing tests** for wallet and subscription positive/negative/zero deltas, duplicate settlement key, partial failure, retry, token quota adjustment, and exactly-once usage statistics.
- [ ] **Step 2: Add failing persistence tests** for journal state transitions, exclusive settlement claim, outbox replay, and duplicate log key handling.
- [ ] **Step 3: Implement the additive journal/outbox schema** with unique settlement/delivery identities and cross-database-compatible GORM definitions.
- [ ] **Step 4: Implement idempotent funding effects** so a crash between funding adjustment and journal update cannot apply the same wallet/subscription delta twice; do not claim idempotency if an effect cannot be transactionally associated with its key.
- [ ] **Step 5: Implement log delivery recovery** so a failed/uncertain log write can be retried without repeating funding; prove how durable acceptance and deduplication work for the configured log backend, including ClickHouse/ByteHouse buffering.
- [ ] **Step 6: Run migration verification** on fresh and representative upgrade databases for SQLite, MySQL >= 5.7.8, and PostgreSQL >= 9.6; run startup/migration twice and verify existing task/log data remains intact.

### Task 5: Integrate synchronous, SSE, and background Interaction lifecycle

**Files:**
- Modify: `controller/native_interactions.go`, `controller/relay.go`, `controller/task_video.go` only through an Interaction-specific call path
- Modify: task persistence only if required by Task 4 journal contract
- Test: controller lifecycle tests with fake Gemini/Vertex upstreams and explicit billing fixtures

**Interfaces:**
- Consumes: channel adapter from Task 2, `InteractionUsage` from Task 3, settlement/outbox API from Task 4.
- Produces: official Interaction response shape for synchronous, stream, background/store, GET, cancel, and continuation operations; logs one final consumption record when enabled.

- [ ] **Step 1: Add failing end-to-end controller tests** for synchronous Gemini/Vertex, SSE passthrough with terminal usage, background creation then fetch/cancel, channel affinity, and preserved legacy model behavior.
- [ ] **Step 2: Add failing billing lifecycle tests** for usage-based settlement, configured-estimate fallback, explicit zero according to existing route behavior, proven upstream rejection/refund, and uncertain creation retry suppression.
- [ ] **Step 3: Integrate the generic Interaction handler** while keeping existing specialized task submit/fetch paths unchanged.
- [ ] **Step 4: Persist task/channel/settlement association** before asynchronous work can outlive the request context; ensure terminal polling claims settlement once.
- [ ] **Step 5: Run controller, relay, middleware, service, and model targeted suites.**

### Task 6: Define log contract and verify no regressions outside Interactions

**Files:**
- Modify: `service/task_billing.go` or add an Interaction-specific log builder; avoid global changes to `model/log.go`
- Modify Java/Admin/CLI only if the existing flat fields cannot represent the required user-facing billing details
- Test: Go log contract tests and any intentionally changed Java/frontend contract tests

**Interfaces:**
- Consumption logs retain existing Java-recognized aggregate/flat fields, request ID/upstream ID relationship, and legacy `is_task` behavior; estimate fallback is explicitly identified as configured estimate.

- [ ] **Step 1: Add log contract tests** for recognized aggregate/modality fields, admin-only diagnostic placement, estimate-vs-upstream usage source, and legacy native Interaction IDs/classification.
- [ ] **Step 2: Implement only the required Interaction log builder fields**; do not alter global `RecordConsumeLog`/`CreateLog` semantics for other channels.
- [ ] **Step 3: Run regression tests** for OpenAI, Claude, Gemini, Vertex GenerateContent, Lyria/Omni/Veo, video task billing/refunds, and existing log readers.
- [ ] **Step 4: Record exact database engine versions, migration commands, and results**; report any unavailable database backend as a blocker rather than claiming compatibility.

## Plan self-review

- Route/channel behavior, response modes, usage normalization, estimate fallback, funding delta, asynchronous restoration, log reading, and legacy isolation each have an owning task.
- No change is allowed to shared billing/log behavior unless a task proves it is Interaction-scoped or separately approved.
- Schema and outbox implementation is a release gate: if log-backend deduplication or idempotent funding cannot be proven, do not describe the resulting implementation as crash-safe/exactly-once.
- The product-facing UI remains unchanged unless the existing Java/Admin/CLI contract cannot correctly display the required existing flat billing fields.
