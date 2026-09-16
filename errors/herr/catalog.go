package wlogherr

import (
	"github.com/jeremygprawira/herr"
	"github.com/jeremygprawira/wlog/catalog"
)

// Catalog converts herr classes into catalog entries. A project then declares a code
// once and gets both libraries' behavior: herr renders it, and the catalog fills the
// static fields on the event. The entry keeps the class's code, its kind name, its
// HTTP status, and its default public message.
//
// The bridge lives here, never in catalog, so the root module stays free of herr.
func Catalog(classes ...*herr.Class) []catalog.Entry {
	entries := make([]catalog.Entry, 0, len(classes))
	for _, class := range classes {
		if class == nil {
			continue
		}
		entries = append(entries, catalog.Entry{
			Code:    class.Code,
			Kind:    kindNames[class.Kind],
			Status:  class.New().HTTPStatus(),
			Message: class.Public.Message,
		})
	}
	return entries
}
