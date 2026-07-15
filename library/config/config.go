package config

import (
	"fmt"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

type Config struct {
	Library LibraryConfig `toml:"library"`
	Reader  ReaderConfig  `toml:"reader"`
	Server  ServerConfig  `toml:"server"`
	Search  SearchConfig  `toml:"search"`
	Log     LogConfig     `toml:"log"`
}

type LibraryConfig struct {
	Root      string `toml:"root"`
	InboxTemp string `toml:"inbox_temp"`
	IndexPath string `toml:"index_path"`
}

// SearchConfig configures the search/ directory in the 9P namespace.
type SearchConfig struct {
	HandleTTL  time.Duration `toml:"handle_ttl"`  // e.g. "30m"
	MaxHandles int           `toml:"max_handles"` // e.g. 100
}

// ReaderConfig configures the reader/ rsync export. Statuses selects which books
// appear; Convert toggles kepub conversion (false serves the original epub);
// CacheDir holds converted kepubs and MUST live outside Library.Root so the
// store walk never treats cached files as books.
type ReaderConfig struct {
	Statuses []string `toml:"statuses"`
	Convert  bool     `toml:"convert"`
	CacheDir string   `toml:"cache_dir"`
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
			IndexPath: "/var/lib/ebookfs/library/.index.db",
		},
		Search: SearchConfig{
			HandleTTL:  30 * time.Minute,
			MaxHandles: 100,
		},
		Reader: ReaderConfig{
			Statuses: []string{model.StatusUnread, model.StatusReading},
			Convert:  false,
			CacheDir: "/var/lib/ebookfs/kepub-cache",
		},
		Server: ServerConfig{
			Listen: "0.0.0.0:5640",
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
		// Reserved in the schema but not wired into the 9P server yet. Accepting
		// it would silently serve unauthenticated, so refuse to start instead.
		return fmt.Errorf(`server.auth = "shared-secret" is not implemented yet; use "none"`)
	default:
		return fmt.Errorf(`server.auth must be "none" or "shared-secret", got %q`, c.Server.Auth)
	}
	return nil
}

func (c *Config) validateReader() error {
	for _, s := range c.Reader.Statuses {
		if !model.IsValidStatus(s) {
			return fmt.Errorf("reader.statuses contains invalid status %q: must be %s", s, model.StatusList())
		}
	}
	if c.Reader.Convert && c.Reader.CacheDir == "" {
		return fmt.Errorf("reader.cache_dir is required when reader.convert = true")
	}
	return nil
}

func (c *Config) validateSearch() error {
	if c.Search.HandleTTL < 0 {
		return fmt.Errorf("search.handle_ttl must be >= 0, got %s", c.Search.HandleTTL)
	}

	if c.Search.MaxHandles < 0 {
		return fmt.Errorf("search.max_handles must be >= 0, got %d", c.Search.MaxHandles)
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
	if c.Library.IndexPath == "" {
		return fmt.Errorf("library.index_path is required")
	}

	if err := c.validateReader(); err != nil {
		return err
	}

	if err := c.validateAuth(); err != nil {
		return err
	}

	if err := c.validateSearch(); err != nil {
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
