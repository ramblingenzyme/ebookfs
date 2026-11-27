package cos

import (
	"bytes"
	"compress/zlib"
	"encoding/ascii85"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"
)

// MaxDecoded caps a stream's decoded size. A stream's length is whatever the
// file claims, and Flate inflates a few kilobytes into gigabytes.
const MaxDecoded = 32 << 20

// ErrUnsupportedFilter reports a filter this package does not decode. The
// image filters (DCT, JPX, CCITT, JBIG2) and LZW never apply to the
// cross-reference, object and metadata streams it reads.
var ErrUnsupportedFilter = errors.New("unsupported stream filter")

// Decode applies the filters a stream dictionary names (§7.4), in order.
// Filter and DecodeParms must be direct objects. An entry of Filter that is
// not a name is skipped, and the parameters pair with the names that remain.
func Decode(d Dict, data []byte) ([]byte, error) {
	params := asArray(d["DecodeParms"])
	i := 0

	for _, o := range asArray(d["Filter"]) {
		f, ok := o.(Name)
		if !ok {
			continue
		}

		var p Dict
		if i < len(params) {
			p, _ = params[i].(Dict)
		}

		i++

		var err error
		if data, err = decodeOne(f, p, data); err != nil {
			return nil, err
		}
	}

	return data, nil
}

// asArray reads a value §7.4 allows either alone or as an array.
func asArray(o Object) Array {
	switch v := o.(type) {
	case nil:
		return nil
	case Array:
		return v
	default:
		return Array{v}
	}
}

func decodeOne(f Name, p Dict, data []byte) ([]byte, error) {
	switch f {
	case "FlateDecode", "Fl":
		out, err := inflate(data)
		if err != nil {
			return nil, err
		}

		return unpredict(p, out)
	case "ASCIIHexDecode", "AHx":
		return decodeHex(data)
	case "ASCII85Decode", "A85":
		return ascii85Decode(data)
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedFilter, f)
	}
}

// inflate keeps what decoded before a truncated or checksum-less end, as
// readers do. Such streams are common from writers that miscount Length.
func inflate(data []byte) ([]byte, error) {
	zr, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, malformed("flate: %v", err)
	}

	out, err := io.ReadAll(io.LimitReader(zr, MaxDecoded+1))
	if len(out) > MaxDecoded {
		return nil, malformed("stream decodes to over %d bytes", MaxDecoded)
	}

	if err != nil && len(out) == 0 {
		return nil, malformed("flate: %v", err)
	}

	return out, nil
}

// decodeHex decodes hexadecimal digits up to any ">", skipping white space. A
// final odd digit is followed by an implied 0 (§7.3.4.3, §7.4.2).
func decodeHex(data []byte) ([]byte, error) {
	data, _, _ = bytes.Cut(data, []byte(">"))

	digits := bytes.Map(func(r rune) rune {
		if r < utf8.RuneSelf && isSpace(byte(r)) {
			return -1
		}

		return r
	}, data)

	if len(digits)%2 == 1 {
		digits = append(digits, '0')
	}

	out := make([]byte, hex.DecodedLen(len(digits)))
	if _, err := hex.Decode(out, digits); err != nil {
		return nil, malformed("hexadecimal data: %v", err)
	}

	return out, nil
}

func ascii85Decode(data []byte) ([]byte, error) {
	data, _, _ = bytes.Cut(data, []byte("~>"))
	data = bytes.TrimPrefix(bytes.TrimSpace(data), []byte("<~"))

	// "z" expands one byte to four.
	out := make([]byte, 4*len(data)+4)

	n, _, err := ascii85.Decode(out, data, true)
	if err != nil {
		return nil, malformed("ASCII85Decode: %v", err)
	}

	return out[:n], nil
}

// unpredict reverses a Flate predictor (§7.4.4.4). Cross-reference streams use
// PNG predictors almost universally.
func unpredict(p Dict, data []byte) ([]byte, error) {
	pred, _ := Int(p["Predictor"])
	if pred <= 1 {
		return data, nil
	}

	colors, bpc, columns := param(p, "Colors", 1), param(p, "BitsPerComponent", 8), param(p, "Columns", 1)
	bpp := max(1, (colors*bpc+7)/8)
	row := (colors*bpc*columns + 7) / 8

	switch {
	case pred == 2 && bpc == 8:
		return unpredictTIFF(data, bpp, row), nil
	case pred >= 10:
		return unpredictPNG(data, bpp, row)
	default:
		return nil, fmt.Errorf("%w: predictor %d with %d bits per component", ErrUnsupportedFilter, pred, bpc)
	}
}

func param(p Dict, key Name, def int) int {
	if v, ok := Int(p[key]); ok && v > 0 && v < 1<<16 {
		return int(v)
	}

	return def
}

func unpredictTIFF(data []byte, bpp, row int) []byte {
	for start := 0; start < len(data); start += row {
		r := data[start:min(start+row, len(data))]
		for i := bpp; i < len(r); i++ {
			r[i] += r[i-bpp]
		}
	}

	return data
}

// unpredictPNG decodes rows that each begin with a PNG filter type byte. A
// short final row is decoded as far as it goes.
func unpredictPNG(data []byte, bpp, row int) ([]byte, error) {
	out := make([]byte, 0, len(data))
	prev := make([]byte, row)

	// Each row is decoded in place, in the buffer inflate allocated.
	for start := 0; start < len(data); start += row + 1 {
		cur := data[start+1 : min(start+1+row, len(data))]
		if err := unfilterRow(data[start], cur, prev, bpp); err != nil {
			return nil, err
		}

		out = append(out, cur...)
		prev = cur
	}

	return out, nil
}

// unfilterRow reverses one PNG filter in place: a is the byte bpp to the
// left, b the byte above, c the byte above a.
func unfilterRow(kind byte, cur, prev []byte, bpp int) error {
	for i := range cur {
		var a, c byte
		if i >= bpp {
			a, c = cur[i-bpp], prev[i-bpp]
		}

		b := prev[i]

		switch kind {
		case 0:
		case 1:
			cur[i] += a
		case 2:
			cur[i] += b
		case 3:
			cur[i] += byte((int(a) + int(b)) / 2)
		case 4:
			cur[i] += paeth(a, b, c)
		default:
			return malformed("PNG predictor row type %d", kind)
		}
	}

	return nil
}

func paeth(a, b, c byte) byte {
	p := int(a) + int(b) - int(c)
	pa, pb, pc := max(p-int(a), int(a)-p), max(p-int(b), int(b)-p), max(p-int(c), int(c)-p)

	switch {
	case pa <= pb && pa <= pc:
		return a
	case pb <= pc:
		return b
	default:
		return c
	}
}
