package cos

import (
	"bytes"
	"regexp"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

var (
	utf16BOM = []byte{0xFE, 0xFF}
	utf8BOM  = []byte{0xEF, 0xBB, 0xBF}
)

// Text decodes a text string (§7.9.2.2): UTF-16BE or UTF-8 after a byte order
// mark, otherwise PDFDocEncoding.
func Text(s String) string {
	switch {
	case bytes.HasPrefix(s, utf16BOM):
		return stripLanguageEscapes(decodeUTF16(s[2:]))
	case bytes.HasPrefix(s, utf8BOM):
		return stripLanguageEscapes(string(s[3:]))
	default:
		return decodePDFDoc(s)
	}
}

// TextString encodes s as a text string: unchanged when PDFDocEncoding spells
// it as ASCII does, and as UTF-16BE otherwise, since PDF 1.x readers predate
// UTF-8 text strings.
func TextString(s string) String {
	if sharedWithASCII(s) {
		return String(s)
	}

	out := append([]byte(nil), utf16BOM...)
	for _, u := range utf16.Encode([]rune(s)) {
		out = append(out, byte(u>>8), byte(u))
	}

	return out
}

func sharedWithASCII(s string) bool {
	return !strings.ContainsFunc(s, func(r rune) bool {
		_, differs := pdfDoc[byte(r)]

		return r >= utf8.RuneSelf || differs
	})
}

func decodeUTF16(b []byte) string {
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = uint16(b[2*i])<<8 | uint16(b[2*i+1])
	}

	return string(utf16.Decode(u))
}

// stripLanguageEscapes drops §7.9.2.2's language codes, ESC lang ESC, which
// mark the text's language and are not part of it.
func stripLanguageEscapes(s string) string { return languageEscape.ReplaceAllString(s, "") }

var languageEscape = regexp.MustCompile("\x1b[^\x1b]*\x1b")

// pdfDoc holds the PDFDocEncoding code points (Annex D.2) that differ from
// ISO Latin-1. Codes it leaves undefined read as their Latin-1 code point.
var pdfDoc = map[byte]rune{
	0x18: '˘', 0x19: 'ˇ', 0x1A: 'ˆ', 0x1B: '˙', 0x1C: '˝', 0x1D: '˛', 0x1E: '˚', 0x1F: '˜',
	0x80: '•', 0x81: '†', 0x82: '‡', 0x83: '…', 0x84: '—', 0x85: '–', 0x86: 'ƒ', 0x87: '⁄',
	0x88: '‹', 0x89: '›', 0x8A: '−', 0x8B: '‰', 0x8C: '„', 0x8D: '“', 0x8E: '”', 0x8F: '‘',
	0x90: '’', 0x91: '‚', 0x92: '™', 0x93: 'ﬁ', 0x94: 'ﬂ', 0x95: 'Ł', 0x96: 'Œ', 0x97: 'Š',
	0x98: 'Ÿ', 0x99: 'Ž', 0x9A: 'ı', 0x9B: 'ł', 0x9C: 'œ', 0x9D: 'š', 0x9E: 'ž', 0xA0: '€',
}

func decodePDFDoc(b []byte) string {
	var sb strings.Builder

	for _, c := range b {
		if r, ok := pdfDoc[c]; ok {
			sb.WriteRune(r)
		} else {
			sb.WriteRune(rune(c))
		}
	}

	return sb.String()
}
