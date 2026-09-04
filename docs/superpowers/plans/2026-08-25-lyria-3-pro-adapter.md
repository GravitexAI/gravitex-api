# Lyria 3 Pro Adapter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an isolated asynchronous `lyria-3-pro-preview` Interactions adapter with audio result handling and fixed-price billing.

**Architecture:** Branch only on the exact Lyria model in native Interactions middleware. Route Lyria through a dedicated task adapter and polling parser, while preserving the existing Omni/video branch and shared `tasks` persistence. Use existing per-call price calculation and mark the persisted billing context as per-call.

**Tech Stack:** Go, Gin, GORM task model, existing relay `TaskAdaptor`, Google Interactions API.

**Spec:** `docs/superpowers/specs/2026-08-25-lyria-3-pro-adapter-design.md`

## Global Constraints

- Do not change behavior for `gemini-omni-flash-preview` or existing video models.
- Only `lyria-3-pro-preview` selects the Lyria branch.
- Lyria accepts text/image input and returns audio/text; reject audio/video input.
- Lyria uses fixed per-call billing and never video duration/resolution billing.

---

### Task 1: Add failing isolation and payload tests

**Files:**
- Modify: `middleware/native_interactions_test.go`
- Create: `relay/channel/task/lyria/adaptor_test.go`

- [ ] **Step 1: Add a test proving Lyria conversion preserves the Interactions model and image input without video metadata.**
- [ ] **Step 2: Add a test proving Omni conversion remains video-specific.**
- [ ] **Step 3: Add parser tests for queued, failed, and completed Lyria `steps` responses.**
- [ ] **Step 4: Run the focused tests and confirm the new tests fail for the missing Lyria behavior.**

### Task 2: Implement the isolated Lyria adapter

**Files:**
- Create: `relay/channel/task/lyria/adaptor.go`
- Modify: `relay/channel/adapter.go`
- Modify: `relay/relay_adaptor.go`
- Modify: `constant/task.go`

- [ ] **Step 1: Add the Lyria task platform/adaptor registration without changing existing platform mappings.**
- [ ] **Step 2: Implement request validation, Interactions URL/header/body construction, and submit response parsing.**
- [ ] **Step 3: Implement polling GET and `steps` parsing, including base64 audio data URI and lyrics metadata.**
- [ ] **Step 4: Run the Lyria adapter tests and confirm they pass.**

### Task 3: Branch native Interactions only for Lyria

**Files:**
- Modify: `middleware/native_interactions.go`
- Modify: `controller/native_interactions.go`
- Modify: `router/video-router.go`

- [ ] **Step 1: Add model-specific Lyria request conversion and reject unsupported media.**
- [ ] **Step 2: Preserve existing Omni conversion and synchronous/background handling.**
- [ ] **Step 3: Return audio and lyrics in the Lyria Interaction response shape from persisted task data.**
- [ ] **Step 4: Run middleware/controller focused tests.**

### Task 4: Ensure fixed-price billing is isolated

**Files:**
- Modify: `controller/relay.go`
- Modify: `service/task_polling.go`
- Modify: `service/task_billing_test.go`

- [ ] **Step 1: Add a failing test asserting Lyria tasks persist `PerCallBilling=true` and skip completion adjustment.**
- [ ] **Step 2: Implement model-scoped per-call marking if the fixed price resolver does not already provide it.**
- [ ] **Step 3: Run billing tests and verify existing non-Lyria task settlement remains unchanged.**

### Task 5: Verify the complete affected surface

- [ ] **Step 1: Run `gofmt` on changed Go files.**
- [ ] **Step 2: Run focused middleware, Lyria adapter, controller, and service tests.**
- [ ] **Step 3: Run the full Go test suite with the project cache workaround if required.**
- [ ] **Step 4: Review `git diff` and report exact files and verification results.**
