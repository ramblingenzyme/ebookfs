package main

import (
	"flag"
	"log"

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

	fs.StartServer(lib, library.NewExporter(cfg.Reader, lib),
		cfg.Server.Listen, cfg.Library.InboxTemp)
}
