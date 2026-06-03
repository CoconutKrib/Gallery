# Scan Progress Feedback Spec

## Summary

The app currently provides scan progress feedback in Settings via polling `GET /api/scan/status` every 1.5 seconds. Users can see running state and partial counters (`found`, `ingested`), then final counters when complete.

This spec adds clearer progress signals and an optional server-sent events (SSE) stream for real-time updates in both Settings and Staging.

## Current State

1. Scan progress exists today in Settings.
2. Transport is polling, not streaming.
3. Staging currently shows library-copy progress, but not scan progress.
4. Backend already tracks cumulative live scan stats through `ScanManager.liveStats` and `Scanner.OnProgress`.

## Goals

1. Improve in-progress scan visibility for users running long scans.
2. Show progress in both Settings and Staging.
3. Preserve backward compatibility with existing polling flow.
4. Keep implementation low risk and incremental.

## Non-Goals

1. No websocket adoption.
2. No major scanner architecture rewrite.
3. No change to ingest correctness semantics.

## Proposed Design

### Phase 1: Improve Existing Polling UX

1. Expand running-status message in Settings to include:
   - found
   - skipped
   - ingested
   - duplicate
   - errors
   - auto_staged (dropzone scans)
2. Add a compact scan-status banner to Staging page that also polls `GET /api/scan/status`.
3. Keep polling interval at 1.5 seconds for parity with current behavior.

### Phase 2: Add SSE Stream (With Polling Fallback)

1. Add `GET /api/scan/stream` endpoint (auth-protected).
2. Emit events:
   - `scan.started`
   - `scan.progress`
   - `scan.finished`
   - `scan.error`
3. Event payload should mirror `scanStatusResponse` shape, with optional event metadata:
   - `event_time` (RFC3339 UTC)
   - `message` (optional human-readable summary)
4. Frontend behavior:
   - Prefer `EventSource` when available.
   - Fall back automatically to existing polling logic if SSE fails or disconnects.

### Phase 3: Optional Counter Clarity Improvements

1. Add explicit counters to reduce ambiguity in user-facing text:
   - `walked_files` (all visited files)
   - `supported_files` (jpg/jpeg candidates)
   - `matched_files` (passed strict filters and camera gate)
2. Keep existing counters for compatibility.
3. Only expose new counters when they are populated.

## API Changes

### New Endpoint

- `GET /api/scan/stream`
  - Content type: `text/event-stream`
  - Authentication: same middleware as other API routes
  - Delivers initial snapshot event and subsequent updates while connected

### Existing Endpoint

- `GET /api/scan/status`
  - Remains unchanged and supported as primary fallback path

## Frontend Changes

1. `web/js/settings.js`
   - Improve progress line content for active scans.
   - Subscribe to SSE when possible, else poll.
2. `web/js/staging.js`
   - Add read-only scan progress banner.
   - Subscribe to SSE when possible, else poll.
3. Optional utility helper in `web/js/utils.js` for shared scan subscription/fallback logic.

## Backend Changes

1. `internal/api/scan.go`
   - Add stream subscriber infrastructure to `ScanManager`.
   - Broadcast updates on scan state transitions and progress callbacks.
2. `internal/api/router.go`
   - Register `GET /api/scan/stream`.

## Reliability and Performance

1. SSE must tolerate reconnects.
2. Bound subscriber buffers to avoid memory growth.
3. Drop stale subscriber messages instead of blocking scanner progress.
4. Preserve mutex safety around shared scan state.

## Security

1. Stream route stays behind existing auth middleware.
2. No file path leakage beyond existing status payload behavior.

## Testing Plan

1. API tests:
   - stream endpoint returns proper headers and event framing.
   - stream emits start/progress/finish lifecycle in order.
   - unauthorized users receive 401 when auth is enabled.
2. UI tests/manual checks:
   - Settings shows richer counters while running.
   - Staging reflects active scan state.
   - SSE disconnect triggers polling fallback.
3. Regression:
   - Existing scan trigger/status flow remains functional without SSE support.

## Rollout Plan

1. Ship Phase 1 first (UX-only, minimal risk).
2. Ship Phase 2 behind graceful fallback logic.
3. Defer Phase 3 unless user testing shows counter ambiguity.

## Acceptance Criteria

1. Users can observe in-progress scan counters beyond just found/ingested.
2. Staging page shows scan progress without navigating to Settings.
3. Real-time updates use SSE when available and polling when not.
4. No regressions in scan triggering or final scan summaries.
