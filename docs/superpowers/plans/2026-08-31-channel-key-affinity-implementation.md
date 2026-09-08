# Channel Key Affinity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend the existing channel affinity feature to bind an existing affinity key to `channel_id + key_index + key_hash`, while preserving old channel-only behavior and exposing the new non-sensitive binding data in both legacy and modern UIs.

**Architecture:** Keep affinity matching, cache lifecycle, and retry decisions in the shared Go service/distributor layer. Store a JSON binding value in the existing HybridCache, use the selected key through the existing request context so every model adapter remains unchanged, and make the old integer cache value readable during migration. Update the embedded legacy/new frontends to display the binding metadata without ever displaying credential material.

**Tech Stack:** Go, Gin, GORM/cachex HybridCache, Go tests, React/TypeScript modern UI, React/JS classic UI, existing settings and usage-log APIs.

**Spec:** `docs/superpowers/specs/2026-08-31-channel-key-affinity-design.md`

## Global Constraints

- Preserve single-key channel behavior and all non-affinity requests.
- Reuse current affinity key sources and cache-key construction.
- Persist only `channel_id`, `key_index`, and non-sensitive `key_hash`; never persist credentials, private keys, JWTs, or access tokens.
- Vertex Service Account hashes use parsed `private_key_id + "\n" + client_email + "\n" + project_id`; ordinary keys use trimmed-string SHA-256.
- A stale binding is cleared; recovery tries another enabled key in the original channel before another channel.
- `skip_retry_on_failure` applies only after an actual channel-and-key affinity hit.
- Keep old cache integer values readable and upgrade them after a successful request.

---

### Task 1: Add binding value and key fingerprint helpers

**Files:**
- Modify: `service/channel_affinity.go`
- Create: `service/channel_affinity_binding_test.go`
- Modify: `service/channel_affinity.go` cache initialization and binding helpers

**Interfaces:**
- Produce `ChannelAffinityBinding` with `ChannelID`, `KeyIndex`, and `KeyHash`.
- Produce deterministic helpers for ordinary strings and parsed Vertex Service Account JSON.
- Produce cache encode/decode behavior that accepts both the new JSON value and legacy integer values.

- [ ] **Step 1: Write failing tests** for ordinary-key hashing, Vertex JSON field-order invariance, missing `private_key_id` fallback, new binding JSON round-trip, and legacy integer decoding.
- [ ] **Step 2: Run** `GOCACHE=/private/tmp/gravitex-go-build-cache go test ./service -run 'ChannelAffinityBinding|KeyHash' -count=1` and confirm the new tests fail because the helpers/type do not exist.
- [ ] **Step 3: Implement** the binding type, SHA-256 helpers, Vertex credential parsing/fingerprint construction, and a JSON codec compatible with the existing HybridCache.
- [ ] **Step 4: Run** the same focused command and confirm it passes.
- [ ] **Step 5: Refactor** only after green, keeping credential text out of errors and logs.

### Task 2: Make channel key selection addressable and preserve context metadata

**Files:**
- Modify: `model/channel.go`
- Modify: `middleware/distributor.go`
- Create/modify: `model/channel_key_affinity_test.go` or the existing channel selection test file

**Interfaces:**
- Add a method that resolves an enabled key by index and verifies its fingerprint.
- Add a public/common selection result carrying channel ID, key index, key string, and hash internally; keep existing `GetNextEnabledKey()` behavior for callers that do not provide a binding.
- Ensure `SetupContextForSelectedChannel()` can initialize a channel with an optional validated key selection while retaining its current signature for existing callers through a small internal helper.

- [ ] **Step 1: Write failing tests** for selecting an enabled key by index, rejecting disabled/out-of-range keys, recovering by matching `key_hash` after key reorder, and preserving ordinary `GetNextEnabledKey()` random/polling behavior.
- [ ] **Step 2: Run** the focused model/middleware tests and confirm failure.
- [ ] **Step 3: Implement** the minimal addressable selection helper and context writes for `ContextKeyChannelMultiKeyIndex` and `ContextKeyChannelKey`.
- [ ] **Step 4: Run** the focused tests and confirm pass.
- [ ] **Step 5: Verify** single-key and non-affinity call sites compile without signature changes.

### Task 3: Upgrade affinity lookup, write-back, and retry recovery

**Files:**
- Modify: `service/channel_affinity.go`
- Modify: `middleware/distributor.go`
- Modify: `controller/relay.go`
- Create/modify: `service/channel_affinity_test.go`, `service/channel_affinity_fixed_ttl_test.go`, and relevant middleware tests

**Interfaces:**
- Change the affinity cache to store `ChannelAffinityBinding` while reading legacy integer values.
- Have affinity lookup return a binding candidate and actual-hit state.
- Write the final successful channel/key binding from request context.
- Add per-request attempted `(channel_id, key_hash)` tracking for retries.

- [ ] **Step 1: Write failing tests** for new binding lookup, first-request legacy upgrade, actual hit versus rule match, fixed TTL with the same key, key-disabled same-channel recovery, cross-channel fallback, and retry de-duplication.
- [ ] **Step 2: Run** the affinity/middleware focused tests and confirm expected failures.
- [ ] **Step 3: Implement** binding-aware lookup and `RecordChannelAffinity()`; only set the skip-retry flag on an actual binding hit.
- [ ] **Step 4: Implement** recovery ordering: original channel/other enabled key, then ordinary channel selection excluding attempted combinations.
- [ ] **Step 5: Run** focused tests and confirm pass.
- [ ] **Step 6: Run** `GOCACHE=/private/tmp/gravitex-go-build-cache go test ./service ./middleware -run 'ChannelAffinity|Affinity|Distributor' -count=1` and record unrelated sandbox listener failures separately if present.

### Task 4: Extend backend logs and cache-management responses

**Files:**
- Modify: `service/channel_affinity.go`
- Modify: `service/log_info_generate.go`
- Modify: `controller/channel_affinity_cache.go`
- Create/modify: relevant service/controller tests

**Interfaces:**
- Add `key_index` and `key_hash` to `other.admin_info.channel_affinity` when a concrete key was selected.
- Extend cache stats/management response data with non-sensitive binding metadata where the existing response shape permits it.
- Preserve old response fields and endpoints.

- [ ] **Step 1: Write failing tests** for log metadata, no-secret assertions, legacy cache stats, and new binding stats.
- [ ] **Step 2: Run** the focused tests and confirm failure.
- [ ] **Step 3: Implement** additive response fields and redaction-safe formatting.
- [ ] **Step 4: Run** focused controller/service tests and confirm pass.

### Task 5: Update modern UI (`web/default`)

**Files:**
- Modify: `web/default/src/features/system-settings/general/channel-affinity/types.ts`
- Modify: `web/default/src/features/system-settings/general/channel-affinity/index.tsx`
- Modify: `web/default/src/features/system-settings/general/channel-affinity/rule-editor-dialog.tsx` only if explanatory copy requires it
- Modify: `web/default/src/features/usage-logs/types.ts`
- Modify: `web/default/src/features/usage-logs/components/columns/common-logs-columns.tsx`
- Modify: modern UI translation files discovered by existing project convention

**Interfaces:**
- Keep existing settings payload keys unchanged.
- Display key index and short hash in affinity detail/log views when supplied by backend.
- Add explanatory text that the feature binds the existing affinity key to a channel and channel credential; never render raw credentials.

- [ ] **Step 1: Add/update UI tests or type-level fixtures** for old affinity payloads without key fields and new payloads with key fields.
- [ ] **Step 2: Run** the narrow frontend test/type command and confirm the new assertions fail if applicable.
- [ ] **Step 3: Implement** additive rendering with fallback `-` for old logs.
- [ ] **Step 4: Run** the modern UI typecheck/build command from its package scripts.

### Task 6: Update legacy UI (`web/classic`)

**Files:**
- Modify: `web/classic/src/pages/Setting/Operation/SettingsChannelAffinity.jsx`
- Modify: `web/classic/src/components/table/usage-logs/UsageLogsColumnDefs.jsx`
- Modify: `web/classic/src/components/table/usage-logs/modals/ChannelAffinityUsageCacheModal.jsx` if cache detail fields are exposed there
- Modify: legacy translation files using existing project conventions

**Interfaces:**
- Preserve existing rule editor JSON shape and cache endpoints.
- Render the additive key index/hash fields for new logs while remaining compatible with old logs.
- Keep old UI copy and controls functional when the backend returns only legacy fields.

- [ ] **Step 1: Add/update** legacy UI fixtures or component tests for old/new affinity metadata.
- [ ] **Step 2: Run** the legacy narrow test/build command and confirm the new behavior is not present before implementation.
- [ ] **Step 3: Implement** additive display and explanatory copy.
- [ ] **Step 4: Run** the legacy build/lint command.

### Task 7: Documentation, compatibility audit, and final verification

**Files:**
- Modify: `docs/superpowers/specs/2026-08-31-channel-key-affinity-design.md`
- Modify: `docs/渠道亲和性配置与命中规则说明.md`
- Modify: any generated/API documentation file only if the repository build requires it

- [ ] **Step 1: Document final cache format, legacy migration, Vertex fingerprinting, UI fields, recovery order, and security redaction.
- [ ] **Step 2: Search all Go/frontend references with `rg` to verify no adapter stores or displays raw credentials.
- [ ] **Step 3: Run Go formatting and focused tests.
- [ ] **Step 4: Run modern and legacy frontend builds.
- [ ] **Step 5: Run `git diff --check`, inspect `git diff`, and verify only scoped files changed.
- [ ] **Step 6: Stage the related files and report exactly what was staged, with test/build results and any sandbox-limited checks clearly separated.
