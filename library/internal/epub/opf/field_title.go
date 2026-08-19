package opf

type titleField struct{ o *Doc }

func (o *Doc) title() titleField { return titleField{o} }

// get returns the title and its sort value, which is the EPUB 3 file-as refine
// on the title element. EPUB 2 has no standard mechanism for it.
func (f titleField) get() (title, sort string) {
	el := f.o.target("title")
	if el == nil {
		return "", ""
	}
	return text(el), f.o.refine(attr(el, "id"), propFileAs)
}

func (f titleField) set(title, sort *string) {
	if title != nil {
		f.o.setDCText("title", *title)
	}
	if sort == nil && title == nil {
		return
	}

	value := ""
	if sort != nil {
		value = collapse(*sort)
	}

	// A v2 package gets nothing: no standard mechanism and, unlike series, no
	// proprietary fallback either.
	el := f.o.target("title")
	if !f.o.epub3() || el == nil {
		return
	}
	if value == "" {
		f.o.removeRefine(attr(el, "id"), propFileAs)
		return
	}
	f.o.setRefine(f.o.ensureID(el, "ebookfs-title"), propFileAs, value, "")
}
