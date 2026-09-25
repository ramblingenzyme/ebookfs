package ocf

import (
	"encoding/xml"
	"fmt"
	"io"
)

const EncryptionPath = "META-INF/encryption.xml"

type encryptionXML struct {
	Data []struct {
		Method struct {
			Algorithm AttrText `xml:"Algorithm,attr"`
		} `xml:"EncryptionMethod"`
		// Nested because encoding/xml reads "a>b,attr" as a literal attribute
		// name.
		Ref struct {
			URI AttrURL `xml:"URI,attr"`
		} `xml:"CipherData>CipherReference"`
	} `xml:"EncryptedData"`
}

// obfuscationAlgorithms are the font-obfuscation schemes, which calibre also
// treats as readable.
var obfuscationAlgorithms = map[string]bool{
	"http://ns.adobe.com/pdf/enc#RC":     true,
	"http://www.idpf.org/2008/embedding": true,
}

// EncryptionInfo records which entries encryption.xml lists, and under which
// algorithm. The file lists real DRM and font obfuscation alike. Obfuscation
// must not block an edit, since every book with an embedded font would become
// uneditable. DRM must, since editing a protected entry corrupts it.
type EncryptionInfo struct {
	algorithms map[string]string // zip entry name -> algorithm
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
		// Keyed under both names the URI could mean, since a producer may have
		// written either form into both files.
		for _, name := range d.Ref.URI.Candidates() {
			if name != "" {
				info.algorithms[name] = algo
			}
		}
	}
	return info, nil
}

// IsEncrypted reports whether the entry is under real encryption rather than
// font obfuscation. A nil EncryptionInfo, from an epub with no encryption.xml,
// encrypts nothing.
func (e *EncryptionInfo) IsEncrypted(name string) bool {
	if e == nil {
		return false
	}
	algo, ok := e.algorithms[name]
	return ok && !obfuscationAlgorithms[algo]
}
