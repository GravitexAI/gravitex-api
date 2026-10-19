# Interactions Task Settlement Reliability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make tiered `/v1beta/interactions` async billing recoverable/idempotent, persist usage-unavailable diagnostics, and correctly evaluate mixed task-usage/token expressions.

**Architecture:** Add a task-keyed primary-database settlement journal and log outbox. A single primary-database transaction applies financial deltas, updates task quota and settlement state, and enqueues a uniquely identified log event. Recovery retries pending settlement transactions; a separate delivery path retries outbox logs without reapplying financial changes. Changes remain scoped to Interactions tiered tasks.

**Tech Stack:** Go, GORM v2, SQLite/MySQL/PostgreSQL, existing quota and billing-expression helpers, testify.

**Spec:** `docs/superpowers/specs/2026-10-16-interactions-task-settlement-reliability-design.md`

## Global Constraints

- Limit new settlement behavior to `/v1beta/interactions` tasks with a tiered billing snapshot.
- Preserve Gemini pass-through, Vertex adaptation, routing/affinity, and every non-Interactions billing path.
- Missing usage retains the charged pre-consume; never fabricate usage or reject solely due to missing completion usage.
- Freeze and reuse submission-time expression/request/pricing inputs.
- Mixed `u()` and token expressions evaluate both variable families; `u()` expressions keep USD-per-request conversion, and token components use explicit `/ 1_000_000` scaling.
- Use common JSON wrappers and checked quota conversion; never permit negative charge/overflow.
- Schema/transaction changes require real SQLite, MySQL >= 5.7.8, and PostgreSQL >= 9.6 verification, fresh and upgrade migration checks, and idempotent startup checks.
- Preserve old task rows and unrelated dirty workspace files; do not commit or stage unrelated files.

## Review Focus

- Crash before settlement transaction commit: retry must apply one financial delta and enqueue one outbox event.
- Crash after financial transaction commit but before log delivery: retry must deliver the same event without changing balances again.
- Missing usage, zero quota delta, and missing task result: durable admin audit must exist while customer-facing data remains clean.
- Changed live pricing after submission: recovery uses the frozen snapshot and request inputs.
- Mixed task/token expression: both referenced variable families affect the result under USD-per-request conversion.

---

### Task 1: Map transactional billing writes and add idempotent persistence models

**Files:**
- Modify: `model/main.go` (register new GORM models)
- Create or modify: `model/task_billing_settlement.go`
- Test: `model/task_billing_settlement_test.go`
- Modify: existing migration/schema tests as needed

**Interfaces:**
- Consumes: `model.Task`, task IDs, existing `BillingSource` and quota ownership semantics.
- Produces: task-keyed settlement journal/outbox models with cross-dialect GORM persistence and atomic create/update helpers; unique operation IDs for settlement and log delivery.

- [ ] **Step 1: Write failing model tests** for unique task settlement identity, legal state transitions, outbox event identity, and serialization of the frozen settlement payload.
- [ ] **Step 2: Run focused model tests** and confirm missing persistence/state behavior.
- [ ] **Step 3: Inventory existing funding writes** for wallet, subscription, token, user, and channel quota. Define transaction-taking helpers without changing their existing callers' behavior.
- [ ] **Step 4: Implement the journal/outbox GORM models and state helpers** using only portable GORM operations and no dialect-specific schema assumptions.
- [ ] **Step 5: Run focused model tests and `go test ./model -run 'Test.*TaskBillingSettlement' -count=1`.**

### Task 2: Make Interactions tiered settlement atomic and recoverable

**Files:**
- Modify: `service/task_billing.go`
- Modify: `service/task_polling.go`
- Modify: `model/task.go` only if a narrowly scoped persistence helper is required
- Test: `service/task_billing_test.go`, `service/task_polling_test.go` or existing task polling tests

**Interfaces:**
- Consumes: Task 1 settlement journal helpers; frozen `TaskBillingContext`; terminal task result.
- Produces: an Interactions-only settlement operation that atomically adjusts the correct billing source, task quota, journal state, and log outbox event; recoverable pending settlement selection.

- [ ] **Step 1: Write failing tests** for wallet and subscription settlement idempotency, token quota delta idempotency, transaction rollback, pre-commit retry, and post-commit retry.
- [ ] **Step 2: Run focused settlement tests** and verify they expose the current success-before-settlement crash window and duplicate-delta risk.
- [ ] **Step 3: Implement a transaction-taking settlement primitive** for the billing sources discovered in Task 1; atomically write the task quota and journal's terminal outcome with those balance changes.
- [ ] **Step 4: Change Interactions tiered polling** to enqueue/execute the journal transaction and add a recovery query that retries terminal Interactions tasks whose settlement is pending. Keep legacy task settlement untouched.
- [ ] **Step 5: Run focused service settlement and polling tests**, including concurrent workers attempting the same task.

### Task 3: Persist missing-usage outcomes and deliver billing logs idempotently

**Files:**
- Modify: `service/task_billing.go`
- Modify: `service/task_polling.go`
- Modify: log model/repository code for stable outbox operation ID deduplication
- Test: `service/task_billing_test.go` and outbox delivery tests

**Interfaces:**
- Consumes: Task 1 outbox; Task 2 settlement outcomes and deltas.
- Produces: durable usage-unavailable outcome and admin-only billing audit, including zero-delta cases; retryable exactly-once log delivery by operation ID.

- [ ] **Step 1: Write failing tests** for missing token usage, missing required `u()` facts, nil task result, zero-delta audit, customer redaction, and log sink failure/retry deduplication.
- [ ] **Step 2: Run the tests** and confirm current code only emits an ephemeral warning or omits a durable record.
- [ ] **Step 3: Persist the usage-unavailable reason and missing fact keys** in the settlement journal while preserving the pre-consumed quota.
- [ ] **Step 4: Implement outbox delivery** with a stable operation ID and a sink-side deduplication boundary; do not rerun financial settlement when delivery retries.
- [ ] **Step 5: Run billing-log, redaction, and delivery retry tests.**

### Task 4: Support mixed usage/token expressions

**Files:**
- Modify: `relay/relay_task.go`
- Modify: `service/task_billing.go`
- Modify: `service/task_polling.go`
- Test: `relay/relay_task_test.go`, `service/task_billing_test.go`

**Interfaces:**
- Consumes: `TaskInfo` normalized token parameters, validated usage facts, and the submission snapshot.
- Produces: completion evaluation that passes both parameter sets when referenced; task-expression conversion remains USD/request.

- [ ] **Step 1: Write failing tests** for token-only, usage-only, and mixed expressions, asserting exact quota and tier selection; include absent usage and explicit zero usage cases.
- [ ] **Step 2: Run focused tests** and confirm mixed expressions currently receive empty token parameters.
- [ ] **Step 3: Pass normalized token parameters and validated usage facts together** to evaluation when the expression uses both; retain task/USD conversion and reserve pre-consume semantics.
- [ ] **Step 4: Run focused relay and service expression tests.**

### Task 5: Verify schema lifecycle, recovery boundaries, and endpoint isolation

**Files:**
- Modify: migration/schema fixtures only when needed
- Test: model/service/controller/middleware/router regression suites
- Modify: design/plan docs only if implementation discovers a necessary contract refinement

**Interfaces:**
- Consumes: Tasks 1-4 implementations.
- Produces: evidence that migrations are portable/idempotent, billing recovery is safe, and non-Interactions behavior remains unchanged.

- [ ] **Step 1: Run SQLite fresh-schema and latest-release upgrade tests**, then run startup/migration twice and verify settlement uniqueness and outbox constraints.
- [ ] **Step 2: Run the same fresh/upgrade/idempotency checks against real MySQL >= 5.7.8 and PostgreSQL >= 9.6 instances.**
- [ ] **Step 3: Verify separately configured log database delivery and deduplication behavior.**
- [ ] **Step 4: Run Interactions/Gemini/Vertex boundary tests and representative non-Interactions task billing regressions.**
- [ ] **Step 5: Run `git diff --check`, focused package tests, and `go build ./...`; report any pre-existing failures separately and do not claim database compatibility if any required matrix item is unavailable.**

## Execution Notes

- Implementation must first prove which existing quota mutation helpers can accept a shared transaction. If a funding source uses a different database or external service, stop and revise the design before coding that source; do not claim atomicity across independent stores.
- No cleanup, reset, broad staging, or commit is authorized by this plan.
