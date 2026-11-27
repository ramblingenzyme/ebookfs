package cos

import (
	"bytes"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// Append writes o in PDF syntax. Dictionary keys are sorted, so the same
// object always serializes to the same bytes.
func Append(b []byte, o Object) []byte {
	switch v := o.(type) {
	case Name:
		return appendName(b, v)
	case String:
		return appendString(b, v)
	case Ref:
		return fmt.Appendf(b, "%d %d R", v.Num, v.Gen)
	case Array:
		return appendArray(b, v)
	case Dict:
		return appendDict(b, v)
	case *Stream:
		return appendStream(b, v)
	default:
		return appendScalar(b, o)
	}
}

func appendScalar(b []byte, o Object) []byte {
	switch v := o.(type) {
	case nil:
		return append(b, "null"...)
	case bool:
		return strconv.AppendBool(b, v)
	case int64:
		return strconv.AppendInt(b, v, 10)
	case float64:
		// §7.3.3: a real has no exponent form.
		return strconv.AppendFloat(b, v, 'f', -1, 64)
	default:
		panic(fmt.Sprintf("cos: cannot serialize %T", o))
	}
}

func appendArray(b []byte, a Array) []byte {
	b = append(b, '[')

	for i, o := range a {
		if i > 0 {
			b = append(b, ' ')
		}

		b = Append(b, o)
	}

	return append(b, ']')
}

func appendDict(b []byte, d Dict) []byte {
	b = append(b, "<<"...)

	for _, k := range slices.Sorted(maps.Keys(d)) {
		b = appendName(b, k)
		b = append(b, ' ')
		b = Append(b, d[k])
	}

	return append(b, ">>"...)
}

func appendStream(b []byte, s *Stream) []byte {
	d := make(Dict, len(s.Dict)+1)
	maps.Copy(d, s.Dict)

	d["Length"] = int64(len(s.Data))

	b = appendDict(b, d)
	b = append(b, "\nstream\n"...)
	b = append(b, s.Data...)

	return append(b, "\nendstream"...)
}

func appendName(b []byte, n Name) []byte {
	b = append(b, '/')

	for i := range len(n) {
		c := n[i]
		if c < '!' || c > '~' || c == '#' || isDelim(c) {
			b = fmt.Appendf(b, "#%02X", c)
		} else {
			b = append(b, c)
		}
	}

	return b
}

// appendString writes printable ASCII as a literal string and anything else,
// such as UTF-16, as hexadecimal, which no reader can mangle.
func appendString(b []byte, s String) []byte {
	if bytes.ContainsFunc(s, func(r rune) bool { return r < ' ' || r > '~' }) {
		return fmt.Appendf(b, "<%X>", []byte(s))
	}

	return append(append(append(b, '('), literalEscaper.Replace(string(s))...), ')')
}

var literalEscaper = strings.NewReplacer(`\`, `\\`, "(", `\(`, ")", `\)`)
