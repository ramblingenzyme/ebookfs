package main

import (
	"flag"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/ramblingenzyme/ebookfs/fs"
	"github.com/ramblingenzyme/ebookfs/library"
	"github.com/ramblingenzyme/ebookfs/library/config"
)

// setupLogging installs a slog handler built from cfg as the default logger.
// The stdlib log calls throughout the codebase route through it at info level,
// so log.level filters them and log.format = "json" applies to them too.
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

	lib, err := library.Open(cfg.Library, *forceReindex)
	if err != nil {
		fatal("opening library", err)
	}

	exp, err := lib.Exporter(cfg.Reader)
	if err != nil {
		fatal("creating exporter", err)
	}

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		log.Print("shutting down…")
		lib.Close()
		os.Exit(0)
	}()

	fs.StartServer(lib, exp, cfg.Server.Listen)
}
