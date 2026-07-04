package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/ramblingenzyme/ebookfs/fs"
	"github.com/ramblingenzyme/ebookfs/library"
	"github.com/ramblingenzyme/ebookfs/library/config"
)

func main() {
	configPath := flag.String("config", "/etc/ebookfs/config.toml", "path to config file")
	forceReindex := flag.Bool("reindex", false, "force a full index rebuild from disk")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	lib, err := library.Open(cfg.Library, *forceReindex)
	if err != nil {
		log.Fatalf("opening library: %v", err)
	}

	exp, err := lib.Exporter(cfg.Reader)
	if err != nil {
		log.Fatalf("creating exporter: %v", err)
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
