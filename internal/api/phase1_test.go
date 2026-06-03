package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/halleck/gallery/internal/auth"
	"github.com/halleck/gallery/internal/config"
	"github.com/halleck/gallery/internal/recognition"
)

func TestAuthMiddleware_APIAndPageBehavior(t *testing.T) {
	cfg := &config.Config{Auth: config.AuthConfig{Enabled: true}, SessionTTLHours: 24}
	h := &Handlers{cfg: cfg, sessions: auth.NewStore(cfg.SessionTTLHours)}
	protected := h.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	t.Run("api unauthenticated returns 401", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/api/photos", nil)
		w := httptest.NewRecorder()

		protected(w, r)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status mismatch: got %d want %d", w.Code, http.StatusUnauthorized)
		}
		if !strings.Contains(w.Body.String(), "unauthorized") {
			t.Fatalf("expected unauthorized body, got: %s", w.Body.String())
		}
	})

	t.Run("page unauthenticated redirects to login", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/events", nil)
		w := httptest.NewRecorder()

		protected(w, r)

		if w.Code != http.StatusFound {
			t.Fatalf("status mismatch: got %d want %d", w.Code, http.StatusFound)
		}
		if got := w.Header().Get("Location"); got != "/login" {
			t.Fatalf("location mismatch: got %q want %q", got, "/login")
		}
	})

	t.Run("authenticated request passes through", func(t *testing.T) {
		hash, err := auth.HashPassword("secret-pass")
		if err != nil {
			t.Fatalf("hash password: %v", err)
		}
		h.cfg.Auth.PasswordHash = hash

		loginReq := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"password":"secret-pass"}`))
		loginReq.Header.Set("Content-Type", "application/json")
		loginResp := httptest.NewRecorder()
		h.handleLogin(loginResp, loginReq)
		if loginResp.Code != http.StatusOK {
			t.Fatalf("login status mismatch: got %d want %d", loginResp.Code, http.StatusOK)
		}

		authedReq := httptest.NewRequest(http.MethodGet, "/api/photos", nil)
		for _, c := range loginResp.Result().Cookies() {
			authedReq.AddCookie(c)
		}
		w := httptest.NewRecorder()

		protected(w, authedReq)

		if w.Code != http.StatusNoContent {
			t.Fatalf("status mismatch: got %d want %d", w.Code, http.StatusNoContent)
		}
	})

	t.Run("auth disabled allows pass-through", func(t *testing.T) {
		h.cfg.Auth.Enabled = false
		r := httptest.NewRequest(http.MethodGet, "/api/photos", nil)
		w := httptest.NewRecorder()

		protected(w, r)

		if w.Code != http.StatusNoContent {
			t.Fatalf("status mismatch: got %d want %d", w.Code, http.StatusNoContent)
		}
	})
}

func TestHandlePostSettings_PartialUpdateAndValidation(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	hash, err := auth.HashPassword("initial")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	cfg := &config.Config{
		LibraryPaths:    []config.LibraryPath{{Path: "/photos", Label: "photos"}},
		CameraWhitelist: []config.CameraEntry{},
		FilenameFilters: config.FilenameFilters{Include: []string{}, Exclude: []string{}},
		Auth:            config.AuthConfig{Enabled: false, PasswordHash: hash, SessionSecret: "session-secret"},
		DBPath:          "gallery.db",
		CacheDir:        ".cache",
		LogLevel:        "info",
		ScanWorkers:     2,
		EventGapDays:    3,
		EventGeoKm:      100,
		SessionTTLHours: 24,
	}
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	h := &Handlers{cfg: cfg, cfgPath: cfgPath, sessions: auth.NewStore(cfg.SessionTTLHours)}

	t.Run("partial update only changes intended fields", func(t *testing.T) {
		body := strings.NewReader(`{"scan_workers":8}`)
		r := httptest.NewRequest(http.MethodPost, "/api/settings", body)
		w := httptest.NewRecorder()

		h.handlePostSettings(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("status mismatch: got %d want %d", w.Code, http.StatusOK)
		}
		if h.cfg.ScanWorkers != 8 {
			t.Fatalf("scan_workers mismatch: got %d want %d", h.cfg.ScanWorkers, 8)
		}
		if h.cfg.EventGapDays != 3 {
			t.Fatalf("event_gap_days changed unexpectedly: got %d want %d", h.cfg.EventGapDays, 3)
		}
	})

	t.Run("invalid log level returns 400", func(t *testing.T) {
		before := h.cfg.LogLevel
		body := strings.NewReader(`{"log_level":"trace"}`)
		r := httptest.NewRequest(http.MethodPost, "/api/settings", body)
		w := httptest.NewRecorder()

		h.handlePostSettings(w, r)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("status mismatch: got %d want %d", w.Code, http.StatusBadRequest)
		}
		if h.cfg.LogLevel != before {
			t.Fatalf("log level changed on invalid update: got %q want %q", h.cfg.LogLevel, before)
		}
	})

	t.Run("auth toggle and password update", func(t *testing.T) {
		body := strings.NewReader(`{"auth_enabled":true,"new_password":"new-secret"}`)
		r := httptest.NewRequest(http.MethodPost, "/api/settings", body)
		w := httptest.NewRecorder()

		h.handlePostSettings(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("status mismatch: got %d want %d", w.Code, http.StatusOK)
		}
		if !h.cfg.Auth.Enabled {
			t.Fatalf("expected auth enabled")
		}
		if h.cfg.Auth.PasswordHash == "" || h.cfg.Auth.PasswordHash == "new-secret" {
			t.Fatalf("password hash not updated safely: %q", h.cfg.Auth.PasswordHash)
		}

		loginReq := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"password":"new-secret"}`))
		loginReq.Header.Set("Content-Type", "application/json")
		loginResp := httptest.NewRecorder()
		h.handleLogin(loginResp, loginReq)
		if loginResp.Code != http.StatusOK {
			t.Fatalf("login status mismatch: got %d want %d", loginResp.Code, http.StatusOK)
		}
	})

	t.Run("invalid internal library overlap returns 400 and keeps prior config", func(t *testing.T) {
		before := h.cfg.InternalLibrary
		body := strings.NewReader(`{"internal_library":{"enabled":true,"path":"/photos"}}`)
		r := httptest.NewRequest(http.MethodPost, "/api/settings", body)
		w := httptest.NewRecorder()

		h.handlePostSettings(w, r)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("status mismatch: got %d want %d", w.Code, http.StatusBadRequest)
		}
		if h.cfg.InternalLibrary != before {
			t.Fatalf("internal library changed on invalid update: got %#v want %#v", h.cfg.InternalLibrary, before)
		}
	})
}

func TestFeatureGates_InternalLibraryAndRecognition(t *testing.T) {
	t.Run("library and people endpoints return 409 when internal library disabled", func(t *testing.T) {
		h := &Handlers{cfg: &config.Config{InternalLibrary: config.InternalLibraryConfig{Enabled: false}}}

		endpoints := []func(http.ResponseWriter, *http.Request){
			h.handleLibraryStatus,
			h.handleListPeople,
		}
		for _, fn := range endpoints {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			fn(w, r)
			if w.Code != http.StatusConflict {
				t.Fatalf("status mismatch: got %d want %d", w.Code, http.StatusConflict)
			}
			if !strings.Contains(w.Body.String(), "internal library is not enabled") {
				t.Fatalf("expected gate error body, got: %s", w.Body.String())
			}
		}
	})

	t.Run("recognition endpoints return unavailable statuses", func(t *testing.T) {
		h1 := &Handlers{
			cfg:         &config.Config{InternalLibrary: config.InternalLibraryConfig{Enabled: true}},
			recognition: recognition.Status{Enabled: false, Available: false},
		}
		w1 := httptest.NewRecorder()
		r1 := httptest.NewRequest(http.MethodGet, "/api/faces/unidentified", nil)
		h1.handleUnidentifiedFaces(w1, r1)
		if w1.Code != http.StatusNotImplemented {
			t.Fatalf("status mismatch: got %d want %d", w1.Code, http.StatusNotImplemented)
		}

		h2 := &Handlers{
			cfg:         &config.Config{InternalLibrary: config.InternalLibraryConfig{Enabled: true}},
			recognition: recognition.Status{Enabled: true, Available: false, Reason: "runtime unavailable"},
		}
		w2 := httptest.NewRecorder()
		r2 := httptest.NewRequest(http.MethodGet, "/api/faces/suggestions", nil)
		h2.handleSuggestions(w2, r2)
		if w2.Code != http.StatusServiceUnavailable {
			t.Fatalf("status mismatch: got %d want %d", w2.Code, http.StatusServiceUnavailable)
		}
		var payload map[string]string
		if err := json.NewDecoder(bytes.NewReader(w2.Body.Bytes())).Decode(&payload); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if payload["error"] != "runtime unavailable" {
			t.Fatalf("error message mismatch: got %q want %q", payload["error"], "runtime unavailable")
		}
	})
}
