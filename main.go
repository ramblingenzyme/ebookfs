package main

import (
	"flag"
	"log"

	"github.com/ramblingenzyme/ebookfs/internal/config"
	"github.com/ramblingenzyme/ebookfs/internal/fs"
)

func main() {
	configPath := flag.String("config", "/etc/ebookfs/config.toml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	fs.StartServer(cfg)
}
