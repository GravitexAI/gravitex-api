# User Request Headers Log Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add per-user configuration for recording inbound client request headers into `logs.other`, with disabled, selected, and all modes, while preserving old `users.setting` and old log compatibility.

**Architecture:** Store a value-type `request_headers_log` object inside the existing user `setting` JSON. At log creation, read the current user setting, select either all inbound headers or a normalized allow-list, and merge the original key/value pairs under `other.client_request_headers`; old or malformed configuration and old logs resolve to empty/default values without panics. The classic and default admin user editors expose the same configuration model.

**Tech Stack:** Go/Gin/GORM, existing `common.Marshal`/`common.Unmarshal`, Go `testify`, React/TypeScript, classic React JSX, existing i18n systems, Bun.

## Global Constraints

- `enabled=false` records no request headers.
- `mode=all` records all inbound client request headers with original key/value; `mode=selected` records only the configured names with original key/value.
- Do not capture upstream-generated headers; use the request received by the Go gateway.
- Preserve the existing `users.setting` JSON fields and old rows; missing, null, empty, or malformed nested configuration must default to disabled.
- Preserve existing `logs.other` keys and tolerate empty, null, malformed, or missing `client_request_headers` when reading old logs.
- Header names are normalized for matching, but stored keys preserve the original inbound header key.
- Use the repository JSON wrappers for business JSON marshal/unmarshal operations.
- Do not record headers for non-user/system logs unless the existing log call has a user ID and request context; consume/request logs are the primary scope.

---

### Task 1: Add configuration types and normalization tests

**Files:**
- Modify: `relaykit/dto/user_settings.go`
- Create: `service/request_headers_log.go`
- Test: `service/request_headers_log_test.go`

**Interfaces:**
- Produces `dto.RequestHeadersLogSetting` with `Enabled`, `Mode`, and `Headers`.
- Produces a helper that returns a safe normalized configuration and a helper that filters inbound `http.Header` values for a user.

- [ ] **Step 1: Write failing table tests** for missing config, invalid mode, selected matching case-insensitively, all mode, duplicate configured names, and original key/value preservation.
- [ ] **Step 2: Run the focused Go test** and confirm it fails because the type/helper is absent.
- [ ] **Step 3: Add the value-type DTO and normalization/filter helpers**, defaulting invalid/missing data to disabled selected mode and preserving original inbound header keys/values in output.
- [ ] **Step 4: Run the focused test** and confirm it passes.

### Task 2: Persist user configuration and enrich consume logs

**Files:**
- Modify: `controller/user.go`
- Modify: `model/log.go`
- Modify: `relay/common/relay_info.go` only if a shared inbound-header accessor is needed
- Test: `controller/user_update_test.go` or a focused service/model test

**Interfaces:**
- `UpdateUser` accepts `setting.request_headers_log` while preserving all other setting fields.
- `RecordConsumeLog` merges `other.client_request_headers` without replacing existing `other` values.

- [ ] **Step 1: Add a failing persistence/log enrichment test** proving a configured selected user gets original headers in `logs.other`, while an old empty setting produces no field.
- [ ] **Step 2: Run the focused test** and confirm the new behavior fails.
- [ ] **Step 3: Extend `updateUserRequest` and merge the nested setting field** using the existing whitelist approach, invalidate the user cache after change, and add request-header enrichment in the common consume-log path.
- [ ] **Step 4: Add handling for direct user request log paths** that bypass `RecordConsumeLog` only when they are user-scoped and have a request context; leave system logs unchanged.
- [ ] **Step 5: Run the focused backend tests** and confirm they pass.

### Task 3: Add classic admin editor configuration

**Files:**
- Modify: `web/classic/src/components/table/users/modals/EditUserModal.jsx`
- Modify: classic locale files for every supported locale if required by the existing locale workflow

**Interfaces:**
- The editor submits `setting.request_headers_log` as part of `PUT /api/user/`.
- UI offers disabled/all/selected modes and selected common header names plus custom names.

- [ ] **Step 1: Add/update a focused frontend behavior test** if the classic test setup supports this component; otherwise verify the payload transform with the smallest existing testable helper.
- [ ] **Step 2: Confirm the new test fails** before implementation.
- [ ] **Step 3: Add the form controls**, initialize missing configuration safely, normalize selected names before submit, and keep raw values unmodified because both modes intentionally store original values.
- [ ] **Step 4: Run the classic frontend test/lint command** for affected files.

### Task 4: Add default admin editor parity

**Files:**
- Modify: `web/src/features/users/components/users-mutate-drawer.tsx`
- Modify: `web/src/features/users/lib/user-form.ts`
- Modify: `web/src/features/users/types.ts`
- Modify: `web/src/features/users/api.ts` only if the typed payload needs a shared setting type
- Modify: `web/src/i18n/locales/*.json` for all supported locales

**Interfaces:**
- Default editor uses the same `request_headers_log` JSON shape and safely handles missing/null data.

- [ ] **Step 1: Add a failing transform test** for default values, all mode, selected mode, and missing settings.
- [ ] **Step 2: Run the focused Bun/Vitest test** and confirm it fails.
- [ ] **Step 3: Implement the typed form fields and payload transform** without converting user IDs to numbers.
- [ ] **Step 4: Run affected frontend tests, typecheck, and lint.**

### Task 5: Make log consumers null-safe and verify compatibility

**Files:**
- Modify: the existing log detail/parser component(s) only where `other` is parsed
- Test: existing log-format/parser test location and new focused tests as needed

**Interfaces:**
- Missing `other`, `null`, malformed JSON, or missing `client_request_headers` renders no request-header section and does not throw.

- [ ] **Step 1: Add failing compatibility tests** for empty, null, malformed, and legacy `other` JSON.
- [ ] **Step 2: Run them and confirm the expected failures.**
- [ ] **Step 3: Normalize parsed values to empty maps and render conditionally.**
- [ ] **Step 4: Run the affected tests and frontend validation.**

### Task 6: Final verification and handoff

- [ ] Run `gofmt` on changed Go files.
- [ ] Run focused Go tests, then the relevant package tests.
- [ ] Run affected frontend tests, `bun run typecheck`, and affected-file lint.
- [ ] Inspect the final diff for accidental sensitive-value masking, field replacement, or changes to protected project identifiers.
- [ ] Stage only related changed files and report the exact staged paths.
