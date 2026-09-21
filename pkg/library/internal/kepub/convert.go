package kepub

import (
	"archive/zip"
	"context"
	"io"

	kepubify "github.com/pgaskin/kepubify/v4/kepub"
)

// convert hands kepubify a *zip.Reader, which implements fs.FS, so kepubify
// keeps the original zip metadata and does not re-compress unchanged entries.
func convert(ctx context.Context, w io.Writer, r io.ReaderAt, size int64) error {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return err
	}
	return kepubify.NewConverter().Convert(ctx, w, zr)
}
