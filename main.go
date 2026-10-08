package main

import (
	"context"
	"crypto/md5"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ramblingenzyme/ebookfs/internal/config"
	"github.com/ramblingenzyme/ebookfs/internal/frontend"
	"github.com/ramblingenzyme/ebookfs/internal/fs"
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

	ninepSrv, err := fs.New(lib, exp, fs.Config{
		Listen:           cfg.Server.Listen,
		SearchTTL:        cfg.Search.HandleTTL,
		SearchMaxHandles: cfg.Search.MaxHandles,
	})
	if err != nil {
		fatal("setting up server", err)
	}

	runner := frontend.NewRunner(cfg.HTTP.Listen)
	runner.Register(ninepSrv)

	// Register OPDS as HTTPFrontend if enabled
	if cfg.OPDS.Enable {
		opdsExp, err := opdsExporter(lib, cfg, exp)
		if err != nil {
			fatal("creating OPDS exporter", err)
		}

		opdsSrv := opds.New(lib, opdsExp, opds.Config{
			BaseURL: cfg.HTTP.BaseURL,
		})
		if err := runner.RegisterHTTP(opdsSrv); err != nil {
			fatal("registering OPDS", err)
		}

		slog.Info("OPDS catalog enabled", "prefix", opdsSrv.Prefix())
	} else {
		slog.Info("OPDS catalog disabled", "reason", "opds.enable is false")
	}

	// Register kosync as HTTPFrontend if enabled
	if cfg.KOSync.Enable {
		// Compute MD5 hash of plaintext password
		passwordHash := fmt.Sprintf("%x", md5.Sum([]byte(cfg.KOSync.Password)))

		// Create kosync server (handles mapping and hook setup internally)
		kosyncSrv, err := kosync.New(lib, kosync.Config{
			MappingPath:      cfg.KOSync.MappingPath,
			Username:         cfg.KOSync.Username,
			PasswordHash:     passwordHash,
			PathPrefix:       "/sync",
			ReadingThreshold: cfg.KOSync.ReadingThreshold,
			ReadThreshold:    cfg.KOSync.ReadThreshold,
		})
		if err != nil {
			fatal("initializing kosync", err)
		}

		if err := runner.RegisterHTTP(kosyncSrv); err != nil {
			fatal("registering kosync", err)
		}

		slog.Info("kosync enabled", "prefix", kosyncSrv.Prefix())
	} else {
		slog.Info("kosync disabled", "reason", "kosync.enable is false")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// The library closes before the exit status is reported, so a frontend
	// failure still leaves the index shut down cleanly.
	runErr := runner.Run(ctx, 10*time.Second)
	if err := lib.Close(); err != nil {
		slog.Error("closing library", "error", err)
	}
	if runErr != nil {
		fatal("serving", runErr)
	}

	slog.Info("server stopped")
}
