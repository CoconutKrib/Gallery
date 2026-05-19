package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"

	"github.com/halleck/gallery/internal/api"
	"github.com/halleck/gallery/internal/cluster"
	"github.com/halleck/gallery/internal/config"
	"github.com/halleck/gallery/internal/db"
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
		log.Fatalf("loading config: %v", err)
	}

	database, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("opening database: %v", err)
	}
	defer database.Close()

	if err := os.MkdirAll(cfg.CacheDir, 0o755); err != nil {
		log.Fatalf("creating cache dir: %v", err)
	}

	if *doScan {
		if len(cfg.LibraryPaths) == 0 {
			log.Fatal("no library_paths configured in config.json")
		}
		for _, lp := range cfg.LibraryPaths {
			lpID, err := db.UpsertLibraryPath(database, lp.Path, lp.Label)
			if err != nil {
				log.Printf("[scan] upsert library path %q: %v", lp.Path, err)
				continue
			}
			scanner, err := scan.NewScanner(cfg, database, lpID)
			if err != nil {
				log.Printf("[scan] creating scanner for %q: %v", lp.Path, err)
				continue
			}
			log.Printf("[scan] starting scan of %q (%s)", lp.Path, lp.Label)
			stats, err := scanner.Run(lp.Path)
			if err != nil {
				log.Printf("[scan] scan of %q failed: %v", lp.Path, err)
			}
			log.Printf("[scan] %q done — found:%d skipped:%d ingested:%d duplicate:%d errors:%d",
				lp.Path, stats.Found, stats.Skipped, stats.Ingested, stats.Duplicate, stats.Errors)
		}
		// Re-cluster after scan.
		log.Printf("[cluster] running event clustering")
		if err := cluster.Run(database, cfg.EventGapDays, cfg.EventGeoKm); err != nil {
			log.Printf("[cluster] error: %v", err)
		} else {
			log.Printf("[cluster] done")
		}
		return
	}

	// HTTP server.
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatalf("embedding web assets: %v", err)
	}
	handlers := api.NewHandlers(database, cfg, *cfgPath)
	mux := http.NewServeMux()
	handlers.RegisterRoutes(mux, http.FS(sub))
	addr := fmt.Sprintf(":%d", *port)
	log.Printf("Gallery listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
