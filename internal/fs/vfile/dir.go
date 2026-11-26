package vfile

import (
	"log/slog"

	"github.com/ramblingenzyme/go9p/fs"
)

// AddChild adds n to d and logs a refusal. StaticDir refuses only a duplicate
// name, which leaves n out of the listing while the caller carries on.
func AddChild(d fs.ModDir, n fs.FSNode) {
	if err := d.AddChild(n); err != nil {
		slog.Error("9P entry dropped", "dir", d.Stat().Name, "entry", n.Stat().Name, "error", err)
	}
}
