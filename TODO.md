TODO

* Allow manually adding a lat long to a photo, if the GPS tags are missing - store this in the database as a manual addition sidecar style piece of data. Also allow an approximation flag on this, where we're uncertain (with potentially a radius). 

* Notes/descriptions fields? 

* Export of data/metadata?

* filename filter - include then exclude (e.g exclude beats include), and check for case insensitivity (jpg, JPG etc, DSCN dscn...)

* Testing follow-ups (tracked from `spec_test_suite.md`):

* Add API-level integration test with `httptest` covering `/api/scan`, `/api/staging`, and `/api/library/copy` end-to-end.

* Add dropzone-mode integration test (lenient scan + auto-staging assertions).

* Add idempotency regression test: second scan should produce zero new ingests/copies and stable face counts.

* Search facets performance optimization: reduce repeated full-filter scans for `/api/search/facets` (currently query-contextual with per-facet self-filter exclusion) by moving to a single-pass aggregation strategy and/or tuned SQL + indexes for large libraries.