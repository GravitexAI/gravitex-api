# Lyria 3 Pro Adapter Design

## Goal

Add an isolated asynchronous adapter for `lyria-3-pro-preview` through the Google Interactions API, while preserving all existing video and Omni task behavior.

## Design

The native Interactions middleware will branch only for `lyria-3-pro-preview`. It will preserve the original interaction payload in a Lyria-specific task request, including text/image input and `response_format`. The Lyria adapter will submit to `POST /v1beta/interactions`, poll `GET /v1beta/interactions/{id}`, parse `steps` content blocks, and store generated audio as an internal result URL/data URI plus lyrics metadata.

The existing `tasks` table and polling lifecycle remain the persistence boundary. Lyria uses `action=song`, the existing task status fields, and the existing `TaskBillingContext` with `PerCallBilling=true`; it does not use video duration/resolution/token settlement. The existing native Omni/video branch remains unchanged.

## Safety boundaries

- Only the exact model name `lyria-3-pro-preview` selects the new branch.
- Existing `gemini-omni-flash-preview` and video models retain their current conversion, routes, adaptors, and billing.
- Lyria accepts text and image inputs only; audio/video inputs are rejected.
- Lyria generation is asynchronous and always uses task persistence; streaming music is not added.

## Verification

Add unit tests for request branching/conversion, Lyria submit/poll response parsing, audio result preservation, and fixed-price billing isolation. Run focused Go tests and the full affected package tests.
