package cos

import (
	"encoding/hex"
	"io"
	"strconv"
	"strings"
)

// maxDepth bounds array and dictionary nesting, so a file of a million "["
// cannot exhaust the stack. Real documents nest a handful of levels deep.
const maxDepth = 100

const bufSize = 4096

// Scanner parses objects from a file by offset. It buffers reads, so seeking
// around a large file costs one ReadAt per window rather than per byte.
type Scanner struct {
	r     io.ReaderAt
	size  int64
	pos   int64
	buf   []byte
	off   int64
	ioErr error
}

func NewScanner(r io.ReaderAt, size, pos int64) *Scanner {
	return &Scanner{r: r, size: size, pos: pos}
}

// Object parses the next direct object, resolving nothing.
func (s *Scanner) Object() (Object, error) { return s.object(0) }

// Indirect parses "num gen obj" and the object after it. For a stream, the
// data is not read, and Stream.Offset is where it begins. endobj is not
// required, since writers that omit it are common and nothing follows from it.
func (s *Scanner) Indirect() (Ref, Object, error) {
	v, err := s.Ints(2)
	if err != nil {
		return Ref{}, nil, err
	}

	if err := s.Expect("obj"); err != nil {
		return Ref{}, nil, err
	}

	ref := Ref{v[0], v[1]}

	o, err := s.Object()
	if err != nil {
		return ref, nil, err
	}

	d, ok := o.(Dict)
	if !ok || !s.AtKeyword("stream") {
		return ref, o, nil
	}

	// §7.3.8.1 requires CRLF or LF after "stream". A lone CR is not allowed,
	// but is skipped too, since the data cannot begin with it.
	s.pos += int64(len("stream"))
	s.skip('\r')
	s.skip('\n')

	return ref, &Stream{Dict: d, Offset: s.pos}, nil
}

// skip consumes c if it is next.
func (s *Scanner) skip(c byte) bool {
	if b, ok := s.peek(); ok && b == c {
		s.pos++

		return true
	}

	return false
}

// Int parses an integer token.
func (s *Scanner) Int() (int64, error) {
	tok, err := s.Token()
	if err != nil {
		return 0, err
	}

	n, err := strconv.ParseInt(tok, 10, 64)
	if err != nil {
		return 0, malformed("at %d: want an integer, got %q", s.pos, tok)
	}

	return n, nil
}

// Ints parses n integer tokens.
func (s *Scanner) Ints(n int) ([]int64, error) {
	out := make([]int64, n)

	for i := range out {
		var err error
		if out[i], err = s.Int(); err != nil {
			return nil, err
		}
	}

	return out, nil
}

// Expect consumes kw or fails.
func (s *Scanner) Expect(kw string) error {
	if !s.AtKeyword(kw) {
		return malformed("at %d: want %q", s.pos, kw)
	}

	s.pos += int64(len(kw))

	return nil
}

// AtKeyword reports whether kw is next, after any white space.
func (s *Scanner) AtKeyword(kw string) bool {
	s.skipSpace()

	start := s.pos
	defer func() { s.pos = start }()

	for i := range len(kw) {
		if b, ok := s.next(); !ok || b != kw[i] {
			return false
		}
	}

	// A keyword of regular characters ends where they do; ">>" ends itself.
	if isDelim(kw[len(kw)-1]) {
		return true
	}

	b, ok := s.peek()

	return !ok || isSpace(b) || isDelim(b)
}

// object dispatches on the first byte. At the end of the data, that byte reads
// as 0, and keywordObject reports the end.
func (s *Scanner) object(depth int) (Object, error) {
	s.skipSpace()

	b, _ := s.peek()
	if strings.IndexByte("/(<[", b) >= 0 {
		s.pos++
	}

	switch {
	case b == '/':
		return s.name(), nil
	case b == '(':
		return s.literal()
	case b == '<':
		if s.skip('<') {
			return s.dict(depth)
		}

		return s.hex()
	case b == '[':
		return s.array(depth)
	case strings.IndexByte("+-.0123456789", b) >= 0:
		return s.numberOrRef()
	default:
		return s.keywordObject()
	}
}

func (s *Scanner) tooDeep(depth int) error {
	if depth >= maxDepth {
		return malformed("at %d: objects nested deeper than %d", s.pos, maxDepth)
	}

	return nil
}

func (s *Scanner) keywordObject() (Object, error) {
	start := s.pos

	tok, err := s.Token()
	if err != nil {
		return nil, err
	}

	if v, ok := keywords[tok]; ok {
		return v, nil
	}

	return nil, malformed("at %d: unexpected %q", start, tok)
}

var keywords = map[string]Object{"true": true, "false": false, "null": nil}

func (s *Scanner) array(depth int) (Array, error) {
	if err := s.tooDeep(depth); err != nil {
		return nil, err
	}

	a := Array{}

	for {
		s.skipSpace()

		if s.skip(']') {
			return a, nil
		}

		o, err := s.object(depth + 1)
		if err != nil {
			return nil, err
		}

		a = append(a, o)
	}
}

func (s *Scanner) dict(depth int) (Dict, error) {
	if err := s.tooDeep(depth); err != nil {
		return nil, err
	}

	d := Dict{}

	for {
		s.skipSpace()

		b, ok := s.peek()

		switch {
		case !ok:
			return nil, s.eof()
		case b == '>':
			return d, s.Expect(">>")
		case !s.skip('/'):
			return nil, malformed("at %d: dictionary key is not a name", s.pos)
		}

		key := s.name()

		v, err := s.object(depth + 1)
		if err != nil {
			return nil, err
		}

		// §7.3.7: a null value is the same as an absent entry.
		if v != nil {
			d[key] = v
		}
	}
}

// numberOrRef reads a number, then looks ahead for "gen R", which turns a
// non-negative integer into a reference.
func (s *Scanner) numberOrRef() (Object, error) {
	start := s.pos

	tok, err := s.Token()
	if err != nil {
		return nil, err
	}

	n, err := strconv.ParseInt(tok, 10, 64)
	if err != nil {
		f, err := strconv.ParseFloat(tok, 64)
		if err != nil {
			return nil, malformed("at %d: bad number %q", start, tok)
		}

		return f, nil
	}

	if n < 0 {
		return n, nil
	}

	after := s.pos
	if gen, err := s.Int(); err == nil && gen >= 0 && s.AtKeyword("R") {
		s.pos++

		return Ref{n, gen}, nil
	}

	s.pos = after

	return n, nil
}

// name decodes a name's #xx escapes. A "#" without two digits after it is kept
// as written, as PDF 1.1 files spell it.
func (s *Scanner) name() Name {
	raw := s.regular()
	out := raw[:0]

	for i := 0; i < len(raw); i++ {
		var v [1]byte
		if raw[i] == '#' && i+2 < len(raw) {
			if _, err := hex.Decode(v[:], raw[i+1:i+3]); err == nil {
				out = append(out, v[0])
				i += 2

				continue
			}
		}

		out = append(out, raw[i])
	}

	return Name(out)
}

func (s *Scanner) hex() (String, error) {
	var raw []byte

	for {
		b, ok := s.next()
		if !ok {
			return nil, s.eof()
		}

		if b == '>' {
			return decodeHex(raw)
		}

		raw = append(raw, b)
	}
}

func (s *Scanner) literal() (String, error) {
	var out []byte

	depth := 1

	for {
		b, ok := s.next()
		if !ok {
			return nil, s.eof()
		}

		switch b {
		case '\\':
			out = s.escape(out)

			continue
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return out, nil
			}
		case '\r':
			// §7.3.4.2: an unescaped end of line of any kind reads as LF.
			s.skip('\n')

			b = '\n'
		}

		out = append(out, b)
	}
}

var escapes = map[byte]byte{'n': '\n', 'r': '\r', 't': '\t', 'b': '\b', 'f': '\f', '(': '(', ')': ')', '\\': '\\'}

// escape decodes what follows a backslash.
func (s *Scanner) escape(out []byte) []byte {
	b, ok := s.next()

	switch {
	case !ok:
		return out
	case escapes[b] != 0:
		return append(out, escapes[b])
	case b == '\r' || b == '\n':
		// §7.3.4.2: a backslash before an end of line continues the string.
		if b == '\r' {
			s.skip('\n')
		}

		return out
	case b >= '0' && b <= '7':
		return append(out, s.octal(b))
	default:
		// §7.3.4.2: a backslash before any other character is ignored.
		return append(out, b)
	}
}

// octal reads up to three octal digits, the first already consumed. High-order
// overflow is ignored, as §7.3.4.2 says.
func (s *Scanner) octal(first byte) byte {
	v := first - '0'

	for range 2 {
		b, ok := s.peek()
		if !ok || b < '0' || b > '7' {
			break
		}

		s.pos++
		v = v<<3 | (b - '0')
	}

	return v
}

// Token reads a run of regular characters, such as a keyword.
func (s *Scanner) Token() (string, error) {
	s.skipSpace()

	out := s.regular()
	if len(out) == 0 {
		if _, ok := s.peek(); !ok {
			return "", s.eof()
		}

		return "", malformed("at %d: want a token", s.pos)
	}

	return string(out), nil
}

// regular reads a run of the characters §7.2.3 calls regular: neither white
// space nor delimiters.
func (s *Scanner) regular() []byte {
	var out []byte

	for {
		b, ok := s.peek()
		if !ok || isSpace(b) || isDelim(b) {
			return out
		}

		s.pos++

		out = append(out, b)
	}
}

func (s *Scanner) skipSpace() {
	for {
		b, ok := s.peek()
		if !ok {
			return
		}

		switch {
		case isSpace(b):
			s.pos++
		case b == '%':
			s.skipLine()
		default:
			return
		}
	}
}

func (s *Scanner) skipLine() {
	for {
		b, ok := s.next()
		if !ok || b == '\n' || b == '\r' {
			return
		}
	}
}

func (s *Scanner) next() (byte, bool) {
	b, ok := s.peek()
	if ok {
		s.pos++
	}

	return b, ok
}

func (s *Scanner) peek() (byte, bool) {
	if s.pos < 0 || s.pos >= s.size {
		return 0, false
	}

	if s.pos < s.off || s.pos >= s.off+int64(len(s.buf)) {
		if !s.fill() {
			return 0, false
		}
	}

	return s.buf[s.pos-s.off], true
}

func (s *Scanner) fill() bool {
	if s.buf == nil {
		s.buf = make([]byte, bufSize)
	}

	s.buf = s.buf[:min(int64(bufSize), s.size-s.pos)]

	n, err := s.r.ReadAt(s.buf, s.pos)
	if n < len(s.buf) && err != nil && err != io.EOF {
		s.ioErr = err
	}

	s.buf, s.off = s.buf[:n], s.pos

	return n > 0
}

// eof reports running out of bytes. A read error says nothing about the file's
// syntax, so it passes through unwrapped.
func (s *Scanner) eof() error {
	if s.ioErr != nil {
		return s.ioErr
	}

	return malformed("at %d: unexpected end of data", s.pos)
}

// isSpace and isDelim are §7.2.3's white-space and delimiter sets.
func isSpace(b byte) bool { return strings.IndexByte("\x00\t\n\f\r ", b) >= 0 }
func isDelim(b byte) bool { return strings.IndexByte("()<>[]{}/%", b) >= 0 }
