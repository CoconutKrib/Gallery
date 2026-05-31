# Internal Photo Library — Feature Specification

## Status: Implemented (Phases 6–9 complete)

### Implementation notes

Phase 6 is fully implemented. The following deviations or clarifications apply:

- **Nav hiding**: the Staging and Library nav links are always present in the HTML. Pages show a "not enabled" notice if `internal_library.enabled = false`. The `body` element gains the class `library-enabled` at startup (via `app.js`) when the feature is enabled — this class controls Stage button visibility on photo cards via CSS.
- **`config.Validate()` call**: path-overlap validation is called at process startup in `main.go`, immediately after loading the config file.
- **Scanner exclusion**: `scanner.go` receives a new `isInternalLibraryPath()` method that returns `fs.SkipDir` for any directory that equals or is a subdirectory of `internal_library.path`, preventing the managed copy tree from being inadvertently re-scanned.
- **Stage button on photo cards**: added to both Browse and Search grids. The button is hidden by CSS until `library-enabled` is set on `<body>`. It calls `Gallery.utils.stagePhoto()` which POSTs to `/api/staging`.
- **Bulk copy job state**: implemented as a package-level `copyJobStatus` struct in `internal/api/library.go` (not a persistent DB record). The status is in-memory only and is reset on server restart.
- **`source` column on `photos`**: added by migration `002_internal_library.sql` with `DEFAULT 'scan'`. The dropzone `'dropzone'` value is set in Phase 7.

Phase 7 (Dropzone) is fully implemented. No spec deviations.

Phase 8 (Library Copy Editing & Re-organisation) is fully implemented. Implementation notes:

- **Double-pointer pattern for PATCH fields**: `LibraryCopyUpdate` uses `**string` / `**int64` fields — outer `nil` means "skip this field"; outer non-nil pointing to a nil `*string` clears the column. This cleanly distinguishes "not sent" from "explicitly set to null".
- **Re-org on date change**: `handleLibraryCopyPatch` checks whether `override_date` or `true_date_unknown` changed (using `ptrStringChanged`); if so, calls `library.MovePhoto` which recalculates the target directory, moves the file with `os.Rename`, prunes empty ancestor directories up to 3 levels, and updates both `relative_path` and `absolute_path` in the DB.
- **Empty-directory pruning**: `pruneEmptyDirs(dir, root, maxLevels int)` in `copy.go` walks up the directory tree removing empty directories, stopping at the library root or after `maxLevels` iterations. Parent directory extraction uses `strings.LastIndex` (not a helper from another package).
- **`BuildRelDir` / `ResolveFilename` exported**: the previously unexported helpers are now also exported for use by the re-org handler; the originals remain as the canonical implementation.
- **Filter query column aliasing**: `ListLibraryCopiesFiltered` prefixes all selected columns with `lc.` to avoid ambiguity when joining the `photos` table.

Phase 9 (Library Photo Removal) is fully implemented. Implementation notes:

- **Cascade in transaction, file delete after commit**: `DeleteLibraryPhotoByID` wraps steps 1–5 of the cascade in a single transaction and returns the resolved `absPath`. The physical file deletion happens in the HTTP handler after the transaction commits — orphaned files are harmless (scanner skips the library path).
- **204 No Content handling in `utils.js`**: `Gallery.utils.api()` now short-circuits on `res.status === 204` and returns `null`, preventing a JSON parse error on empty bodies.
- **Confirmation via native `confirm()`**: the spec calls for a modal; the implementation uses the browser's native `confirm()` dialog to keep the JS footprint small.

---

## 1. Goals

Gallery currently operates as a **read-only discovery engine** over pre-existing file trees. This feature adds a complementary **write side**: an organized, Gallery-managed copy of the photos a user chooses to keep, annotated, and laid out in a human-readable filesystem structure.

Two capabilities are specified:

| Capability | Summary |
|---|---|
| **Internal Photo Library** | Gallery maintains an organized, filesystem-visible copy of curated photos. Year/Month/Event hierarchy. All metadata in SQLite. Original sources untouched. |
| **Dropzone** | A single configured input folder for photos that must be imported but may lack usable EXIF (scanned prints, B&W archival photos, third-party photographer albums). Treated as a lenient scan source; otherwise feeds the same internal library workflow. |

The user-facing term for the Gallery-managed copy is **"internal photo library"** throughout the codebase and UI.

---

## 2. Phasing

- **Phase 6** — Internal library infrastructure, staging queue, copy mechanism, review UI
  - Also includes completion of previously-deferred settings UI work (inline whitelist/filter editing, ingest issues panel) as a preliminary step. This work is small and mostly unrelated to the internal library feature.
- **Phase 7** — Dropzone source (lenient scanner, auto-stage on ingest)

Dropzone is treated as a source detail; the internal library is the primary investment.

---

## 3. Internal Photo Library

### 3.1 Concept

The internal photo library is a single folder on the filesystem, managed exclusively by Gallery. It is **write-only**: Gallery copies photos in, never re-scans it as a source. The structure is:

```
{internal_library_path}/
  2024/
    06/
      Wedding-Smith/
        IMG_0001.jpg
        IMG_0002.jpg
      /                       ← photos in June without an event
        DSC00123.jpg
  _undated/
    Grandma-1940s/
      scan_0001.jpg
    /                         ← undated photos without an event
      scan_0002.jpg
```

Key properties:
- **Non-destructive**: originals (scan tree, dropzone) are never moved or deleted.
- **Deduplicated by SHA-256**: a photo can only appear once in the internal library.
- **Human-inspectable**: folder structure is the primary navigation aid for non-Gallery tooling.
- **Metadata in SQLite**: titles, descriptions, flags, GPS overrides, event links — all in the DB.
- **Portable**: the folder + DB together form a complete, portable archive.

### 3.2 Filesystem Hierarchy

Path construction follows this priority order:

```
{year}/{month_zero_padded}/{event_slug}/
{year}/{month_zero_padded}/               ← no event
{year}/                                   ← year only, no month
_undated/{event_slug}/                    ← date unknown, has event
_undated/                                 ← date unknown, no event
```

- `year` and `month` are derived from `captured_at` (or the user-supplied override date).
- `event_slug` is the event name lowercased with non-alphanumeric characters replaced by hyphens (e.g. `Wedding-Smith`, `Holiday-Cornwall-2018`).
- If an event name would produce an empty or degenerate slug, fall back to the parent path without the event segment.

### 3.3 Filename Collision Resolution

If the target filename already exists in the destination folder (different content, same name), append the first 8 characters of the photo's SHA-256 hash:

```
IMG_0001.jpg          → first arrival
IMG_0001_a3f2c891.jpg → collision
```

Photos already in the internal library (same SHA-256) are **silently skipped** — copy is idempotent.

### 3.4 Configuration

New keys in the existing `Config` struct:

```json
{
  "internal_library": {
    "path": "/home/alice/Gallery",
    "enabled": true
  },
  "dropzone": {
    "path": "/home/alice/Photos/To-Import",
    "enabled": false
  }
}
```

- `internal_library.path` must not overlap with any `library_paths` or the `dropzone.path` — validated at startup.
- When `enabled: false`, the path is ignored and all copy/staging APIs return `409 Conflict` with a clear message.

---

## 4. Staging Queue

### 4.1 Purpose

Photos do not go directly from discovery to the internal library. They pass through a **staging queue**: a holding area where the user can review, annotate, and approve (or skip) each photo before the copy is executed.

Any Gallery-discovered photo (from any library path or the dropzone) can be added to the staging queue manually. Dropzone photos are **automatically staged** on ingest.

### 4.2 States

```
discovered ──► staged ──► approved ──► copied_to_library
                 │
                 └──► rejected (removed from queue, not from source)
```

| State | Meaning |
|---|---|
| `staged` | In the queue, awaiting review |
| `approved` | User has reviewed and authorized the copy |
| `rejected` | Dismissed from queue; not copied; source untouched |
| (implicit) | `copied` — a `library_copies` DB row exists for this SHA-256 |

### 4.3 Annotations

The user can set the following on any staged photo before or during the copy step:

| Field | Notes |
|---|---|
| `title` | Free text, optional |
| `description` | Free text, multi-line, optional |
| `override_date` | RFC3339 UTC; replaces `captured_at` for path calculation and DB storage |
| `override_lat`, `override_lon` | Manual GPS; marked as `approximate: true` in the DB |
| `event_id` | Assign or reassign to an existing Gallery event |
| `tags` | JSON string array; merged with any existing EXIF-derived flags; carried over verbatim to `library_copies.tags` at copy time |
| `true_date_unknown` | Boolean; if set, photo lands in `_undated/` regardless of any date and is persistently tagged as archival/historic |

`true_date_unknown` is stored on the `library_copies` row and surfaced as a flag in search results. It is a permanent marker: the exact original capture date cannot be known.

---

## 5. Database Schema Changes

### 5.1 New migration (e.g. `002_internal_library.sql`)

```sql
-- Source column on photos; 'scan' for normal library paths, 'dropzone' for dropzone ingest
ALTER TABLE photos ADD COLUMN source TEXT NOT NULL DEFAULT 'scan';

-- Staging queue
CREATE TABLE staging_queue (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    photo_sha256    TEXT    NOT NULL UNIQUE REFERENCES photos(sha256),
    title           TEXT,
    description     TEXT,
    override_date   TEXT,               -- RFC3339 UTC, nullable
    override_lat    REAL,
    override_lon    REAL,
    event_id        INTEGER REFERENCES events(id),
    tags            TEXT    NOT NULL DEFAULT '[]',  -- JSON array
    true_date_unknown INTEGER NOT NULL DEFAULT 0,
    state           TEXT    NOT NULL DEFAULT 'staged',  -- staged | approved | rejected
    created_at      TEXT    NOT NULL,
    updated_at      TEXT    NOT NULL
);

-- Internal library copies
CREATE TABLE library_copies (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    photo_sha256     TEXT    NOT NULL UNIQUE REFERENCES photos(sha256),
    relative_path    TEXT    NOT NULL,  -- e.g. 2024/06/Wedding-Smith/IMG_0001.jpg
    absolute_path    TEXT    NOT NULL,
    true_date_unknown INTEGER NOT NULL DEFAULT 0,
    tags             TEXT    NOT NULL DEFAULT '[]',  -- JSON array; carried over from staging_queue at copy time
    copied_at        TEXT    NOT NULL
);

CREATE INDEX idx_staging_state ON staging_queue(state);
CREATE INDEX idx_library_copies_sha ON library_copies(photo_sha256);
```

---

## 6. API Changes

All new endpoints are under the existing auth middleware.

### 6.1 Staging

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/staging` | List staging queue; supports `?state=staged\|approved\|rejected` |
| `POST` | `/api/staging` | Add a photo to the queue; body: `{"sha256": "..."}` |
| `GET` | `/api/staging/{id}` | Get a single staging entry with full annotation detail |
| `PATCH` | `/api/staging/{id}` | Update annotations on a staged entry |
| `POST` | `/api/staging/{id}/approve` | Transition to `approved` |
| `POST` | `/api/staging/{id}/reject` | Transition to `rejected` (removes from queue) |
| `DELETE` | `/api/staging/{id}` | Remove from queue (equivalent to reject) |

### 6.2 Internal Library — Copy

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/library/copy` | Trigger copy for all `approved` entries (async, returns job ID) |
| `POST` | `/api/library/copy/{staging_id}` | Copy a single approved entry immediately |
| `GET` | `/api/library/status` | Status of the last copy job |

> **Clustering**: after a bulk copy job completes, `cluster.Run()` must be triggered so newly-added library photos are incorporated into event groups. Single-photo copies (`/api/library/copy/{staging_id}`) do not re-trigger clustering.

### 6.3 Internal Library — Browse

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/library/photos` | List all photos in the internal library (paginated, filterable) |
| `GET` | `/api/library/tree` | Return the year/month/event folder hierarchy as JSON |

### 6.4 Settings

`GET /api/settings` response gains:

```json
{
  "internal_library": { "path": "...", "enabled": true },
  "dropzone": { "path": "...", "enabled": false }
}
```

---

## 7. Frontend Changes

### 7.1 New page: Staging (`/staging`)

A dedicated SPA route showing the staging queue. Layout:

- **Left panel**: photo grid, filterable by state (staged / approved / rejected)
- **Right panel**: annotation form for the selected photo, showing:
  - Current EXIF metadata (read-only summary)
  - All editable annotation fields (title, description, override date, GPS, event picker, tags, `true_date_unknown` toggle)
  - Action buttons: Approve, Reject, Add to Queue (from search)
- **Top bar**: "Copy all approved" button → triggers `POST /api/library/copy`; shows progress/status

From the search and browse views, each photo card gains a small **"Stage"** button (visible when internal library is enabled) to add a photo to the queue.

### 7.2 New page: Internal Library (`/library`)

- Mirrors the browse view but scoped to `library_copies`
- Folder-tree navigation (year → month → event)
- Chips/badges for `true_date_unknown` photos
- Link back to original scan source photo detail

### 7.3 Navigation

Add **"Staging"** and **"Library"** items to the main nav. Both are hidden when `internal_library.enabled = false`.

### 7.4 Settings page — Dropzone section

The `/settings` page gains a **Dropzone** section (rendered via `dropzoneSectionHtml` in `settings.js`) between the Libraries section and the Camera Whitelist section. It shows:
- Enabled/disabled badge
- Configured path
- **"Scan Dropzone"** button (rendered only when `enabled: true` and path is non-empty); clicking it posts `{source: 'dropzone'}` to `POST /api/scan` and polls scan status with the label `'Dropzone'`

---

## 8. Dropzone (Phase 7)

### 8.1 Scanner Changes

The scanner acquires a `mode` parameter: `strict` (existing behaviour) or `lenient` (dropzone):

| Check | Strict | Lenient |
|---|---|---|
| EXIF camera make/model whitelist | Applied | Skipped |
| `captured_at` required | Yes | No (defaults to file mtime, flagged `true_date_unknown`) |
| Filename include/exclude filters | Applied | Skipped |
| SHA-256 dedup | Applied | Applied |

Photos from the dropzone have `source = 'dropzone'` in the `photos` table.

### 8.2 Auto-Staging

After a successful dropzone ingest, any newly inserted dropzone photo is automatically inserted into `staging_queue` with `state = 'staged'`. The user then reviews them in the Staging page.

If a dropzone photo has no EXIF date, `true_date_unknown = 1` is set on the staging entry automatically. File mtime is still used as the path-placement date (so the photo lands in the correct year/month folder), but the photo is permanently flagged because the exact capture date cannot be known. In other words: `true_date_unknown` is set whenever EXIF date is absent, regardless of whether the mtime fallback succeeded.

**Idempotency on rescan**: if a dropzone photo is rescanned while it is already in the staging queue, the UNIQUE constraint on `staging_queue.photo_sha256` prevents a duplicate entry. The duplicate insertion error is silently swallowed and `stats.AutoStaged` is not incremented again.

### 8.3 Scan Trigger

The existing `/api/scan` endpoint accepts an optional `{"source": "dropzone"}` body to trigger a dropzone-only scan. Without this field, a normal scan runs library paths only.

> **Implementation note**: there is no combined "scan all" mode that runs both library paths and the dropzone in one request. The two scan types are always triggered independently. A dropzone scan must always be requested explicitly with `{"source": "dropzone"}`.

The scan manager uses the label `"Dropzone"` for in-progress status reporting while a dropzone scan is running. Re-clustering is triggered after a dropzone scan only when `stats.Ingested > 0` (same as for library scans).

---

## 9. Constraints and Invariants

| Invariant | Enforcement |
|---|---|
| Internal library path not overlapping any library path | Config validation at startup |
| Internal library never re-scanned as input | `scanner.go` compares each walk root against `config.InternalLibrary.Path`; roots that equal or are subdirectories of it are skipped before walking begins |
| Copy is idempotent (SHA-256 dedup) | `UNIQUE` constraint on `library_copies.photo_sha256` |
| Originals never moved or deleted | Copy code only calls `os.Copy`; no rename/delete paths |
| `true_date_unknown` is permanent | Exposed as a flag in all photo representations; never silently dropped |
| Annotations in staging override EXIF for path calculation only | EXIF data in `photos` table is immutable; overrides live in `staging_queue` / `library_copies` |

---

## 10. Open Questions / Future Work

- **Re-organization and post-copy editing**: specced as Phase 8 — see §11.
- **Removal from internal library**: specced as Phase 9 — see §12.
- **Export**: bulk export/zip of the internal library is a separate future feature. The internal library is a pure filesystem structure, so could be copied out directly; the photo metadata (event membership, overridden dates, descriptions, tags — all stored in SQLite) could be exported as a separate JSON file stored alongside it.
- **Multiple event membership**: current model assigns one event per photo; many-to-many is a future schema change.
- **Dropzone watch mode**: instead of manual scan trigger, inotify/fsnotify watch on the dropzone folder. Out of scope for Phase 7.

---

## 11. Library Copy Management — Editing and Re-organisation (Phase 8)

### 11.1 Motivation

After a photo is copied to the internal library it must remain fully editable: the user may have dated archival photos carefully in staging, or may discover a wrong camera-clock date later. Edits that change the effective date must physically move the file to keep the folder hierarchy consistent with the metadata.

Additionally, the library browse view needs to be queryable by the annotations that make it useful (tags, event, description, source, date-override flag), since the internal library is the primary curated data store rather than the discovery layer.

### 11.2 New Migration (`003_library_copy_metadata.sql`)

```sql
-- Rich annotations on library copies (carried from staging_queue at copy time;
-- can be updated post-copy via PATCH /api/library/copies/{id})
ALTER TABLE library_copies ADD COLUMN title       TEXT;
ALTER TABLE library_copies ADD COLUMN description TEXT;
ALTER TABLE library_copies ADD COLUMN override_date TEXT;  -- RFC3339 UTC; when set, drives path placement
ALTER TABLE library_copies ADD COLUMN event_id    INTEGER REFERENCES events(id);

CREATE INDEX IF NOT EXISTS idx_library_copies_event_id ON library_copies(event_id);
```

The **copy service** (`internal/library/copy.go`) is updated to carry `title`, `description`, `override_date`, and `event_id` from the `staging_queue` row into the new `library_copies` columns at copy time.

The `true_date_unknown` column already exists on `library_copies` and continues to be carried from staging.

### 11.3 Post-Copy Editing API

#### New endpoint: `PATCH /api/library/copies/{id}`

Accepts a JSON body with any subset of:

| Field | Type | Notes |
|---|---|---|
| `title` | string | Free text |
| `description` | string | Free text, multi-line |
| `tags` | `[]string` | Replaces existing tags |
| `event_id` | integer\|null | Assign/clear event |
| `override_date` | string\|null | RFC3339 UTC; null clears the override |
| `true_date_unknown` | boolean | Permanent archival flag |

Returns the updated library copy record.

**Re-organisation trigger**: after applying updates, if `override_date` or `true_date_unknown` changed, the handler recalculates the target path using the same priority logic as the copy service (§3.2). If the directory component differs from the current `relative_path`, the file is moved:

1. Create the new destination directory (`os.MkdirAll`).
2. Apply collision resolution if a different file already exists at the new path (§3.3).
3. Move the file (`os.Rename`).
4. Update `library_copies.relative_path` and `library_copies.absolute_path`.
5. Remove the old parent directory if it is now empty (walk up at most 3 levels, stop at `internal_library.path`).

If the file move fails, the DB is not updated and the handler returns `500`. The operation is atomic from the user's perspective: either both the file and the DB update succeed, or neither does.

#### Staging queue (pre-copy)

Date editing before copy already works via the existing `PATCH /api/staging/{id}` endpoint (`override_date`, `true_date_unknown`). No new staging API is required. The path shown in the staging review UI should preview the calculated destination folder using current annotation values.

### 11.4 Effective Date Resolution

The same rule applies in both the copy service and the re-organisation handler:

```
if true_date_unknown → _undated/
else if override_date set → use override_date
else if photos.captured_at set → use captured_at
else → _undated/
```

`true_date_unknown = true` always wins over any date value — it represents a permanent curatorial decision that the capture date cannot be reliably known.

### 11.5 Extended Library Browse Filters

`GET /api/library/photos` gains the following additional query parameters. All are optional and composable.

| Parameter | Type | Behaviour |
|---|---|---|
| `source` | `scan` \| `dropzone` | Filters by `photos.source` (JOIN to `photos` on `sha256`) |
| `has_date_override` | `true` \| `false` | `true` → `library_copies.override_date IS NOT NULL` |
| `true_date_unknown` | `true` \| `false` | Filters on `library_copies.true_date_unknown` |
| `tag` | string | Photo must contain this value in `library_copies.tags` JSON array (case-insensitive) |
| `event_id` | integer | Exact match on `library_copies.event_id` |
| `q` | string | Keyword search across `title`, `description`, and `filename` (LIKE, case-insensitive) |

These filters are implemented in a new `LibraryCopyFilter` struct in `internal/db/library.go` (or equivalent), keeping them separate from `PhotoFilter` which operates on the `photos` table.

### 11.6 Frontend

The `/library` page gains:

- **Edit panel**: clicking a photo opens a right-hand annotation panel (similar in layout to the staging review panel) showing the current `title`, `description`, `tags`, `event_id`, `override_date`, and `true_date_unknown`. Changes are saved via `PATCH /api/library/copies/{id}`. If the server responds with a moved path, the UI reflects the new folder location.
- **Filter bar**: the library grid gains filter controls for `source`, `has_date_override`, `true_date_unknown`, `tag`, `event_id`, and keyword `q`, mirroring the controls available on the Search page.
- **`has_date_override` badge**: photos with `override_date` set display a small "date overridden" badge in the grid and detail view.

---

## 12. Library Photo Removal (Phase 9)

### 12.1 Purpose

Removing a photo from the internal library is a destructive, irreversible operation. It:

- Deletes the physical file from the internal library folder.
- Removes all Gallery database records for that SHA-256.
- Leaves the **original source file** untouched (in the scan library or dropzone folder).
- Means the photo will only reappear in Gallery if the source is rescanned, causing it to be ingested as a new photo and auto-staged (if from the dropzone) or simply visible in discovery again (if from a library path).

### 12.2 Removal API

#### New endpoint: `DELETE /api/library/copies/{id}`

No request body. On success, returns `204 No Content`.

**Cascade order** (SQLite FK constraints are checked; no `ON DELETE CASCADE` is defined, so referencing rows must be deleted explicitly in this order):

1. Resolve `sha256` from `library_copies.photo_sha256` for the given `id`.
2. Delete from `photo_events` WHERE `photo_id = (SELECT id FROM photos WHERE sha256 = ?)`.
3. Delete from `staging_queue` WHERE `photo_sha256 = ?`.
4. Delete from `duplicate_paths` WHERE `sha256 = ?`.
5. Delete from `library_copies` WHERE `photo_sha256 = ?`.
6. Delete the physical file at `library_copies.absolute_path`.
7. Remove the parent directory if empty (walk up at most 3 levels, stop at `internal_library.path`).
8. Delete from `photos` WHERE `sha256 = ?`.

Steps 2–5 and 8 are wrapped in a single DB transaction. The physical file (step 6) is deleted after the transaction commits. If the file delete fails, the DB changes are **not** rolled back — the record is gone and the orphaned file will be ignored on future scans (not a JPEG at a known library path).

### 12.3 Constraints and Safety

| Constraint | Enforcement |
|---|---|
| Requires internal library enabled | Handler returns `409 Conflict` if `internal_library.enabled = false` |
| Requires confirmation from UI | Frontend shows a confirmation modal before calling DELETE |
| Original source file is never touched | Only `library_copies.absolute_path` is deleted; source `filepath` in `photos` is not |
| Idempotent on missing file | If the physical file is already absent, the file-delete step is skipped (log a warning) |

### 12.4 Frontend

The `/library` page gains a **"Remove from Library"** action:

- Available in the photo detail/edit panel (§11.6) as a destructive-styled button.
- Clicking opens a confirmation modal: _"Remove [filename] from the library? This will delete the copy and all Gallery records for this photo. The original file is not affected."_
- On confirmation, calls `DELETE /api/library/copies/{id}`.
- On success, removes the photo from the current grid view and shows a brief toast notification.
