package pdf

import (
	"encoding/xml"
	"errors"
	"fmt"
	"slices"

	"github.com/ramblingenzyme/ebookfs/pkg/pdf/internal/cos"
	"github.com/ramblingenzyme/ebookfs/pkg/pdf/internal/xmp"
)

// field maps one Metadata field to the places a PDF keeps it. Reading,
// change detection and writing all walk the same table, so a field added here
// is handled everywhere.
type field struct {
	name string
	// dict is the dictionary holding key, if any besides XMP does.
	dict dictKind
	key  cos.Name
	prop xml.Name
	form xmp.Form
	// One of text and items points at the field.
	text  func(*Metadata) *string
	items func(*Metadata) *[]string
}

type dictKind int

const (
	noDict dictKind = iota
	infoDict
	catalogDict
)

// authorSeparator joins several authors into the information dictionary's one
// Author string, as Acrobat writes it.
const authorSeparator = "; "

var fields = []field{
	{name: "title", dict: infoDict, key: "Title", prop: dc("title"), form: xmp.Alt, text: func(m *Metadata) *string { return &m.Title }},
	{name: "author", dict: infoDict, key: "Author", prop: dc("creator"), form: xmp.Seq, items: func(m *Metadata) *[]string { return &m.Authors }},
	{name: "description", dict: infoDict, key: "Subject", prop: dc("description"), form: xmp.Alt, text: func(m *Metadata) *string { return &m.Description }},
	{name: "subject", prop: dc("subject"), form: xmp.Bag, items: func(m *Metadata) *[]string { return &m.Subjects }},
	{name: "keywords", dict: infoDict, key: "Keywords", prop: pdfNS("Keywords"), form: xmp.Simple, text: func(m *Metadata) *string { return &m.Keywords }},
	{name: "language", dict: catalogDict, key: "Lang", prop: dc("language"), form: xmp.Bag, text: func(m *Metadata) *string { return &m.Language }},
	{name: "publisher", prop: dc("publisher"), form: xmp.Bag, text: func(m *Metadata) *string { return &m.Publisher }},
	{name: "rights", prop: dc("rights"), form: xmp.Alt, text: func(m *Metadata) *string { return &m.Rights }},
	{name: "contributor", prop: dc("contributor"), form: xmp.Bag, items: func(m *Metadata) *[]string { return &m.Contributors }},
}

func dc(local string) xml.Name    { return xml.Name{Space: xmp.NSDC, Local: local} }
func pdfNS(local string) xml.Name { return xml.Name{Space: xmp.NSPDF, Local: local} }
func xmpNS(local string) xml.Name { return xml.Name{Space: xmp.NSXMP, Local: local} }

// creationDate is read like a field but never written, since §14.3.4 says an
// edit leaves the creation date as it is.
var creationDate = field{dict: infoDict, key: "CreationDate", prop: xmpNS("CreateDate")}

// single reports a field that holds one value. A write then leaves an array's
// later items alone while its first agrees.
func (f field) single() bool { return f.items == nil }

func (f field) get(m *Metadata) []string {
	if f.items != nil {
		return *f.items(m)
	}

	if s := *f.text(m); s != "" {
		return []string{s}
	}

	return nil
}

func (f field) set(m *Metadata, v []string) {
	switch {
	case f.items != nil:
		*f.items(m) = v
	case len(v) > 0:
		*f.text(m) = v[0]
	default:
		*f.text(m) = ""
	}
}

// build fills each field of a Metadata with value's result for it.
func build(value func(field) []string) Metadata {
	var m Metadata
	for _, f := range fields {
		f.set(&m, value(f))
	}

	return m
}

func take(m Metadata) Metadata {
	return build(func(f field) []string { return slices.Clone(f.get(&m)) })
}

func changed(a, b Metadata) bool {
	return slices.ContainsFunc(fields, func(f field) bool { return !slices.Equal(f.get(&a), f.get(&b)) })
}

// sources is what Open read from each store.
type sources struct {
	catalog, info dictStore
	packet        xmpStore
	packetErr     error
	preferInfo    bool
}

func readSources(f *File) (sources, error) {
	s := sources{catalog: dictStore{kind: catalogDict}, info: dictStore{kind: infoDict}}

	if _, ok := f.x.Trailer["Encrypt"]; ok {
		return s, ErrEncrypted
	}

	var err error
	if s.catalog.d, err = resolveDict(f, "Root"); err != nil {
		return s, err
	}

	if s.catalog.d == nil {
		return s, fmt.Errorf("%w: catalog is not a dictionary", cos.ErrMalformed)
	}

	if s.info.d, err = resolveDict(f, "Info"); err != nil {
		return s, err
	}

	s.packet.p, err = readPacket(f, s.catalog.d)

	switch {
	case errors.Is(err, errBadPacket):
		s.packetErr = err
	case err != nil:
		return s, err
	}

	s.preferInfo = infoIsNewer(s.info.d, s.packet.p)

	return s, nil
}

// resolveDict returns the dictionary the trailer's key refers to, or nil if it
// refers to anything else.
func resolveDict(f *File, key cos.Name) (cos.Dict, error) {
	o, err := f.x.Resolve(f.x.Trailer[key])
	d, _ := o.(cos.Dict)

	return d, err
}

// errBadPacket marks an XMP packet that cannot be decoded or parsed. It does
// not stop the rest of the metadata being read; only a failed read fails Open.
var errBadPacket = errors.New("unreadable XMP metadata stream")

// readPacket returns the catalog's XMP packet, or nil if there is none.
func readPacket(f *File, catalog cos.Dict) (*xmp.Packet, error) {
	o, err := f.x.Resolve(catalog["Metadata"])
	if err != nil {
		return nil, badPacket(err)
	}

	s, ok := o.(*cos.Stream)
	if !ok {
		return nil, nil
	}

	data, err := f.x.Data(s)
	if err != nil {
		return nil, badPacket(err)
	}

	p, err := xmp.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errBadPacket, err)
	}

	return p, nil
}

func badPacket(err error) error {
	if errors.Is(err, cos.ErrMalformed) || errors.Is(err, cos.ErrUnsupportedFilter) {
		return fmt.Errorf("%w: %w", errBadPacket, err)
	}

	return err
}

// read takes f from the first store, in the order the package doc says a
// reader should trust them, that holds a value for it.
func (s *sources) read(f field) []string {
	trust := []store{s.packet, s.info, s.catalog}
	if s.preferInfo {
		trust = []store{s.info, s.packet, s.catalog}
	}

	for _, st := range trust {
		if v, ok := st.value(f); ok {
			return v
		}
	}

	return nil
}
