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
	// qual, when set, makes the field a qualified XMP value: its second item
	// is the qualifier's.
	qual xml.Name
	accessor
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
	{name: "title", dict: infoDict, key: "Title", prop: dc("title"), form: xmp.Alt, accessor: text(func(m *Metadata) *string { return &m.Title })},
	{name: "author", dict: infoDict, key: "Author", prop: dc("creator"), form: xmp.Seq, accessor: list(func(m *Metadata) *[]string { return &m.Authors })},
	{name: "description", dict: infoDict, key: "Subject", prop: dc("description"), form: xmp.Alt, accessor: text(func(m *Metadata) *string { return &m.Description })},
	{name: "subject", prop: dc("subject"), form: xmp.Bag, accessor: list(func(m *Metadata) *[]string { return &m.Subjects })},
	{name: "keywords", dict: infoDict, key: "Keywords", prop: pdfNS("Keywords"), form: xmp.Simple, accessor: text(func(m *Metadata) *string { return &m.Keywords })},
	{name: "language", dict: catalogDict, key: "Lang", prop: dc("language"), form: xmp.Bag, accessor: text(func(m *Metadata) *string { return &m.Language })},
	{name: "publisher", prop: dc("publisher"), form: xmp.Bag, accessor: text(func(m *Metadata) *string { return &m.Publisher })},
	{name: "rights", prop: dc("rights"), form: xmp.Alt, accessor: text(func(m *Metadata) *string { return &m.Rights })},
	{name: "contributor", prop: dc("contributor"), form: xmp.Bag, accessor: list(func(m *Metadata) *[]string { return &m.Contributors })},
	{name: "sort title", prop: calibre("title_sort"), form: xmp.Simple, accessor: text(func(m *Metadata) *string { return &m.SortTitle })},
	{name: "author sort", prop: calibre("author_sort"), form: xmp.Simple, accessor: text(func(m *Metadata) *string { return &m.AuthorSort })},
	{name: "series", prop: calibre("series"), form: xmp.Simple, qual: xml.Name{Space: xmp.NSCalibreSI, Local: "series_index"}, accessor: series()},
}

func dc(local string) xml.Name      { return xml.Name{Space: xmp.NSDC, Local: local} }
func pdfNS(local string) xml.Name   { return xml.Name{Space: xmp.NSPDF, Local: local} }
func xmpNS(local string) xml.Name   { return xml.Name{Space: xmp.NSXMP, Local: local} }
func calibre(local string) xml.Name { return xml.Name{Space: xmp.NSCalibre, Local: local} }

// creationDate is read like a field but never written, since §14.3.4 says an
// edit leaves the creation date as it is.
var creationDate = field{dict: infoDict, key: "CreationDate", prop: xmpNS("CreateDate"), accessor: accessor{single: true}}

// accessor reads and writes one Metadata field as a list of strings, the shape
// every store compares and writes.
type accessor struct {
	get func(*Metadata) []string
	set func(*Metadata, []string)
	// single fields hold one value. A write then leaves an array's later items
	// alone while its first agrees.
	single bool
}

// text is a string field: empty is no value.
func text(p func(*Metadata) *string) accessor {
	return accessor{
		get: func(m *Metadata) []string {
			if s := *p(m); s != "" {
				return []string{s}
			}

			return nil
		},
		set: func(m *Metadata, v []string) {
			*p(m) = ""
			if len(v) > 0 {
				*p(m) = v[0]
			}
		},
		single: true,
	}
}

func list(p func(*Metadata) *[]string) accessor {
	return accessor{
		get: func(m *Metadata) []string { return *p(m) },
		set: func(m *Metadata, v []string) { *p(m) = v },
	}
}

// series is Metadata.Series as its name and index.
func series() accessor {
	return accessor{
		get: func(m *Metadata) []string {
			if m.Series == nil {
				return nil
			}

			return []string{m.Series.Name, m.Series.Index}
		},
		set: func(m *Metadata, v []string) {
			m.Series = nil
			if len(v) == 2 {
				m.Series = &Series{Name: v[0], Index: v[1]}
			}
		},
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
