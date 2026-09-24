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
// rendition. Both convert into reader.cache_dir, so at most one kepub cache
// exists in the process.
//
// Statuses is left empty. It decides reader/ membership through Includes,
// which the catalog's Renderer does not declare and never calls.
func opdsExporter(lib *library.Library, cfg *config.Config, readerExp library.Exporter) (library.Exporter, error) {
	if cfg.OPDS.Convert == cfg.Reader.Convert {
		return readerExp, nil
	}
	return lib.Exporter(library.ReaderConfig{
		Convert:  cfg.OPDS.Convert,
		CacheDir: cfg.Reader.CacheDir,
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

	if cfg.OPDS.Listen != "" {
		opdsExp, err := opdsExporter(lib, cfg, exp)
		if err != nil {
			fatal("creating OPDS exporter", err)
		}
		frontends = append(frontends, opds.New(lib, opdsExp, opds.Config{
			Listen:  cfg.OPDS.Listen,
			BaseURL: cfg.OPDS.BaseURL,
		}))
	} else {
		slog.Info("OPDS catalog disabled", "reason", "opds.listen is empty")
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
