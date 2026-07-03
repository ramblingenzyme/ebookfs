package kepub

import (
	"archive/zip"
	"context"
	"io"

	kepubify "github.com/pgaskin/kepubify/v4/kepub"
)

// convert reads the source epub (r, size bytes) and writes its kepub rendition
// to w. The source is wrapped in a *zip.Reader, which implements fs.FS, so
// kepubify preserves the original zip metadata and avoids re-compressing
// unchanged entries.
func convert(ctx context.Context, w io.Writer, r io.ReaderAt, size int64) error {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return err
	}
	return kepubify.NewConverter().Convert(ctx, w, zr)
}
