package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaults(t *testing.T) {
	cfg := defaults()
	if cfg.Library.Root != "/var/lib/ebookfs/library" {
		t.Errorf("Library.Root = %q, want %q", cfg.Library.Root, "/var/lib/ebookfs/library")
	}
	if cfg.Library.InboxTemp != "/var/lib/ebookfs/library/.inbox-tmp" {
		t.Errorf("Library.InboxTemp = %q, want %q", cfg.Library.InboxTemp, "/var/lib/ebookfs/library/.inbox-tmp")
	}
	if cfg.Library.IndexPath != "/var/lib/ebookfs/library/.index.db" {
		t.Errorf("Library.IndexPath = %q, want %q", cfg.Library.IndexPath, "/var/lib/ebookfs/library/.index.db")
	}
	if cfg.Reader.Convert {
		t.Errorf("Reader.Convert = true, want false")
	}
	if len(cfg.Reader.Statuses) != 2 || cfg.Reader.Statuses[0] != "unread" || cfg.Reader.Statuses[1] != "reading" {
		t.Errorf("Reader.Statuses = %v, want [unread reading]", cfg.Reader.Statuses)
	}
	if cfg.Server.Listen != "0.0.0.0:5640" {
		t.Errorf("Server.Listen = %q, want %q", cfg.Server.Listen, "0.0.0.0:5640")
	}
	if cfg.Server.Auth != "none" {
		t.Errorf("Server.Auth = %q, want %q", cfg.Server.Auth, "none")
	}
	if cfg.Log.Level != "info" {
		t.Errorf("Log.Level = %q, want %q", cfg.Log.Level, "info")
	}
	if cfg.Log.Format != "text" {
		t.Errorf("Log.Format = %q, want %q", cfg.Log.Format, "text")
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

const reqLibSection = "[library]\nroot = \"/l\"\ninbox_temp = \"/t\"\nindex_path = \"/i\"\n\n"

func TestLoad(t *testing.T) {
	t.Run("minimal valid", func(t *testing.T) {
		path := writeConfig(t, reqLibSection)
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Library.Root != "/l" {
			t.Errorf("Library.Root = %q, want %q", cfg.Library.Root, "/l")
		}
		if cfg.Reader.Convert {
			t.Errorf("Reader.Convert should default to false")
		}
	})

	t.Run("full custom", func(t *testing.T) {
		path := writeConfig(t, `
[library]
root = "/custom/root"
inbox_temp = "/custom/temp"
index_path = "/custom/index"

[reader]
statuses = ["read", "abandoned"]
convert = true
cache_dir = "/custom/cache"

[server]
listen = ":9999"
auth = "none"

[log]
level = "debug"
format = "json"
`)
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Library.Root != "/custom/root" {
			t.Errorf("Library.Root = %q", "/custom/root")
		}
		if cfg.Reader.Statuses[0] != "read" {
			t.Errorf("Reader.Statuses[0] = %q", cfg.Reader.Statuses[0])
		}
		if !cfg.Reader.Convert {
			t.Errorf("Reader.Convert should be true")
		}
		if cfg.Reader.CacheDir != "/custom/cache" {
			t.Errorf("Reader.CacheDir = %q", cfg.Reader.CacheDir)
		}
		if cfg.Log.Level != "debug" {
			t.Errorf("Log.Level = %q", cfg.Log.Level)
		}
		if cfg.Log.Format != "json" {
			t.Errorf("Log.Format = %q", cfg.Log.Format)
		}
	})

	t.Run("reader defaults", func(t *testing.T) {
		path := writeConfig(t, reqLibSection)
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Reader.CacheDir != "/var/lib/ebookfs/kepub-cache" {
			t.Errorf("Reader.CacheDir should be default, got %q", cfg.Reader.CacheDir)
		}
	})

	t.Run("invalid reader status", func(t *testing.T) {
		path := writeConfig(t, reqLibSection+`
[reader]
statuses = ["invalid_status"]
`)
		_, err := Load(path)
		if err == nil {
			t.Fatal("expected error for invalid reader status")
		}
	})

	t.Run("convert without cache dir", func(t *testing.T) {
		path := writeConfig(t, reqLibSection+`
[reader]
convert = true
cache_dir = ""
`)
		_, err := Load(path)
		if err == nil {
			t.Fatal("expected error: cache_dir required when convert=true")
		}
	})

	t.Run("convert with cache dir", func(t *testing.T) {
		path := writeConfig(t, reqLibSection+`
[reader]
convert = true
cache_dir = "/cache"
`)
		_, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
	})

	t.Run("shared-secret auth rejected", func(t *testing.T) {
		path := writeConfig(t, reqLibSection+`
[server]
auth = "shared-secret"
`)
		_, err := Load(path)
		if err == nil {
			t.Fatal("expected error: shared-secret not implemented")
		}
	})

	t.Run("invalid auth", func(t *testing.T) {
		path := writeConfig(t, reqLibSection+`
[server]
auth = "kerberos"
`)
		_, err := Load(path)
		if err == nil {
			t.Fatal("expected error: invalid auth mode")
		}
	})

	t.Run("valid log level", func(t *testing.T) {
		for _, level := range []string{"debug", "info", "warn", "error"} {
			path := writeConfig(t, reqLibSection+`
[log]
level = "`+level+`"
`)
			_, err := Load(path)
			if err != nil {
				t.Errorf("log.level %q should be valid: %v", level, err)
			}
		}
	})

	t.Run("invalid log level", func(t *testing.T) {
		path := writeConfig(t, reqLibSection+`
[log]
level = "verbose"
`)
		_, err := Load(path)
		if err == nil {
			t.Fatal("expected error for invalid log level")
		}
	})

	t.Run("valid log format", func(t *testing.T) {
		for _, format := range []string{"text", "json"} {
			path := writeConfig(t, reqLibSection+`
[log]
format = "`+format+`"
`)
			_, err := Load(path)
			if err != nil {
				t.Errorf("log.format %q should be valid: %v", format, err)
			}
		}
	})

	t.Run("invalid log format", func(t *testing.T) {
		path := writeConfig(t, reqLibSection+`
[log]
format = "xml"
`)
		_, err := Load(path)
		if err == nil {
			t.Fatal("expected error for invalid log format")
		}
	})

	t.Run("missing root", func(t *testing.T) {
		path := writeConfig(t, `
[library]
root = ""
inbox_temp = "/t"
index_path = "/i"
`)
		_, err := Load(path)
		if err == nil {
			t.Fatal("expected error: root required")
		}
	})

	t.Run("missing inbox temp", func(t *testing.T) {
		path := writeConfig(t, `
[library]
root = "/l"
inbox_temp = ""
index_path = "/i"
`)
		_, err := Load(path)
		if err == nil {
			t.Fatal("expected error: inbox_temp required")
		}
	})

	t.Run("missing index path", func(t *testing.T) {
		path := writeConfig(t, `
[library]
root = "/l"
inbox_temp = "/t"
index_path = ""
`)
		_, err := Load(path)
		if err == nil {
			t.Fatal("expected error: index_path required")
		}
	})
}
