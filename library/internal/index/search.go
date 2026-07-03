package index

import "github.com/ramblingenzyme/ebookfs/library/model"

// Search parses query and returns matching books.
//
// Supported prefixes: title:, author:, tag:, series:, status:, id:.
// Compound queries join terms with +, e.g. "tag:sci-fi+status:unread".
func (idx *Index) Search(query string) ([]*model.Book, error) {
	panic("not yet implemented")
}
