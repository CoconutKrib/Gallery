package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type LibraryPath struct {
	Path  string `json:"path"`
	Label string `json:"label"`
}

type CameraEntry struct {
	Make  string `json:"make"`
	Model string `json:"model"`
}

type FilenameFilters struct {
	Include []string `json:"include"`
	Exclude []string `json:"exclude"`
}

type AuthConfig struct {
	Enabled       bool   `json:"enabled"`
	PasswordHash  string `json:"password_hash"`
	SessionSecret string `json:"session_secret"`
}

type Config struct {
	LibraryPaths    []LibraryPath   `json:"library_paths"`
	CameraWhitelist []CameraEntry   `json:"camera_whitelist"`
	FilenameFilters FilenameFilters `json:"filename_filters"`
	Auth            AuthConfig      `json:"auth"`
	DBPath          string          `json:"db_path"`
	CacheDir        string          `json:"cache_dir"`
	LogFile         string          `json:"log_file"`
	LogLevel        string          `json:"log_level"`
	ScanWorkers     int             `json:"scan_workers"`
	EventGapDays    int             `json:"event_gap_days"`
	EventGeoKm      float64         `json:"event_geo_km"`
	SessionTTLHours int             `json:"session_ttl_hours"`
}

func defaults() Config {
	return Config{
		LibraryPaths:    []LibraryPath{},
		CameraWhitelist: []CameraEntry{},
		FilenameFilters: FilenameFilters{
			Include: []string{},
			Exclude: []string{},
		},
		Auth: AuthConfig{
			Enabled: false,
		},
		DBPath:          "./gallery.db",
		CacheDir:        "./.cache",
		LogFile:         "",
		LogLevel:        "info",
		ScanWorkers:     4,
		EventGapDays:    2,
		EventGeoKm:      500,
		SessionTTLHours: 24,
	}
}

// Load reads config from path. If the file does not exist, a default config
// is written to path and returned.
func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		cfg := defaults()
		if err := ensureSessionSecret(&cfg); err != nil {
			return nil, err
		}
		if err := save(path, &cfg); err != nil {
			return nil, fmt.Errorf("writing default config: %w", err)
		}
		return &cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("opening config: %w", err)
	}
	defer f.Close()

	cfg := defaults()
	if err := json.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	if err := ensureSessionSecret(&cfg); err != nil {
		return nil, err
	}
	// Persist back in case session secret was just generated.
	if err := save(path, &cfg); err != nil {
		return nil, fmt.Errorf("saving config after secret generation: %w", err)
	}
	return &cfg, nil
}

// Save writes cfg to path atomically (write temp, rename).
func Save(path string, cfg *Config) error {
	return save(path, cfg)
}

func save(path string, cfg *Config) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("opening temp config: %w", err)
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if encErr := enc.Encode(cfg); encErr != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("encoding config: %w", encErr)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

func ensureSessionSecret(cfg *Config) error {
	if cfg.Auth.SessionSecret != "" {
		return nil
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return fmt.Errorf("generating session secret: %w", err)
	}
	cfg.Auth.SessionSecret = hex.EncodeToString(b)
	return nil
}
