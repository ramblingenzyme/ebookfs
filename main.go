package main

import (
	"context"
	"flag"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ramblingenzyme/ebookfs/internal/config"
	"github.com/ramblingenzyme/ebookfs/internal/frontend"
	"github.com/ramblingenzyme/ebookfs/internal/fs"
	"github.com/ramblingenzyme/ebookfs/internal/httpfrontend"
	"github.com/ramblingenzyme/ebookfs/internal/kosync"
	"github.com/ramblingenzyme/ebookfs/internal/opds"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

func setupLogging(cfg config.LogConfig) {
	levels := map[string]slog.Level{
		"debug": slog.LevelDebug,
		"info":  slog.LevelInfo,
		"warn":  slog.LevelWarn,
		"error": slog.LevelError,
	}
	opts := &slog.HandlerOptions{Level: levels[cfg.Level]}
	var h slog.Handler
	if cfg.Format == "json" {
		h = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		h = slog.NewTextHandler(os.Stderr, opts)
	}
	slog.SetDefault(slog.New(h))
}

// fatal logs at error level, which no valid log.level filters, and exits.
// log.Fatalf bridges at info level and could be silenced.
func fatal(msg string, err error) {
	slog.Error(msg, "error", err)
	os.Exit(1)
}

// opdsExporter reuses the reader's exporter whenever the two want the same
// rendition. Both convert into each book's .sidecar/ directory, so at most one
// kepub file exists per book.
//
// Statuses is left empty. It decides reader/ membership through Includes,
// which the catalog's Renderer does not declare and never calls.
func opdsExporter(lib *library.Library, cfg *config.Config, readerExp library.Exporter) (library.Exporter, error) {
	if cfg.OPDS.Convert == cfg.Reader.Convert {
		return readerExp, nil
	}
	return lib.Exporter(library.ReaderConfig{
		Convert: cfg.OPDS.Convert,
	})
}

func main() {
	configPath := flag.String("config", "/etc/ebookfs/config.toml", "path to config file")
	forceReindex := flag.Bool("reindex", false, "force a full index rebuild from disk")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		// Logging isn't configured yet; fall back to plain stdlib output.
		log.Fatalf("loading config: %v", err)
	}
	setupLogging(cfg.Log)

	var opts []library.Option
	if *forceReindex {
		opts = append(opts, library.WithForceReindex())
	}
	lib, err := library.Open(library.Config(cfg.Library), opts...)
	if err != nil {
		fatal("opening library", err)
	}

	exp, err := lib.Exporter(library.ReaderConfig(cfg.Reader))
	if err != nil {
		fatal("creating exporter", err)
	}

	srv, err := fs.New(lib, exp, fs.Config{
		Listen:           cfg.Server.Listen,
		SearchTTL:        cfg.Search.HandleTTL,
		SearchMaxHandles: cfg.Search.MaxHandles,
	})
	if err != nil {
		fatal("setting up server", err)
	}
	frontends := []frontend.Frontend{srv}

	if cfg.HTTP.Listen != "" {
		httpSrv := httpfrontend.New(httpfrontend.Config{
			Listen: cfg.HTTP.Listen,
		})

		// Register OPDS handler
		opdsExp, err := opdsExporter(lib, cfg, exp)
		if err != nil {
			fatal("creating OPDS exporter", err)
		}
		opdsHandler := opds.NewHandler(lib, opdsExp, cfg.OPDS.BaseURL)
		httpSrv.Handle("/opds/", opdsHandler)
		slog.Info("OPDS catalog enabled", "path", "/opds/")

		// Register kosync handler if credentials are configured
		if cfg.KOSync.Username != "" && cfg.KOSync.Credential != "" {
			// Initialize kosync mapping and hook
			kosyncDir := cfg.Library.Root + "/.kosync"
			mapping, err := kosync.LoadMapping(kosyncDir)
			if err != nil {
				slog.Warn("kosync mapping corrupt or missing, rebuilding", "error", err)
				// Create an empty mapping and rebuild it from the library
				mapping = kosync.NewEmptyMapping(kosyncDir)
				if err := mapping.Rebuild(lib); err != nil {
					fatal("rebuilding kosync mapping", err)
				}
			}

			// Register kosync hook to compute document IDs
			hook := kosync.NewHook(mapping)
			lib.AddHook(hook)

			// Create kosync HTTP handler
			kosyncHandler := kosync.NewHandler(lib, mapping, kosync.Config{
				Username:         cfg.KOSync.Username,
				PasswordHash:     cfg.KOSync.Credential,
				PathPrefix:       "/sync",
				ReadingThreshold: cfg.KOSync.ReadingThreshold,
				ReadThreshold:    cfg.KOSync.ReadThreshold,
			})
			httpSrv.Handle("/sync/", kosyncHandler)
			slog.Info("kosync enabled", "path", "/sync/")
		} else {
			slog.Info("kosync disabled", "reason", "credentials not configured")
		}

		frontends = append(frontends, httpSrv)
	} else {
		slog.Info("HTTP server disabled", "reason", "http.listen is empty")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// The library closes before the exit status is reported, so a frontend
	// failure still leaves the index shut down cleanly.
	runErr := frontend.Run(ctx, 10*time.Second, frontends...)
	if err := lib.Close(); err != nil {
		slog.Error("closing library", "error", err)
	}
	if runErr != nil {
		fatal("serving", runErr)
	}

	slog.Info("server stopped")
}
