package check

import (
	"bytes"
	"embed"
	"io/fs"
	"sync"

	"github.com/abdulwaheedsyed/leafbind/internal/rng"
)

// The EPUB 3 schemas, as EPUBCheck 5.4.0 ships them: the package document,
// and XHTML content and navigation documents, which are EPUB's own modules
// over the validator.nu HTML schema, with MathML, SVG and ITS. The files are
// verbatim, each under the license in its directory.
//
//go:embed schema
var schemaFiles embed.FS

func loadSchema(name string) func() (*rng.Schema, error) {
	return sync.OnceValues(func() (*rng.Schema, error) {
		sub, err := fs.Sub(schemaFiles, "schema/30")
		if err != nil {
			return nil, err
		}
		return rng.Load(sub, name)
	})
}

var (
	xhtmlSchema   = loadSchema("epub-xhtml-30.rnc")
	navSchema     = loadSchema("epub-nav-30.rnc")
	packageSchema = loadSchema("package-30.rnc")
)

// validateSchema validates a document against a schema and reports what
// it finds as RSC-005, as EPUBCheck does.
func (c *checker) validateSchema(schema func() (*rng.Schema, error), path string, data []byte) {
	s, err := schema()
	if err != nil {
		return // cannot happen with the embedded schemas; the tests load them
	}
	errs, perr := s.Validate(bytes.NewReader(data))
	if perr != nil {
		return // not well-formed: already reported as RSC-016
	}
	for _, e := range errs {
		c.report("RSC-005", path, e.Line, e.Column, e.Message)
	}
}

// checkSchema validates a content document, against the navigation
// document's schema when it is one.
func (c *checker) checkSchema(it *item, data []byte) {
	if slicesContains(it.props, "nav") {
		c.validateSchema(navSchema, it.path, data)
		return
	}
	c.validateSchema(xhtmlSchema, it.path, data)
}
