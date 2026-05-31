# Internal Photo Library — Feature Specification

## Status: Implemented (Phases 6 & 7 complete)

### Implementation notes

Phase 6 is fully implemented. The following deviations or clarifications apply:

- **Nav hiding**: the Staging and Library nav links are always present in the HTML. Pages show a "not enabled" notice if `internal_library.enabled = false`. The `body` element gains the class `library-enabled` at startup (via `app.js`) when the feature is enabled — this class controls Stage button visibility on photo cards via CSS.
- **`config.Validate()` call**: path-overlap validation is called at process startup in `main.go`, immediately after loading the config file.
- **Scanner exclusion**: `scanner.go` receives a new `isInternalLibraryPath()` method that returns `fs.SkipDir` for any directory that equals or is a subdirectory of `internal_library.path`, preventing the managed copy tree from being inadvertently re-scanned.
- **Stage button on photo cards**: added to both Browse and Search grids. The button is hidden by CSS until `library-enabled` is set on `<body>`. It calls `Gallery.utils.stagePhoto()` which POSTs to `/api/staging`.
- **Bulk copy job state**: implemented as a package-level `copyJobStatus` struct in `internal/api/library.go` (not a persistent DB record). The status is in-memory only and is reset on server restart.
- **`source` column on `photos`**: added by migration `002_internal_library.sql` with `DEFAULT 'scan'`. The dropzone `'dropzone'` value is reserved for Phase 7.

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

- **Re-organization**: if a user later supplies a date for an `_undated` photo, should Gallery move the file within the internal library or leave it and update the DB path? (Suggested: move the file, update `library_copies.relative_path` and `absolute_path`.) - Yes, sound logic. But in case we need to find these, ensure that our search features allow us to find all photos that have had manual date ovverrides applied. Check we can also find photos by import (e.g. dropzone vs scan), and by tags, or event or description. 
- **Removal from internal library**: out of scope for Phase 6/7. When implemented: delete the physical file, remove the `library_copies` row; source photo record in `photos` is also deleted - the photo records and file are totally removed from the gallery internal databases and library - it would need a re-scan to reappear in staging.
- **Export**: bulk export/zip of the internal library is a separate future feature. External library is a pure filesystem structure, so could be copied out directly (the photo metadata, such as event membership or overridden dates, or manually added descriptions, all of which are stored in the sqlite database could be exported as a separate json file and stored alonside this)
- **Multiple event membership**: current model assigns one event per photo; many-to-many is a future schema change.
- **Dropzone watch mode**: instead of manual scan trigger, inotify/fsnotify watch on the dropzone folder. Out of scope for Phase 7.
