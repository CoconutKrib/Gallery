package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"

	"github.com/halleck/gallery/internal/api"
	"github.com/halleck/gallery/internal/cluster"
	"github.com/halleck/gallery/internal/config"
	"github.com/halleck/gallery/internal/db"
	"github.com/halleck/gallery/internal/logging"
	"github.com/halleck/gallery/internal/scan"
)

//go:embed all:web
var webFS embed.FS

func main() {
	cfgPath := flag.String("config", "./config.json", "path to config.json")
	port := flag.Int("port", 8080, "HTTP port to listen on")
	doScan := flag.Bool("scan", false, "run a scan of all library paths and exit")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		slog.Error("loading config", "err", err)
		os.Exit(1)
	}

	cleanupLog, err := logging.Setup(cfg.LogFile, cfg.LogLevel)
	if err != nil {
		slog.Error("setting up logging", "err", err)
		os.Exit(1)
	}
	defer cleanupLog()

	database, err := db.Open(cfg.DBPath)
	if err != nil {
		slog.Error("opening database", "err", err)
		os.Exit(1)
	}
	defer database.Close()

	if err := os.MkdirAll(cfg.CacheDir, 0o755); err != nil {
		slog.Error("creating cache dir", "err", err)
		os.Exit(1)
	}

	if *doScan {
		if len(cfg.LibraryPaths) == 0 {
			slog.Error("no library_paths configured in config.json")
			os.Exit(1)
		}
		for _, lp := range cfg.LibraryPaths {
			lpID, err := db.UpsertLibraryPath(database, lp.Path, lp.Label)
			if err != nil {
				slog.Error("scan: upsert library path failed", "path", lp.Path, "err", err)
				continue
			}
			scanner, err := scan.NewScanner(cfg, database, lpID)
			if err != nil {
				slog.Error("scan: creating scanner failed", "path", lp.Path, "err", err)
				continue
			}
			slog.Info("scan: starting", "path", lp.Path, "label", lp.Label)
			stats, err := scanner.Run(lp.Path)
			if err != nil {
				slog.Error("scan: failed", "path", lp.Path, "err", err)
			}
			slog.Info("scan: done",
				"path", lp.Path,
				"found", stats.Found, "skipped", stats.Skipped,
				"ingested", stats.Ingested, "duplicate", stats.Duplicate,
				"errors", stats.Errors)
		}
		// Re-cluster after scan.
		slog.Info("cluster: running event clustering")
		if err := cluster.Run(database, cfg.EventGapDays, cfg.EventGeoKm); err != nil {
			slog.Error("cluster: failed", "err", err)
		} else {
			slog.Info("cluster: done")
		}
		return
	}

	// HTTP server.
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		slog.Error("embedding web assets", "err", err)
		os.Exit(1)
	}
	handlers := api.NewHandlers(database, cfg, *cfgPath)
	mux := http.NewServeMux()
	handlers.RegisterRoutes(mux, http.FS(sub))
	addr := fmt.Sprintf(":%d", *port)
	slog.Info("Gallery listening", "addr", addr)
	if err := http.ListenAndServe(addr, logging.HTTPMiddleware(mux)); err != nil {
		slog.Error("server error", "err", err)
		os.Exit(1)
	}
}
