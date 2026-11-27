// Package xmp reads and edits the properties of an XMP packet (Adobe XMP
// Specification Part 1).
//
// Edit splices bytes rather than re-serializing a tree, so everything it does
// not touch survives as written: unknown namespaces, qualifiers, comments,
// formatting and the packet wrapper. A property is replaced whole, wherever it
// was, by a copy in one new rdf:Description, which §7.4 allows alongside the
// others.
package xmp

import (
	"bytes"
	"cmp"
	"encoding/xml"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode/utf8"
)

const (
	NSRDF = "http://www.w3.org/1999/02/22-rdf-syntax-ns#"
	NSXML = "http://www.w3.org/XML/1998/namespace"
	NSDC  = "http://purl.org/dc/elements/1.1/"
	NSXMP = "http://ns.adobe.com/xap/1.0/"
	NSPDF = "http://ns.adobe.com/pdf/1.3/"
	// NSXMPIdq qualifies an xmp:Identifier with its scheme (XMP Part 2).
	NSXMPIdq = "http://ns.adobe.com/xmp/Identifier/qual/1.0/"
	// NSCalibre and NSCalibreSI are calibre's own, which it writes into every
	// PDF it edits: series, sort title and author sort.
	NSCalibre   = "http://calibre-ebook.com/xmp-namespace"
	NSCalibreSI = "http://calibre-ebook.com/xmp-namespace-series-index"
)

// NSPRISM lists the PRISM Basic namespaces, one per version, that
// publishers write a DOI, ISBN or publication date under.
var NSPRISM = []string{
	"http://prismstandard.org/namespaces/basic/3.0/",
	"http://prismstandard.org/namespaces/basic/2.1/",
	"http://prismstandard.org/namespaces/basic/2.0/",
	"http://prismstandard.org/namespaces/basic/1.2/",
}

var prefixes = map[string]string{
	NSRDF: "rdf", NSDC: "dc", NSXMP: "xmp", NSPDF: "pdf",
	NSXMPIdq: "xmpidq", NSCalibre: "calibre", NSCalibreSI: "calibreSI",
}

// declarations binds every prefix Edit writes. Declaring them on the new
// rdf:Description itself means none can clash with the packet's own.
var declarations = func() string {
	var b strings.Builder
	for _, ns := range slices.Sorted(maps.Keys(prefixes)) {
		b.WriteString(` xmlns:` + prefixes[ns] + `="` + ns + `"`)
	}

	return b.String()
}()

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// Packet is a parsed XMP packet.
type Packet struct {
	raw []byte
	rdf *node
}

// Parse requires well-formed XML holding an rdf:RDF element.
func Parse(data []byte) (*Packet, error) {
	root, err := parseTree(data)
	if err != nil {
		return nil, err
	}

	rdf := root.find(rdf("RDF"))
	if rdf == nil {
		return nil, errors.New("xmp: no rdf:RDF element")
	}

	return &Packet{raw: data, rdf: rdf}, nil
}

// Form is how a property's value is written (§6.3, §8.2.2).
type Form int

const (
	Simple Form = iota
	Alt         // a language alternative; the value is written as x-default
	Bag
	Seq
)

// Property is a value for Edit. Empty Values removes the property.
// Qualifiers apply to a Simple value (§6.4). A non-nil map, even an empty one,
// writes the value in the rdf:parseType="Resource" form, with the value in
// rdf:value, which readers of a qualified property look for.
type Property struct {
	Name       xml.Name
	Form       Form
	Values     []string
	Qualifiers map[xml.Name]string
}

// Item is one value of a property, with its qualifiers: a simple value's, or
// one array item's (§6.4, §7.8).
type Item struct {
	Value      string
	Qualifiers map[xml.Name]string
}

func rdf(local string) xml.Name { return xml.Name{Space: NSRDF, Local: local} }

// Items returns a property's items, and false if no rdf:Description holds it
// or p is nil. An array yields its items in order, except that a language
// alternative's x-default comes first. A simple value yields one item.
func (p *Packet) Items(name xml.Name) ([]Item, bool) {
	if p == nil {
		return nil, false
	}

	for _, d := range p.descriptions() {
		if v, ok := d.attr(name); ok {
			return []Item{{Value: v}}, true
		}

		if prop := d.child(name); prop != nil {
			return prop.items(), true
		}
	}

	return nil, false
}

// Values is Items without the qualifiers.
func (p *Packet) Values(name xml.Name) ([]string, bool) {
	items, ok := p.Items(name)

	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Value
	}

	return out, ok
}

// Edit returns the packet with each property in props replaced. Every
// existing copy is removed, and the new values are written in one new
// rdf:Description at the end of rdf:RDF. A nil Packet edits an empty one, so
// Edit also creates packets.
func (p *Packet) Edit(props []Property) []byte {
	if p == nil {
		p = empty()
	}

	var edits []splice

	for _, d := range p.descriptions() {
		edits = append(edits, d.removeProperties(props)...)
	}

	if inserted := p.description(props); inserted != "" {
		edits = append(edits, p.rdf.appendChild(inserted))
	}

	return apply(p.raw, edits)
}

func empty() *Packet {
	p, err := Parse([]byte(skeleton))
	if err != nil {
		panic(fmt.Sprintf("xmp: skeleton packet does not parse: %v", err))
	}

	return p
}

const skeleton = "<?xpacket begin=\"\uFEFF\" id=\"W5M0MpCehiHzreSzNTczkc9d\"?>\n" +
	"<x:xmpmeta xmlns:x=\"adobe:ns:meta/\">\n" +
	"<rdf:RDF xmlns:rdf=\"" + NSRDF + "\">\n" +
	"</rdf:RDF>\n" +
	"</x:xmpmeta>\n" +
	"<?xpacket end=\"w\"?>"

func (p *Packet) descriptions() []*node {
	var out []*node

	for _, c := range p.rdf.children {
		if c.name == rdf("Description") {
			out = append(out, c)
		}
	}

	return out
}

// description writes the properties that have values as one rdf:Description,
// whose rdf:about matches the existing descriptions', as §7.4 requires.
func (p *Packet) description(props []Property) string {
	var body strings.Builder

	for _, prop := range props {
		if len(prop.Values) > 0 {
			writeProperty(&body, prop)
		}
	}

	if body.Len() == 0 {
		return ""
	}

	about := ""
	if ds := p.descriptions(); len(ds) > 0 {
		about, _ = ds[0].attr(rdf("about"))
	}

	return `<rdf:Description rdf:about="` + escape(about) + `"` + declarations + ">\n" + body.String() + "</rdf:Description>\n"
}

func writeProperty(b *strings.Builder, prop Property) {
	var li strings.Builder
	for _, v := range prop.Values {
		li.WriteString("<rdf:li>" + escape(v) + "</rdf:li>")
	}

	items := li.String()

	value := map[Form]string{
		Simple: escape(prop.Values[0]),
		Alt:    `<rdf:Alt><rdf:li xml:lang="x-default">` + escape(prop.Values[0]) + "</rdf:li></rdf:Alt>",
		Bag:    "<rdf:Bag>" + items + "</rdf:Bag>",
		Seq:    "<rdf:Seq>" + items + "</rdf:Seq>",
	}[prop.Form]

	name := prefixed(prop.Name)

	start := "<" + name + ">"
	if prop.Form == Simple && prop.Qualifiers != nil {
		start = "<" + name + ` rdf:parseType="Resource"><rdf:value>`
		value = escape(prop.Values[0]) + "</rdf:value>" + qualifierElements(prop.Qualifiers)
	}

	b.WriteString(start + value + "</" + name + ">\n")
}

// prefixed names n by the prefix declarations binds its namespace to.
func prefixed(n xml.Name) string { return prefixes[n.Space] + ":" + n.Local }

// qualifierElements writes qualifiers in name order, so the same edit always
// writes the same bytes. Without qualifiers it is empty.
func qualifierElements(q map[xml.Name]string) string {
	var b strings.Builder

	for _, n := range slices.SortedFunc(maps.Keys(q), func(a, c xml.Name) int {
		return cmp.Or(strings.Compare(a.Space, c.Space), strings.Compare(a.Local, c.Local))
	}) {
		b.WriteString("<" + prefixed(n) + ">" + escape(q[n]) + "</" + prefixed(n) + ">")
	}

	return b.String()
}

// Representable reports whether XML 1.0 can hold s (XML 1.0 §2.2, Char).
func Representable(s string) bool {
	return utf8.ValidString(s) && !strings.ContainsFunc(s, func(r rune) bool { return !isChar(r) })
}

func isChar(r rune) bool {
	return r == '\t' || r == '\n' || r == '\r' ||
		r >= 0x20 && r <= 0xD7FF || r >= 0xE000 && r <= 0xFFFD || r >= 0x10000 && r <= 0x10FFFF
}

func escape(s string) string {
	var b bytes.Buffer

	_ = xml.EscapeText(&b, []byte(s))

	return b.String()
}
