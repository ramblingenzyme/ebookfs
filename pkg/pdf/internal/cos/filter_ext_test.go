package cos_test

import (
	"bytes"
	"compress/zlib"
	"encoding/ascii85"
	"encoding/hex"
	"testing"

	"github.com/ramblingenzyme/ebookfs/pkg/pdf/internal/cos"
)

func deflate(b []byte) []byte {
	var buf bytes.Buffer

	zw := zlib.NewWriter(&buf)
	_, _ = zw.Write(b)
	_ = zw.Close()

	return buf.Bytes()
}

// The vectors are worked by hand from §7.4.4.4 and the PNG specification, one
// row per PNG filter type, so each row's arithmetic can be checked on paper.
func TestDecode(t *testing.T) {
	a85 := make([]byte, ascii85.MaxEncodedLen(5))
	a85 = a85[:ascii85.Encode(a85, []byte("hello"))]

	png := []byte{
		1, 1, 1, 1, // Sub:     1 2 3
		2, 1, 1, 1, // Up:      2 3 4
		3, 1, 0, 0, // Average: 2 2 3
		4, 0, 0, 0, // Paeth:   2 2 3
	}

	for _, tc := range []struct {
		name string
		dict cos.Dict
		data []byte
		want []byte
	}{
		{"no filter", cos.Dict{}, []byte("raw"), []byte("raw")},
		{"ASCIIHex", cos.Dict{"Filter": cos.Name("ASCIIHexDecode")}, []byte("68 65\n6C6c6F>junk"), []byte("hello")},
		{"ASCIIHex odd", cos.Dict{"Filter": cos.Name("AHx")}, []byte("414>"), []byte("A@")},
		{"ASCII85", cos.Dict{"Filter": cos.Name("ASCII85Decode")}, append(append([]byte("<~"), a85...), "~>junk"...), []byte("hello")},
		{"Flate", cos.Dict{"Filter": cos.Name("FlateDecode")}, deflate([]byte("hello")), []byte("hello")},
		{"chain", cos.Dict{"Filter": cos.Array{cos.Name("AHx"), cos.Name("Fl")}}, []byte(hex.EncodeToString(deflate([]byte("hello"))) + ">"), []byte("hello")},
		{
			"PNG predictors",
			cos.Dict{"Filter": cos.Name("FlateDecode"), "DecodeParms": cos.Dict{"Predictor": int64(12), "Columns": int64(3)}},
			deflate(png),
			[]byte{1, 2, 3, 2, 3, 4, 2, 2, 3, 2, 2, 3},
		},
		{
			"TIFF predictor",
			cos.Dict{"Filter": cos.Name("FlateDecode"), "DecodeParms": cos.Dict{"Predictor": int64(2), "Columns": int64(3)}},
			deflate([]byte{1, 1, 1, 5, 1, 1}),
			[]byte{1, 2, 3, 5, 6, 7},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := cos.Decode(tc.dict, tc.data)
			if err != nil || !bytes.Equal(got, tc.want) {
				t.Errorf("Decode = %v, %v, want %v", got, err, tc.want)
			}
		})
	}
}

func TestDecodeRejectsWhatItCannotRead(t *testing.T) {
	for name, d := range map[string]cos.Dict{
		"unknown filter":   {"Filter": cos.Name("DCTDecode")},
		"bad hex digit":    {"Filter": cos.Name("ASCIIHexDecode")},
		"bad PNG row type": {"Filter": cos.Name("FlateDecode"), "DecodeParms": cos.Dict{"Predictor": int64(12)}},
	} {
		t.Run(name, func(t *testing.T) {
			data := []byte("zz>")
			if name == "bad PNG row type" {
				data = deflate([]byte{9, 0})
			}

			if _, err := cos.Decode(d, data); err == nil {
				t.Error("Decode succeeded")
			}
		})
	}
}
