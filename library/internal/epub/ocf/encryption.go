package ocf

import (
	"encoding/xml"
	"fmt"
	"io"

	epubxml "github.com/ramblingenzyme/ebookfs/library/internal/epub/xml"
)

// Reading META-INF/encryption.xml, which covers two unrelated things spelled
// identically: real DRM and font obfuscation. Telling them apart is the point.
// Treating obfuscation as DRM makes every book with an embedded font
// uneditable; treating DRM as readable lets an edit corrupt a protected entry.

const EncryptionPath = "META-INF/encryption.xml"

type encryptionXML struct {
	Data []struct {
		Method struct {
			Algorithm epubxml.AttrText `xml:"Algorithm,attr"`
		} `xml:"EncryptionMethod"`
		// Keep the nesting: > works on an element field, but an attr-mode tag
		// containing it is read as a literal attribute name and matches nothing.
		Ref struct {
			URI epubxml.AttrURL `xml:"URI,attr"`
		} `xml:"CipherData>CipherReference"`
	} `xml:"EncryptedData"`
}

// obfuscationAlgorithms are the two font-obfuscation schemes. They appear like
// real DRM but are not encryption; calibre treats them as readable and so do we,
// so a book with obfuscated fonts stays editable.
var obfuscationAlgorithms = map[string]bool{
	"http://ns.adobe.com/pdf/enc#RC":     true,
	"http://www.idpf.org/2008/embedding": true,
}

// encryptionInfo records which zip entries are listed in META-INF/encryption.xml
// and under which algorithm, so a real-DRM entry can be distinguished from a
// merely font-obfuscated one (see obfuscationAlgorithms).
type EncryptionInfo struct {
	algorithms map[string]string // zip entry name -> EncryptionMethod algorithm
}

func NewEncryptionInfo(r io.Reader) (*EncryptionInfo, error) {
	var doc encryptionXML
	if err := xml.NewDecoder(r).Decode(&doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", EncryptionPath, err)
	}

	info := &EncryptionInfo{algorithms: make(map[string]string, len(doc.Data))}
	for _, d := range doc.Data {
		algo := string(d.Method.Algorithm)
		if algo == "" {
			continue
		}
		// Keyed under every name the URI could mean, because isEncrypted is
		// asked about zip entry names and a producer may have written either
		// form into both files.
		for _, name := range d.Ref.URI.Candidates() {
			if name != "" {
				info.algorithms[name] = algo
			}
		}
	}
	return info, nil
}

// isEncrypted reports whether name is protected by real encryption (as opposed
// to font obfuscation). An entry absent from encryption.xml is not encrypted.
func (e *EncryptionInfo) IsEncrypted(name string) bool {
	if e == nil {
		return false
	}
	algo, ok := e.algorithms[name]
	return ok && !obfuscationAlgorithms[algo]
}
