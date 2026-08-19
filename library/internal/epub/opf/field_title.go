package opf

type titleField struct{ o *Doc }

func (o *Doc) title() titleField { return titleField{o} }

// get returns the title and its sort value. The sort title is the EPUB 3
// file-as refine on the title element; EPUB 2 has no standard mechanism, so it
// is simply absent there.
func (f titleField) get() (title, sort string) {
	el := f.o.target("title")
	if el == nil {
		return "", ""
	}
	return text(el), f.o.refine(attr(el, "id"), propFileAs)
}

// set writes the title and its sort value, either of which is nil when the edit
// did not name it. Changing the title without supplying a sort title clears the
// old one, which was derived from the title that just went.
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

	// Since the sort title is not exposed for editing in the frontend we do not
	// invent a proprietary EPUB 2 fallback the way the series field does for
	// calibre:series — a v2 package simply gets nothing.
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
