package epub

import (
	"encoding/xml"
	"fmt"
	"io"
)

// Reading META-INF/encryption.xml, which an epub uses for two unrelated things:
// real DRM, and font obfuscation. They are spelled identically — an EncryptedData
// entry naming a zip entry and an algorithm — so telling them apart is a matter
// of recognising the two obfuscation algorithms by URI.
//
// That distinction is the whole point of this file. Treating obfuscation as DRM
// would make every book with an embedded font uneditable; treating DRM as
// readable would let an edit corrupt a protected entry.

const encryptionPath = "META-INF/encryption.xml"

type encryptionXML struct {
	Data []struct {
		Method struct {
			Algorithm string `xml:"Algorithm,attr"`
		} `xml:"EncryptionMethod"`
		Ref struct {
			URI string `xml:"URI,attr"`
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
type encryptionInfo struct {
	algorithms map[string]string // zip entry name -> EncryptionMethod algorithm
}

func newEncryptionInfo(r io.Reader) (*encryptionInfo, error) {
	var doc encryptionXML
	if err := xml.NewDecoder(r).Decode(&doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", encryptionPath, err)
	}

	info := &encryptionInfo{algorithms: make(map[string]string, len(doc.Data))}
	for _, d := range doc.Data {
		if d.Ref.URI != "" && d.Method.Algorithm != "" {
			// Keyed by the zip entry name isEncrypted will be asked about.
			// CipherReference/@URI is a URL like the container's full-path, so a
			// space arrives as %20 and the raw value would key the map by a name
			// no entry has — silently reporting an encrypted entry as readable.
			info.algorithms[rootfilePath(d.Ref.URI)] = d.Method.Algorithm
		}
	}
	return info, nil
}

// isEncrypted reports whether name is protected by real encryption (as opposed
// to font obfuscation). An entry absent from encryption.xml is not encrypted.
func (e *encryptionInfo) isEncrypted(name string) bool {
	if e == nil {
		return false
	}
	algo, ok := e.algorithms[name]
	return ok && !obfuscationAlgorithms[algo]
}
