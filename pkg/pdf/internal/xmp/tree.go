package xmp

import (
	"bytes"
	"cmp"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
)

// node is an element and where its bytes lie in the packet.
type node struct {
	name     xml.Name // resolved, its Space a namespace URI
	qname    string   // as written
	attrs    []attr
	children []*node
	text     strings.Builder
	scope    map[string]string // prefixes in scope here

	start, tagEnd int // the start tag
	endTag, end   int // the end tag; both tagEnd for a self-closing element
}

type attr struct {
	name         xml.Name
	qname, value string
}

// parseTree reads raw tokens, which keep prefixes as written, and resolves
// namespaces itself, since encoding/xml's resolved tokens lose the prefixes a
// rewritten start tag must repeat.
func parseTree(data []byte) (*node, error) {
	body := bytes.TrimPrefix(data, utf8BOM)
	skip := len(data) - len(body)
	d := xml.NewDecoder(bytes.NewReader(body))
	stack := []*node{{scope: map[string]string{"xml": NSXML}}}

	for {
		before := skip + int(d.InputOffset())

		tok, err := d.RawToken()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return nil, fmt.Errorf("xmp: %w", err)
		}

		if stack, err = push(stack, tok, data, before, skip+int(d.InputOffset())); err != nil {
			return nil, err
		}
	}

	if len(stack) != 1 {
		return nil, errors.New("xmp: unclosed element")
	}

	return stack[0], nil
}

// push adds tok, which lies at data[before:after], to the tree whose open
// elements are stack, and returns the stack after it.
func push(stack []*node, tok xml.Token, data []byte, before, after int) ([]*node, error) {
	top := stack[len(stack)-1]

	switch t := tok.(type) {
	case xml.StartElement:
		n := newNode(t, top.scope, before, after)

		// encoding/xml synthesizes the end tag of <x/> at no offset of its own.
		if bytes.HasSuffix(data[before:after], []byte("/>")) {
			n.endTag, n.end = after, after
		}

		top.children = append(top.children, n)

		return append(stack, n), nil
	case xml.EndElement:
		if len(stack) == 1 {
			return nil, errors.New("xmp: unbalanced end tag")
		}

		if top.endTag == 0 {
			top.endTag, top.end = before, after
		}

		return stack[:len(stack)-1], nil
	case xml.CharData:
		top.text.Write(t)
	}

	return stack, nil
}

func pushScope(parent map[string]string, attrs []xml.Attr) map[string]string {
	if len(attrs) == 0 {
		return parent
	}

	scope := maps.Clone(parent)

	for _, a := range attrs {
		switch {
		case a.Name.Space == "xmlns":
			scope[a.Name.Local] = a.Value
		case a.Name.Space == "" && a.Name.Local == "xmlns":
			scope[""] = a.Value
		}
	}

	return scope
}

func newNode(t xml.StartElement, parent map[string]string, start, tagEnd int) *node {
	scope := pushScope(parent, t.Attr)
	n := &node{
		scope:  scope,
		name:   xml.Name{Space: scope[t.Name.Space], Local: t.Name.Local},
		qname:  qname(t.Name),
		start:  start,
		tagEnd: tagEnd,
	}

	for _, a := range t.Attr {
		// An unprefixed attribute is in no namespace, and neither is a
		// namespace declaration, so no lookup matches one.
		space := ""
		if a.Name.Space != "" && a.Name.Space != "xmlns" {
			space = scope[a.Name.Space]
		}

		n.attrs = append(n.attrs, attr{xml.Name{Space: space, Local: a.Name.Local}, qname(a.Name), a.Value})
	}

	return n
}

func (n *node) selfClosing() bool { return n.endTag == n.tagEnd }

func qname(n xml.Name) string {
	if n.Space == "" {
		return n.Local
	}

	return n.Space + ":" + n.Local
}

func (n *node) find(name xml.Name) *node {
	if n.name == name {
		return n
	}

	for _, c := range n.children {
		if f := c.find(name); f != nil {
			return f
		}
	}

	return nil
}

func (n *node) child(name xml.Name) *node {
	for _, c := range n.children {
		if c.name == name {
			return c
		}
	}

	return nil
}

func (n *node) attr(name xml.Name) (string, bool) {
	for _, a := range n.attrs {
		if a.name == name {
			return a.value, true
		}
	}

	return "", false
}

// values reads a property element: an array's items, a URI, or simple text.
func (n *node) values() []string {
	if v, ok := n.attr(rdf("resource")); ok {
		return []string{v}
	}

	for _, kind := range []string{"Alt", "Bag", "Seq"} {
		if arr := n.child(rdf(kind)); arr != nil {
			return arr.items(kind == "Alt")
		}
	}

	return []string{valueOf(n)}
}

func (n *node) items(alt bool) []string {
	var out []string

	for _, li := range n.children {
		if li.name != rdf("li") {
			continue
		}

		if lang, _ := li.attr(xml.Name{Space: NSXML, Local: "lang"}); alt && lang == "x-default" {
			out = slices.Insert(out, 0, valueOf(li))
		} else {
			out = append(out, valueOf(li))
		}
	}

	return out
}

// valueOf reads simple text, or the rdf:value of a qualified one (§7.8).
func valueOf(n *node) string {
	if v := n.child(rdf("value")); v != nil {
		return v.text.String()
	}

	if d := n.child(rdf("Description")); d != nil {
		if v, ok := d.attr(rdf("value")); ok {
			return v
		}

		return valueOf(d)
	}

	return n.text.String()
}

// splice replaces raw[from:to] with text.
type splice struct {
	from, to int
	text     string
}

func (n *node) removeProperties(props []Property) []splice {
	var out []splice

	named := func(name xml.Name) bool {
		return slices.ContainsFunc(props, func(p Property) bool { return p.Name == name })
	}

	if slices.ContainsFunc(n.attrs, func(a attr) bool { return named(a.name) }) {
		out = append(out, splice{n.start, n.tagEnd, n.startTag(named)})
	}

	for _, c := range n.children {
		if named(c.name) {
			out = append(out, splice{c.start, c.end, ""})
		}
	}

	return out
}

// startTag rewrites the start tag without the attributes drop names.
func (n *node) startTag(drop func(xml.Name) bool) string {
	var b strings.Builder

	b.WriteString("<" + n.qname)

	for _, a := range n.attrs {
		if drop(a.name) {
			continue
		}

		b.WriteString(" " + a.qname + `="` + escape(a.value) + `"`)
	}

	if n.selfClosing() {
		b.WriteString("/>")
	} else {
		b.WriteString(">")
	}

	return b.String()
}

// appendChild inserts text as n's last child.
func (n *node) appendChild(text string) splice {
	if !n.selfClosing() {
		return splice{n.endTag, n.endTag, text}
	}

	open := strings.TrimSuffix(n.startTag(func(xml.Name) bool { return false }), "/>") + ">"

	return splice{n.start, n.tagEnd, open + text + "</" + n.qname + ">"}
}

func apply(raw []byte, edits []splice) []byte {
	slices.SortFunc(edits, func(a, b splice) int { return cmp.Compare(a.from, b.from) })

	var out []byte

	at := 0

	for _, e := range edits {
		out = append(out, raw[at:e.from]...)
		out = append(out, e.text...)
		at = e.to
	}

	return append(out, raw[at:]...)
}
