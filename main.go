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
	"github.com/ramblingenzyme/ebookfs/internal/fs"
	"github.com/ramblingenzyme/ebookfs/internal/opds"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// setupLogging installs a slog handler built from cfg as the default logger.
// Every slog call in the codebase goes through it, so log.level filters by
// severity and log.format = "json" applies uniformly.
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

// fatal logs at error level — never filtered by any valid log.level — and
// exits. log.Fatalf would be bridged at info level and could be silenced.
func fatal(msg string, err error) {
	slog.Error(msg, "error", err)
	os.Exit(1)
}

// opdsExporter builds the catalog's exporter, reusing the reader's whenever
// the two want the same rendition. Both convert into reader.cache_dir, so at
// most one kepub cache exists in the process: either they share this instance,
// or the half that does not convert has no cache at all.
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

	srv, err := fs.SetupServer(lib, exp, cfg.Search.HandleTTL, cfg.Search.MaxHandles)
	if err != nil {
		fatal("setting up server", err)
	}

	// Start the 9P listener in a background goroutine so the main
	// goroutine can receive signals. Serve returns without error
	// when Shutdown is called.
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(cfg.Server.Listen)
	}()

	// The OPDS catalog is opt-in: no listen address, no listener.
	var opdsSrv *opds.Server
	opdsDone := make(chan struct{})
	if cfg.OPDS.Listen != "" {
		opdsExp, err := opdsExporter(lib, cfg, exp)
		if err != nil {
			fatal("creating OPDS exporter", err)
		}
		opdsSrv = opds.SetupServer(lib, opdsExp, cfg.OPDS.BaseURL)
		go func() {
			defer close(opdsDone)
			// Logged here rather than after the shutdown wait below: a bind
			// failure happens at startup, and an error only read at shutdown
			// leaves the operator staring at a port that never answers.
			if err := opdsSrv.Start(cfg.OPDS.Listen); err != nil {
				slog.Error("OPDS listener failed", "listen", cfg.OPDS.Listen, "error", err)
			}
		}()
	} else {
		slog.Info("OPDS catalog disabled", "reason", "opds.listen is empty")
	}

	// Main goroutine: wait for a signal, then initiate graceful
	// shutdown with a deadline.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	slog.Info("shutting down…")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Warn("9P shutdown deadline exceeded, forcing close", "error", err)
	}
	if opdsSrv != nil {
		if err := opdsSrv.Shutdown(shutdownCtx); err != nil {
			slog.Warn("OPDS shutdown deadline exceeded, forcing close", "error", err)
		}
		<-opdsDone // the listener has already reported its own failure
	}

	// Wait for Serve to return (confirming the listener is fully down).
	if err := <-errCh; err != nil {
		slog.Error("9P server exited with error", "error", err)
	}

	// Close the library after the server is fully down.
	if err := lib.Close(); err != nil {
		slog.Error("closing library", "error", err)
	}

	slog.Info("server stopped")
}
