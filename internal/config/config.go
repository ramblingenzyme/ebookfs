package config

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Library LibraryConfig `toml:"library"`
	Index   IndexConfig   `toml:"index"`
	Epub    EpubConfig    `toml:"epub"`
	Server  ServerConfig  `toml:"server"`
	Log     LogConfig     `toml:"log"`
}

type LibraryConfig struct {
	Root      string `toml:"root"`
	InboxTemp string `toml:"inbox_temp"`
}

type IndexConfig struct {
	Path string `toml:"path"`
}

type EpubConfig struct {
	EbookMeta string `toml:"ebook_meta"`
}

type ServerConfig struct {
	Listen           string `toml:"listen"`
	Auth             string `toml:"auth"`
	SharedSecretFile string `toml:"shared_secret_file"`
}

type LogConfig struct {
	Level  string `toml:"level"`
	Format string `toml:"format"`
}

func Load(path string) (*Config, error) {
	cfg := defaults()

	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return cfg, nil
}

func defaults() *Config {
	return &Config{
		Library: LibraryConfig{
			Root:      "/var/lib/ebookfs/library",
			InboxTemp: "/var/lib/ebookfs/library/.inbox-tmp",
		},
		Index: IndexConfig{
			Path: "/var/lib/ebookfs/library/.index.db",
		},
		Epub: EpubConfig{
			EbookMeta: "/usr/bin/ebook-meta",
		},
		Server: ServerConfig{
			Listen: "tcp!0.0.0.0!5640",
			Auth:   "none",
		},
		Log: LogConfig{
			Level:  "info",
			Format: "text",
		},
	}
}

func (c *Config) validateAuth() error {
	switch c.Server.Auth {
	case "none":
		// OK
	case "shared-secret":
		if c.Server.SharedSecretFile == "" {
			return fmt.Errorf("server.shared_secret_file is required when auth = shared-secret")
		}
	default:
		return fmt.Errorf(`server.auth must be "none" or "shared-secret", got %q`, c.Server.Auth)
	}
	return nil
}

func (c *Config) validate() error {
	if c.Library.Root == "" {
		return fmt.Errorf("library.root is required")
	}
	if c.Library.InboxTemp == "" {
		return fmt.Errorf("library.inbox_temp is required")
	}
	if c.Index.Path == "" {
		return fmt.Errorf("index.path is required")
	}
	if c.Epub.EbookMeta == "" {
		return fmt.Errorf("epub.ebook_meta is required")
	}

	info, err := os.Stat(c.Epub.EbookMeta)
	if err != nil {
		return fmt.Errorf("epub.ebook_meta %q: %w", c.Epub.EbookMeta, err)
	}
	if info.Mode()&0111 == 0 {
		return fmt.Errorf("epub.ebook_meta %q: not executable", c.Epub.EbookMeta)
	}

	if err := c.validateAuth(); err != nil {
		return err
	}

	switch c.Log.Level {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("log.level must be one of debug/info/warn/error, got %q", c.Log.Level)
	}

	switch c.Log.Format {
	case "text", "json":
	default:
		return fmt.Errorf(`log.format must be "text" or "json", got %q`, c.Log.Format)
	}

	return nil
}
