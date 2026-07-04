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
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	lib, err := library.Open(cfg.Library)
	if err != nil {
		log.Fatalf("opening library: %v", err)
	}

	exp := library.NewExporter(cfg.Reader, lib)

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		log.Print("shutting down…")
		exp.Close()
		lib.Close()
		os.Exit(0)
	}()

	fs.StartServer(lib, exp, cfg.Server.Listen)
}
