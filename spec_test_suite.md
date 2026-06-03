# Improved Test Suite Specification

## Document status

- Status: Proposed
- Date: 2026-06-03
- Owner: Gallery project
- Scope: Backend and API test coverage improvements for scan, staging, internal library, people, and recognition flows

## Context

The current suite is weighted toward a small number of unit tests:

- internal/config/config_test.go
- internal/scan/scanner_filter_test.go
- internal/library/copy_test.go

A new integration test now exists and validates sample-image flow through scan, stage, and library copy, with optional recognition assertions:

- internal/integration/pipeline_test.go

The codebase now contains significantly more behavior than the existing unit tests cover, especially in API handlers, database query logic, recognition helper logic, and scan edge conditions.

## Goals

1. Increase confidence for core workflows that can lose data fidelity or produce incorrect state transitions.
2. Catch regressions early in API behavior, SQL filtering logic, and scanner decision rules.
3. Keep tests fast and deterministic by separating pure unit tests from integration tests.
4. Support optional recognition environments without making the default suite brittle.

## Non-goals

1. Full browser UI end-to-end testing in this phase.
2. Performance benchmarking as a primary deliverable.
3. Snapshot testing of frontend rendering output.

## Target test architecture

### Layer 1: Unit tests

Fast, deterministic, no network, minimal filesystem use.

- Target runtime: under 30 seconds for the unit-only subset on a typical dev machine.
- Focus: pure functions, request validation, branch logic, SQL behavior with isolated DB fixtures.

### Layer 2: API integration tests

Use httptest against handlers and an isolated temporary SQLite database.

- Focus: route behavior, status codes, payload validation, gate conditions, auth behavior.
- No browser required.

### Layer 3: Workflow integration tests

Real filesystem flow with sample images and temp directories.

- Existing baseline: internal/integration/pipeline_test.go.
- Focus: scan to stage to copy path and persistence correctness.

## Test environments

### Default environment

- Runs all unit tests and non-optional integrations.
- Recognition runtime is optional; tests that depend on runtime availability must degrade gracefully.

### Recognition-enabled environment

- Runs additional assertions requiring onnxruntime and model files.
- Can be enabled in CI via a dedicated job.

## Prioritized implementation plan

## Phase 1: API and auth correctness (highest priority)

### Files under test

- internal/api/router.go
- internal/api/settings.go
- internal/api/staging.go
- internal/api/library.go
- internal/api/people.go

### Required tests

1. Auth middleware behavior
- API route unauthenticated returns 401 JSON when auth is enabled.
- Page route unauthenticated redirects to /login when auth is enabled.
- Auth disabled allows pass-through.

2. Settings update semantics
- Partial POST /api/settings updates only intended fields.
- Invalid log_level returns 400.
- Auth toggle and password update pathways return expected responses.

3. Feature gate behavior
- Library and people endpoints return 409 when internal library is disabled.
- Recognition-only endpoints return expected unavailable status when runtime is not available.

### Acceptance criteria

- Handler tests exist for all items above.
- All tests run with go test ./... and pass in default environment.

## Phase 2: Database behavior and state transitions

### Files under test

- internal/db/staging.go
- internal/db/library.go
- internal/db/people.go
- internal/db/queries.go

### Required tests

1. Staging updates
- UpdateStagingEntry changes only provided fields.
- Tags and true_date_unknown persistence are stable.

2. Library copy updates
- Double-pointer semantics verified for nullable fields:
  - skip field
  - set non-null
  - clear to null

3. Delete cascades
- DeleteLibraryPhotoByID removes expected dependent rows in the right scope.

4. Filtered listing behavior
- ListLibraryCopiesFiltered combinations for source, has_date_override, true_date_unknown, tag, event_id, keyword.
- ListPhotosFiltered combinations for make/model/gps/flag/keyword with pagination and ordering.

### Acceptance criteria

- SQL branch and filter behavior covered by deterministic tests with known fixtures.
- No ambiguous or brittle ordering assumptions.

## Phase 3: Scanner and ingestion edge cases

### Files under test

- internal/scan/scanner.go
- internal/scan/exif.go
- internal/scan/thumbnail.go

### Required tests

1. Duplicate handling
- Canonical path rescan increments skipped, not duplicate.
- Different path with same hash records duplicate path once.

2. Internal library path exclusion
- Scanner skips configured internal library subtree.

3. Dropzone lenient behavior
- Missing EXIF date falls back to mtime and marks true_date_unknown.
- Auto-staging occurs for new dropzone ingests.

4. Thumbnail behavior
- GenerateThumbnail idempotency.
- Path layout correctness under cache directory.

### Acceptance criteria

- Core scanner decision branches are covered and stable.
- Tests remain deterministic using temp dirs and controlled fixtures.

## Phase 4: Recognition pure-logic tests

### Files under test

- internal/recognition/suggest.go
- internal/recognition/cluster.go
- internal/recognition/preprocess.go

### Required tests

1. Suggestion logic
- Threshold boundaries for accept and reject.
- Nearest-neighbor tie and no-match behavior.

2. Clustering logic
- Stable cluster grouping on synthetic embeddings.
- Min-samples behavior and singleton handling.

3. Embedding helpers
- Normalization invariants.
- Embedding byte encode and decode roundtrip.

### Acceptance criteria

- Pure-logic functions have deterministic vector fixtures.
- No runtime onnx dependency required for these tests.

## Existing and planned integration workflows

### Existing

- internal/integration/pipeline_test.go validates:
  - sample image scan
  - staging and approval
  - internal library copy
  - file and DB record validation
  - optional face detection assertions when recognition runtime is available

### Planned additions

1. API-level integration test via httptest
- Endpoints: /api/scan, /api/staging, /api/library/copy

2. Dropzone integration test
- Lenient scan plus auto-stage expectations

3. Idempotency regression integration test
- Second scan yields zero new ingests and stable copy and face counts

## CI strategy

## Job A: Default test suite

- Command: go test ./...
- Includes unit tests and non-optional integrations.
- Must pass on every PR.

## Job B: Recognition-enabled suite (optional but recommended)

- Enables onnxruntime and model assets.
- Runs integration tests with stricter face-detection assertions.
- Can be required after environment stability is confirmed.

## Test data and fixtures

1. Reuse samples directory JPEG assets for integration flows.
2. Use temporary directories for internal library and cache in tests.
3. Use isolated temp SQLite DB per test or per test package.
4. Add small synthetic fixture generators for vector and SQL filter tests where real image files are unnecessary.

## Risks and mitigations

1. Flaky recognition tests due to runtime environment
- Mitigation: optional mode by default, strict mode in dedicated recognition-enabled job.

2. Slow integration tests
- Mitigation: keep unit tests broad and fast; bound integration count and fixture size.

3. Over-coupled API tests
- Mitigation: test observable contracts only (status, payload shape, side effects), avoid asserting internal implementation details.

## Definition of done

1. Phase 1 and Phase 2 are complete and merged.
2. At least one new test file exists for each target package group in Phases 1 and 2.
3. Default suite remains green with go test ./....
4. Integration tests validate sample image workflow end to end.
5. TODO entries for remaining integration work are tracked and linked to this spec.

## Out of scope for this spec

1. Frontend visual regression testing.
2. Full Playwright browser journey tests.
3. Load testing or benchmark thresholds.
