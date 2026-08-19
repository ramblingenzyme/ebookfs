package opf

type titleField struct{ o *Doc }

func (o *Doc) title() titleField { return titleField{o} }

func (f titleField) element() *elementSlot { return f.o.dcElement("title", "ebookfs-title") }

// get returns the title and its sort value, which is the EPUB 3 file-as refine
// on the title element. EPUB 2 has no standard mechanism for it.
func (f titleField) get() (title, sort string) {
	el := f.element()
	return el.get(), el.refine("file-as").get()
}

func (f titleField) set(title, sort *string) {
	el := f.element()
	if title != nil {
		el.set(*title)
	}

	// A v2 package gets no sort title: no standard mechanism and, unlike series,
	// no proprietary fallback either. Neither does a file with no title element
	// to refine — a sort title alone is not reason enough to invent one.
	if !f.o.epub3() || !el.exists() || (title == nil && sort == nil) {
		return
	}

	// A title written without one drops the sort title it used to carry.
	value := ""
	if sort != nil {
		value = collapse(*sort)
	}
	put(el.refine("file-as"), value)
}
