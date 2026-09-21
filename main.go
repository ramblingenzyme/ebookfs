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

	// Start returns without error once Shutdown is called.
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(cfg.Server.Listen)
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	slog.Info("shutting down…")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Warn("9P shutdown deadline exceeded, forcing close", "error", err)
	}

	if err := <-errCh; err != nil {
		slog.Error("9P server exited with error", "error", err)
	}

	if err := lib.Close(); err != nil {
		slog.Error("closing library", "error", err)
	}

	slog.Info("server stopped")
}
