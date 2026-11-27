// Package cos reads and writes PDF objects (ISO 32000-2 §7.2-7.3), decodes
// stream filters (§7.4) and converts text strings (§7.9.2).
//
// An Object is one of: nil (null), bool, int64, float64, Name, String, Array,
// Dict, Ref or *Stream.
package cos

import (
	"errors"
	"fmt"
)

// ErrMalformed reports bytes that are not the PDF syntax they should be. An
// error from reading the file itself is never wrapped in it.
var ErrMalformed = errors.New("malformed PDF")

func malformed(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrMalformed, fmt.Sprintf(format, args...))
}

type (
	Object any
	Name   string
	// String holds a string object's bytes, after escapes are decoded. Text
	// converts one holding a text string.
	String []byte
	Array  []Object
	Dict   map[Name]Object
)

// Ref is an indirect reference, "num gen R".
type Ref struct {
	Num int64
	Gen int64
}

// Stream is a stream object. A parsed stream knows only where its data begins,
// since its length may be an indirect reference the scanner cannot resolve. A
// stream to be written carries its encoded Data, and Append sets its Length.
type Stream struct {
	Dict   Dict
	Offset int64
	Data   []byte
}

// Int returns o as an integer, accepting a real with no fractional part, as
// some writers emit.
func Int(o Object) (int64, bool) {
	switch v := o.(type) {
	case int64:
		return v, true
	case float64:
		if v == float64(int64(v)) {
			return int64(v), true
		}
	}

	return 0, false
}
