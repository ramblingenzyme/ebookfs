package config

import (
	"fmt"
	"net/url"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/ramblingenzyme/ebookfs/internal/book"
)

type Config struct {
	Library LibraryConfig `toml:"library"`
	Reader  ReaderConfig  `toml:"reader"`
	Server  ServerConfig  `toml:"server"`
	HTTP    HTTPConfig    `toml:"http"`
	OPDS    OPDSConfig    `toml:"opds"`
	KOSync  KOSyncConfig  `toml:"kosync"`
	Search  SearchConfig  `toml:"search"`
	Log     LogConfig     `toml:"log"`
}

type LibraryConfig struct {
	Root      string `toml:"root"`
	InboxTemp string `toml:"inbox_temp"`
	IndexPath string `toml:"index_path"`
}

type SearchConfig struct {
	HandleTTL  time.Duration `toml:"handle_ttl"`  // e.g. "30m"
	MaxHandles int           `toml:"max_handles"` // e.g. 100
}

// ReaderConfig is library.ReaderConfig with the toml tags, and must stay
// field-identical to it since main converts between the two.
// library.ReaderConfig documents what each field means.
type ReaderConfig struct {
	Statuses []string `toml:"statuses"`
	Convert  bool     `toml:"convert"`
}

// HTTPConfig configures the shared HTTP listener.
type HTTPConfig struct {
	Listen string `toml:"listen"` // e.g. "0.0.0.0:8080"
}

// OPDSConfig configures the OPDS catalog.
type OPDSConfig struct {
	Enable  bool   `toml:"enable"`   // explicit toggle to enable/disable OPDS
	BaseURL string `toml:"base_url"` // absolute, scheme://host; trailing slashes are stripped

	// Convert is the catalog's rendition choice, separate from [reader]'s.
	// Both convert into each book's .sidecar/ directory, since a book's kepub
	// is the same file whoever asked for it.
	Convert bool `toml:"convert"`
}

// KOSyncConfig configures the kosync progress sync server.
type KOSyncConfig struct {
	Enable           bool    `toml:"enable"`            // explicit toggle to enable/disable kosync
	MappingPath      string  `toml:"mapping_path"`      // path to mapping directory (default: <library_root>/.kosync)
	Username         string  `toml:"username"`          // pre-provisioned username
	Credential       string  `toml:"credential"`        // pre-provisioned credential (MD5 of password)
	ReadingThreshold float64 `toml:"reading_threshold"` // percentage to mark as "reading" (default: 0.05)
	ReadThreshold    float64 `toml:"read_threshold"`    // percentage to mark as "read" (default: 0.95)
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

	// Apply defaults that depend on other config values
	if cfg.KOSync.MappingPath == "" {
		cfg.KOSync.MappingPath = cfg.Library.Root + "/.kosync"
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
			Statuses: []string{book.StatusUnread, book.StatusReading},
			Convert:  false,
		},
		Server: ServerConfig{
			Listen: "0.0.0.0:5640",
			Auth:   "none",
		},
		KOSync: KOSyncConfig{
			ReadingThreshold: 0.05,
			ReadThreshold:    0.95,
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
		// Accepting this would serve unauthenticated, so refuse to start.
		return fmt.Errorf(`server.auth = "shared-secret" is not implemented yet; use "none"`)
	default:
		return fmt.Errorf(`server.auth must be "none" or "shared-secret", got %q`, c.Server.Auth)
	}
	return nil
}

func (c *Config) validateReader() error {
	for _, s := range c.Reader.Statuses {
		if !book.IsValidStatus(s) {
			return fmt.Errorf("reader.statuses contains invalid status %q: must be %s", s, book.StatusList())
		}
	}
	return nil
}

// validateOPDS checks the catalog's base URL.
func (c *Config) validateOPDS() error {
	if c.OPDS.BaseURL == "" {
		return nil
	}
	u, err := url.Parse(c.OPDS.BaseURL)
	if err != nil {
		return fmt.Errorf("opds.base_url is not a URL: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("opds.base_url must be absolute (scheme://host), got %q", c.OPDS.BaseURL)
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

func (c *Config) validateKOSync() error {
	if !c.KOSync.Enable {
		return nil
	}
	// kosync is enabled, so username and credential are required
	if c.KOSync.Username == "" {
		return fmt.Errorf("kosync.username is required when kosync.enable is true")
	}
	if c.KOSync.Credential == "" {
		return fmt.Errorf("kosync.credential is required when kosync.enable is true")
	}

	// Validate thresholds
	if c.KOSync.ReadingThreshold < 0 || c.KOSync.ReadingThreshold > 1 {
		return fmt.Errorf("kosync.reading_threshold must be between 0 and 1, got %f", c.KOSync.ReadingThreshold)
	}
	if c.KOSync.ReadThreshold < 0 || c.KOSync.ReadThreshold > 1 {
		return fmt.Errorf("kosync.read_threshold must be between 0 and 1, got %f", c.KOSync.ReadThreshold)
	}
	if c.KOSync.ReadingThreshold >= c.KOSync.ReadThreshold {
		return fmt.Errorf("kosync.reading_threshold (%f) must be less than read_threshold (%f)",
			c.KOSync.ReadingThreshold, c.KOSync.ReadThreshold)
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

	if err := c.validateOPDS(); err != nil {
		return err
	}

	if err := c.validateKOSync(); err != nil {
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
