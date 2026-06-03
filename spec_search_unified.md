# Unified Search Spec (Library-First, Face-Aware)

## Status

Draft (proposed)

Implementation progress:

- Phase 1 backend: completed (June 3, 2026)
- Phase 2 frontend controls/cards: completed (June 3, 2026)
- Phase 3 polish/facets/match reasons: completed (June 3, 2026)

## Goal

Upgrade the existing Search feature into a single, unified query surface that supports:

- Discovery of curated internal-library photos by default.
- Filtering by people tags and confirmed face-recognition assignments.
- Optional inclusion of unverified face suggestions (toggleable, default off).
- Existing metadata filters (date range, camera make/model, GPS, flags, keyword).
- Existing pagination and sorting.

The implementation should preserve current API compatibility where practical while extending behavior and payloads in a backward-safe way.

## Product Decisions (confirmed)

1. Default result scope: curated internal-library photos.
2. Face filtering default: confirmed faces only.
3. Endpoint strategy: one search endpoint with expanded capabilities.

## Non-Goals

- Replacing the dedicated Face Review workflow.
- Introducing free-text semantic/vector search.
- Reworking the entire UI design system.

## Current Gaps

- Search currently filters only from core photo metadata (q/from/to/make/model/has_gps/flag).
- Search cannot filter by people, face confirmation state, library tags, override-date state, or event assignment.
- Search result cards do not expose why a photo matched (person/tag/event/source context).

## Proposed API Contract

Primary endpoint remains:

- GET /api/photos

### Existing query params (keep)

- q
- from
- to
- make
- model
- has_gps
- flag
- page
- per_page
- order
- library_id

### New query params

Scope and curation:

- in_library
  - Values: true | false | any
  - Default: true (for Search page requests)
  - Notes:
    - true = only photos with a row in library_copies.
    - false = only photos not yet in library_copies.
    - any = no library membership filter.

Library metadata:

- tag
  - String; case-insensitive match in library_copies.tags JSON array.
- event_id
  - Integer; exact match on library_copies.event_id.
- has_date_override
  - Values: true | false | any
  - Operates on library_copies.override_date.
- true_date_unknown
  - Values: true | false | any
  - Operates on library_copies.true_date_unknown.
- source
  - Values: scan | dropzone | any
  - Uses photos.source.

People and face-recognition:

- person_id
  - Integer; photos containing at least one matching face assignment.
- has_people
  - Values: true | false | any
  - true = at least one face row with person_id not null.
- face_verified
  - Values: true | false | any
  - Default when person/face filters are used: true.
  - true = only verified assignments.
  - false = only unverified assignments.
- face_source
  - Values: manual | auto | any
  - Filter by faces.source.
- include_unverified_faces
  - Values: true | false
  - Default: false.
  - When false, unverified assignments are excluded from person matching and face chips.
  - When true, unverified matches are allowed and should be labeled in response metadata.

Reasoning and diagnostics:

- include_match_meta
  - Values: true | false
  - Default: false.
  - When true, response includes compact match_reasons array per item.

### Validation

- Unknown enum values return 400 with clear error message.
- Numeric parsing errors return 400 for person_id, event_id, page, per_page.
- Existing permissive date parsing behavior remains.

## Response Schema

Keep current top-level shape:

- total
- page
- per_page
- items

Enhance each item with optional and default-safe fields:

Required additions:

- source: "scan" | "dropzone"
- in_library: boolean
- library_copy_id: number | null
- tags: string[] (empty array when not in library)
- event_id: number | null
- title: string | null
- description: string | null
- override_date: string | null
- true_date_unknown: boolean | null
- people: array of
  - person_id
  - person_name
  - verified
  - source
  - face_count

Optional additions (only when include_match_meta=true):

- match_reasons: string[]
  - Examples: "person:42", "tag:favorite", "event:7", "camera:Canon", "keyword:party".

Compatibility note:

- Existing fields remain unchanged.
- New fields should always be present with null/empty defaults to simplify frontend logic.

## Backend Design

### Data-layer extension

Extend db.PhotoFilter in internal/db/queries.go with:

- InLibrary *bool plus Any-state handling via dedicated enum/string.
- Source string.
- Tag string.
- EventID *int64.
- HasDateOverride *bool.
- TrueDateUnknown *bool.
- PersonID *int64.
- HasPeople *bool.
- FaceVerified *bool.
- FaceSource string.
- IncludeUnverifiedFaces bool.

Query strategy:

- Start from photos p.
- LEFT JOIN library_copies lc on lc.photo_sha256 = p.sha256 when any library field/filter is used or when in_library filter is active.
- LEFT JOIN faces f and people pe when any person/face field/filter is used.
- Use EXISTS subqueries for person/face presence filters to avoid duplicate photo rows.
- Keep counting and list query consistent by sharing WHERE logic.
- Preserve existing ordering defaults and limits.

Performance and correctness:

- Use DISTINCT p.id only when unavoidable.
- Prefer EXISTS for has_people/person filters.
- Add indexes if query plans regress:
  - faces(photo_id, person_id, verified, source)
  - library_copies(photo_sha256, event_id, true_date_unknown)

### API-layer extension

Update internal/api/photos.go parser:

- Parse new params into db.PhotoFilter.
- For Search page behavior, default in_library=true unless explicitly overridden.
- Apply default include_unverified_faces=false.
- Apply effective face_verified=true when person/face filters are used and face_verified is absent.

Update photoSummary mapping:

- Add newly described fields.
- Assemble people chips from grouped query results (limit e.g. top 5 names with counts).

### Route strategy

No new endpoint required for baseline. Keep GET /api/photos.

Optional future:

- GET /api/search/facets for distinct values and counts (people, tags, cameras, flags, sources).

## Frontend Design (Search page)

File: web/js/search.js

### Default behavior

- On initial load without explicit in_library, set it to true.
- Keep existing URL-driven state model.

### New filter controls

Add compact controls for:

- Scope: in_library (Library only default; Not in library; Any).
- Source: scan/dropzone/any.
- Tag.
- Event ID.
- Date override: yes/no/any.
- True date unknown: yes/no/any.
- Person selector:
  - typeahead list loaded from /api/people.
  - stores person_id in query string.
- Face options:
  - verified only (default checked).
  - include unverified suggestions (default off).
  - source manual/auto/any.
- Sort dropdown wired to existing order param.

### Result cards

Enhance cards with small metadata strips:

- tags (first 2 with +N)
- people chips (verified/unverified visual distinction)
- source badge
- event badge
- existing flag indicator remains

### UX details

- Keep query state in URL for shareability.
- Show active filter chips with one-click removal.
- Add "Clear all" button.
- Maintain pagination behavior.

## Security and Auth

- Existing auth middleware remains unchanged.
- No filesystem exposure changes.
- Do not return sensitive absolute paths beyond current behavior.

## Migration and Compatibility

- No schema migration required for baseline feature set.
- If index additions are needed, add a new migration file (005_search_indexes.sql).
- Existing clients continue to work without sending new params.

## Test Plan

Reference and extend spec_test_suite.md strategy.

### Unit tests (DB)

Add/extend tests in internal/db/phase2_test.go (or a new focused file):

- in_library true/false/any behavior.
- tag/event/date override/true_date_unknown filters.
- person_id with verified only default.
- include_unverified_faces toggle behavior.
- face_source filter.
- combined filters with pagination and order.
- total count correctness under joins/existence predicates.

### API tests

Add tests in internal/api/phase1_test.go or new search-focused API test file:

- parser validation (invalid enums/ids -> 400).
- defaulting behavior (in_library=true for search usage).
- face filter defaulting (verified true when applicable).
- response schema includes new fields with stable defaults.

### Frontend tests

If current stack has no JS unit harness, add lightweight integration checks or manual checklist:

- URL query round-trip for all new controls.
- person selector writes person_id.
- verified/unverified toggles produce expected request params.
- card chips render and degrade safely with empty metadata.

## Rollout Plan

Phase 1 (backend):

- Extend filter model and query builder.
- Add parser support and response enrichment.
- Add DB/API tests.

Phase 1 implementation result:

- Done: db.PhotoFilter and db.ListPhotosFiltered now support in_library/source/tag/event/date-override/true-date-unknown/person/people/face-verified/face-source constraints.
- Done: /api/photos parser supports new params and validates invalid enum/int inputs with 400.
- Done: response items now include unified metadata fields: source, in_library, library_copy_id, tags, event_id, title, description, override_date, true_date_unknown, people.
- Done: default face behavior for person/face filters is verified-only unless include_unverified_faces=true.
- Done: tests added in internal/db/phase2_test.go and internal/api/photos_test.go.

Phase 1 discrepancies vs initial draft:

- in_library defaulting is not forced globally in /api/photos; it is intended to be set by Search UI state in Phase 2 to avoid changing behavior for other consumers.
- include_match_meta and match_reasons are not implemented in Phase 1 (still planned for Phase 3).

Phase 2 (frontend controls):

- Add new filters and URL-state wiring.
- Add sort control and active chips.

Phase 2 implementation result:

- Done: Search page now includes unified filters (scope/source/tag/event/date-override/date-unknown/person/face controls) in web/js/search.js.
- Done: URL state is wired for new filters, including pagination and sort.
- Done: Search default behavior now applies in_library=true, face_verified=true, include_unverified_faces=false from UI state.
- Done: active filter chips with one-click removal and a clear-all action are implemented.
- Done: result cards now render metadata badges/chips for source/event/tags/people.

Phase 2 discrepancies vs initial draft:

- Person selection currently resolves by exact name match from /api/people typeahead; no free-text person search fallback is implemented.
- Frontend automated tests were not added yet (manual verification path remains recommended until a JS test harness is introduced).

Phase 3 (polish):

- Add match reasons and optional facets endpoint.
- Tune indexes based on query plans.

Phase 3 implementation result:

- Done: include_match_meta query parameter added to GET /api/photos.
- Done: match_reasons array emitted per item when include_match_meta=true.
- Done: new GET /api/search/facets endpoint added and routed.
- Done: facets payload includes scope, sources, camera_make, camera_model, tags, people, flags.
- Done: facets are query-contextual and exclude their own active filter dimension (e.g. source facet excludes source filter while honoring the rest).
- Done: API tests added for include_match_meta and facets endpoint shape.

Phase 3 discrepancies vs initial draft:

- match_reasons currently reports active-filter reason tokens (and lightweight context hints), not strict per-field proof metadata.
- /api/search/facets now uses query-contextual filtering with per-facet self-filter exclusion; further optimization may be desirable for very large libraries.
- index tuning migration (005_search_indexes.sql) was not required by current test-scale performance and remains optional if production profiling indicates need.

## Acceptance Criteria

- Search defaults to curated-library photos.
- User can filter by person and receive only confirmed matches by default.
- User can explicitly include unverified matches via toggle.
- User can filter by library tags/event/date-override/date-unknown/source.
- Existing metadata filters continue working.
- Pagination totals remain correct under combined filters.
- No regressions in existing /api/photos consumers.

## Open Implementation Notes

- Confirm whether default in_library=true should apply globally at API layer, or only when called by Search page. Recommended: only Search page sends it explicitly to avoid surprising other consumers.
- For person chips in result cards, cap payload size to avoid oversized responses on dense group photos.
- For tag matching on JSON text, keep current LIKE strategy for now; consider SQLite JSON1 usage later if needed.
