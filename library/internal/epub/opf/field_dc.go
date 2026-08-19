package opf

// description and language are repeatable (§5.5.3.2.1) but single-valued to us,
// and have no encoding of their own, so they get no field type: dcElement picks
// the element a read and a write both mean.
func (o *Doc) description() string { return o.dcElement("description", "").get() }

func (o *Doc) language() string { return o.dcElement("language", "").get() }
