package ocf

import (
	"encoding/xml"
	"fmt"
	"io"

	epubxml "github.com/ramblingenzyme/ebookfs/library/internal/epub/xml"
)

// Reading META-INF/encryption.xml, which an epub uses for two unrelated things:
// real DRM, and font obfuscation. They are spelled identically — an EncryptedData
// entry naming a zip entry and an algorithm — so telling them apart is a matter
// of recognising the two obfuscation algorithms by URI.
//
// That distinction is the whole point of this file. Treating obfuscation as DRM
// would make every book with an embedded font uneditable; treating DRM as
// readable would let an edit corrupt a protected entry.

const EncryptionPath = "META-INF/encryption.xml"

type encryptionXML struct {
	Data []struct {
		Method struct {
			Algorithm epubxml.AttrText `xml:"Algorithm,attr"`
		} `xml:"EncryptionMethod"`
		// Keep the nesting. The > path works because it sits on this element
		// field; an attr-mode tag containing > is read as a literal attribute
		// name and would silently match nothing.
		Ref struct {
			URI epubxml.AttrURL `xml:"URI,attr"`
		} `xml:"CipherData>CipherReference"`
	} `xml:"EncryptedData"`
}

// obfuscationAlgorithms are the two font-obfuscation schemes the EPUB ecosystem
// uses. They appear in encryption.xml exactly like real DRM but are not actually
// encryption — calibre deliberately treats them as readable, and so do we, so a
// book with obfuscated fonts stays editable.
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
