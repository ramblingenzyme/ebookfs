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

	lib, err := library.Open(cfg)
	if err != nil {
		log.Fatalf("opening library: %v", err)
	}

	readerCfg := fs.ReaderConfig{Statuses: cfg.Reader.Statuses, Convert: cfg.Reader.Convert}
	fs.StartServer(lib, lib.Exporter(), readerCfg, cfg.Server.Listen, cfg.Library.InboxTemp)
}
