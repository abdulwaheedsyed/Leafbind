// Copyright 2026 Syed Abdul Waheed
// SPDX-License-Identifier: Apache-2.0

package check

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// The rules of EPUBCheck's package-30.sch, for EPUB 3 package documents:
// references between metadata, rendition properties and overrides, media
// overlays and deprecated forms. Errors are RSC-005 and warnings RSC-017.

const opfNS = "http://www.idpf.org/2007/opf"

var (
	uriScheme     = regexp.MustCompile(`[a-zA-Z]([a-zA-Z0-9]|\+|-|\.)*:`)
	viewportValue = regexp.MustCompile(`^((width=\d+,\s*height=\d+)|(height=\d+,\s*width=\d+))$`)
	smilClock     = regexp.MustCompile(`^(([0-9]+:[0-5][0-9]:[0-5][0-9](\.[0-9]+)?)|((\s*)[0-5][0-9]:[0-5][0-9](\.[0-9]+)?(\s*))|((\s*)[0-9]+(\.[0-9]+)?(h|min|s|ms)?(\s*)))$`)

	renditionRules = map[string]struct {
		values  []string
		message string
	}{
		"rendition:flow":        {[]string{"paginated", "scrolled-continuous", "scrolled-doc", "auto"}, `must be either "paginated", "scrolled-continuous", "scrolled-doc", or "auto"`},
		"rendition:layout":      {[]string{"reflowable", "pre-paginated", "roll"}, `must be either "reflowable", "pre-paginated", or "roll"`},
		"rendition:orientation": {[]string{"landscape", "portrait", "auto"}, `must be either "landscape", "portrait" or "auto"`},
		"rendition:spread":      {[]string{"none", "landscape", "portrait", "both", "auto"}, `must be either "none", "landscape", "portrait", "both" or "auto"`},
	}

	// Spine properties of which an itemref may carry at most one.
	exclusiveOverrides = []struct {
		props   []string
		message string
	}{
		{[]string{"rendition:flow-paginated", "rendition:flow-scrolled-continuous", "rendition:flow-scrolled-doc", "rendition:flow-auto"},
			`Properties "rendition:flow-paginated", "rendition:flow-scrolled-continuous", "rendition:flow-scrolled-doc" and "rendition:flow-auto" are mutually exclusive`},
		{[]string{"rendition:layout-reflowable", "rendition:layout-pre-paginated"},
			`Properties "rendition:layout-reflowable" and "rendition:layout-pre-paginated" are mutually exclusive`},
		{[]string{"rendition:orientation-landscape", "rendition:orientation-portrait", "rendition:orientation-auto"},
			`Properties "rendition:orientation-landscape", "rendition:orientation-portrait" and "rendition:orientation-auto" are mutually exclusive`},
		{[]string{"rendition:spread-portrait", "rendition:spread-landscape", "rendition:spread-both", "rendition:spread-none", "rendition:spread-auto"},
			`Properties "rendition:spread-portrait", "rendition:spread-landscape", "rendition:spread-both", "rendition:spread-none" and "rendition:spread-auto" are mutually exclusive`},
	}
)

// checkPackageSchematron applies package-30.sch to the package element.
func (c *checker) checkPackageSchematron(pkg *node) {
	errorf := func(n *node, format string, args ...any) {
		c.report("RSC-005", c.opf, n.line, n.col, fmt.Sprintf(format, args...))
	}
	warnf := func(n *node, format string, args ...any) {
		c.report("RSC-017", c.opf, n.line, n.col, fmt.Sprintf(format, args...))
	}
	opf := func(n *node, local string) bool { return n.name.Space == opfNS && n.name.Local == local }
	dc := func(n *node, local string) bool { return n.name.Space == dcNS && n.name.Local == local }
	ns := normalizeSpace
	prop := func(n *node) string { return ns(n.attr("property")) }

	// Every element's id, and the manifest.
	ids := map[string]bool{}
	pkg.walk(func(n *node) {
		if id, ok := n.plainAttr("id"); ok {
			ids[ns(id)] = true
		}
	})
	var metadata, manifest, spine *node
	for _, k := range pkg.kids {
		switch {
		case opf(k, "metadata"):
			metadata = k
		case opf(k, "manifest"):
			manifest = k
		case opf(k, "spine"):
			spine = k
		}
	}
	var items []*node
	itemByID := map[string]*node{}
	if manifest != nil {
		for _, it := range manifest.kids {
			if opf(it, "item") {
				items = append(items, it)
				itemByID[ns(it.attr("id"))] = it
			}
		}
	}
	metas := func() []*node {
		var out []*node
		if metadata != nil {
			for _, k := range metadata.kids {
				if opf(k, "meta") {
					out = append(out, k)
				}
			}
		}
		return out
	}()

	// The unique identifier.
	if uid, ok := pkg.plainAttr("unique-identifier"); ok && metadata != nil {
		uid = ns(uid)
		if !slices.ContainsFunc(metadata.kids, func(k *node) bool { return dc(k, "identifier") && ns(k.attr("id")) == uid }) {
			errorf(pkg, `package element unique-identifier attribute does not resolve to a dc:identifier element (given reference was "%s")`, uid)
		}
	}

	// Package-level metadata: counts of global properties.
	if metadata != nil {
		count := func(p string, global bool) int {
			n := 0
			for _, m := range metas {
				_, refines := m.plainAttr("refines")
				if prop(m) == p && (!global || !refines) {
					n++
				}
			}
			return n
		}
		if count("dcterms:modified", true) != 1 {
			errorf(metadata, "package dcterms:modified meta element must occur exactly once")
		}
		for _, p := range []string{"rendition:flow", "rendition:layout", "rendition:orientation", "rendition:spread"} {
			if count(p, false) > 1 {
				errorf(metadata, `The "%s" property must not occur more than one time in the package metadata.`, p)
			}
		}
		if count("rendition:viewport", true) > 1 {
			errorf(metadata, `The "rendition:viewport" property must not occur more than one time as a global value in the package metadata.`)
		}
		for _, p := range []string{"media:active-class", "media:playback-active-class"} {
			if count(p, false) > 1 {
				errorf(metadata, `The '%s' property must not occur more than one time in the package metadata.`, strings.TrimPrefix(p, "media:"))
			}
		}
	}

	// Rules on elements anywhere in the package, outside collections.
	var visit func(n *node, inCollection bool, parent *node, prev []*node)
	visit = func(n *node, inCollection bool, parent *node, prev []*node) {
		refines, hasRefines := n.plainAttr("refines")
		refines = ns(refines)
		if hasRefines && !inCollection {
			if uriScheme.MatchString(n.attr("refines")) {
				errorf(n, "@refines must be a relative URL")
			}
			if !strings.HasPrefix(refines, "#") {
				if target, ok := resolve(c.opf, refines); ok {
					for _, it := range items {
						if p, ok := resolve(c.opf, ns(it.attr("href"))); ok && p == target {
							warnf(n, `@refines should instead refer to "%s" using a fragment identifier pointing to its manifest item ("#%s")`, n.attr("refines"), it.attr("id"))
						}
					}
				}
			} else if !ids[refines[1:]] {
				errorf(n, `@refines missing target id: "%s"`, refines[1:])
			}
		}

		if opf(n, "link") && !inCollection {
			rel := strings.Fields(n.attr("rel"))
			if slices.Contains(rel, "record") && hasRefines {
				errorf(n, `"record" links only applies to the Publication (must not have a "refines" attribute).`)
			}
			if slices.Contains(rel, "voicing") && !hasRefines {
				errorf(n, `"voicing" links must have a "refines" attribute.`)
			}
		}
		if opf(n, "meta") && parent != nil {
			c.checkMetaProperty(n, parent, prev, inCollection, refines, hasRefines, errorf, warnf)
		}
		if dc(n, "subject") && parent != nil && opf(parent, "metadata") {
			id := ns(n.attr("id"))
			var authority, term int
			pkg.walk(func(m *node) {
				if opf(m, "meta") && xpathTail(ns(m.attr("refines"))) == id {
					switch prop(m) {
					case "authority":
						authority++
					case "term":
						term++
					}
				}
			})
			switch {
			case authority == 1 && term == 0:
				errorf(n, "A term property must be associated with a dc:subject when an authority is specified")
			case authority == 0 && term == 1:
				errorf(n, "An authority property must be associated with a dc:subject when a term is specified")
			case authority > 1 || term > 1:
				errorf(n, "Only one pair of authority and term properties can be associated with a dc:subject")
			}
		}

		// Nested collections are checked with their top-level one.
		if opf(n, "collection") && !inCollection {
			c.checkCollectionRefines(n, errorf)
		}

		if opf(n, "reference") {
			typ, href := ns(strings.ToLower(n.attr("type"))), ns(strings.ToLower(n.attr("href")))
			same := 0
			pkg.walk(func(r *node) {
				if opf(r, "reference") && ns(strings.ToLower(r.attr("type"))) == typ && ns(strings.ToLower(r.attr("href"))) == href {
					same++
				}
			})
			if same > 1 {
				warnf(n, `Duplicate "reference" elements with the same "type" and "href" attributes`)
			}
		}

		if opf(n, "bindings") && parent == pkg {
			warnf(n, "Use of the bindings element is deprecated")
		}

		var seen []*node
		for _, k := range n.kids {
			visit(k, inCollection || opf(n, "collection"), n, seen)
			seen = append(seen, k)
		}
	}
	visit(pkg, false, nil, nil)

	// The manifest.
	if manifest != nil {
		navs, dataNavs, covers := 0, 0, 0
		for _, it := range items {
			props := strings.Fields(it.attr("properties"))
			if slices.Contains(props, "nav") {
				navs++
				if it.attr("media-type") != "application/xhtml+xml" {
					errorf(it, `The manifest item representing the Navigation Document must be of the "application/xhtml+xml" type (given type was "%s")`, it.attr("media-type"))
				}
			}
			if slices.Contains(props, "data-nav") {
				dataNavs++
			}
			if slices.Contains(props, "cover-image") {
				covers++
			}
			if ns(it.attr("media-type")) == "application/x-dtbncx+xml" && !pkg.hasDescendant(func(s *node) bool { return opf(s, "spine") && s.has("toc") }) {
				errorf(it, "spine element toc attribute must be set when an NCX is included in the publication")
			}
			if mo, ok := it.plainAttr("media-overlay"); ok {
				target := itemByID[ns(mo)]
				mt := ""
				if target != nil {
					mt = ns(target.attr("media-type"))
				}
				if mt != "application/smil+xml" {
					errorf(it, `media overlay items must be of the "application/smil+xml" type (given type was "%s")`, mt)
				}
				if own := ns(it.attr("media-type")); own != "application/xhtml+xml" && own != "image/svg+xml" {
					errorf(it, "The media-overlay attribute is only allowed on XHTML and SVG content documents.")
				}
				uri := "#"
				if target != nil {
					uri = "#" + ns(target.attr("id"))
				}
				if !slices.ContainsFunc(metas, func(m *node) bool { return prop(m) == "media:duration" && ns(m.attr("refines")) == uri }) {
					errorf(it, `item media:duration meta element not set (expecting: meta property='media:duration' refines='%s')`, uri)
				}
			}
		}
		if navs != 1 {
			errorf(manifest, `Exactly one manifest item must declare the "nav" property (number of "nav" items: %d).`, navs)
		}
		if dataNavs > 1 {
			errorf(manifest, `Found %d "data-nav" items. The manifest must not include more than one Data Navigation Document.`, dataNavs)
		}
		if covers > 1 {
			errorf(manifest, `Multiple occurrences of the "cover-image" property (number of "cover-image" items: %d).`, covers)
		}
		if slices.ContainsFunc(items, func(it *node) bool { return it.has("media-overlay") }) &&
			!pkg.hasDescendant(func(m *node) bool { return opf(m, "meta") && prop(m) == "media:duration" && !m.has("refines") }) {
			errorf(manifest, "global media:duration meta element not set")
		}
	}

	// The spine.
	if spine != nil {
		if toc, ok := spine.plainAttr("toc"); ok {
			mt := ""
			if it := itemByID[ns(toc)]; it != nil {
				mt = ns(it.attr("media-type"))
			}
			if mt != "application/x-dtbncx+xml" {
				errorf(spine, `spine element toc attribute must reference the NCX manifest item (referenced media type was "%s")`, mt)
			}
		}
		roll := slices.ContainsFunc(metas, func(m *node) bool { return prop(m) == "rendition:layout" && ns(m.text()) == "roll" })
		var earlier []string
		for _, r := range spine.kids {
			if !opf(r, "itemref") {
				continue
			}
			idref, hasIdref := r.plainAttr("idref")
			if hasIdref && itemByID[ns(idref)] == nil {
				errorf(r, "itemref element idref attribute does not resolve to a manifest item element")
			}
			if slices.Contains(earlier, ns(idref)) {
				errorf(r, "Itemref refers to the same manifest entry as a previous itemref")
			}
			earlier = append(earlier, ns(idref))

			props := strings.Fields(r.attr("properties"))
			for _, ex := range exclusiveOverrides {
				n := 0
				for _, p := range props {
					if slices.Contains(ex.props, p) {
						n++
					}
				}
				if n > 1 {
					errorf(r, "%s", ex.message)
				}
			}
			var layout []string
			for _, p := range props {
				if p == "rendition:layout-reflowable" || p == "rendition:layout-pre-paginated" {
					layout = append(layout, p)
				}
			}
			if len(layout) > 0 && roll {
				errorf(r, `Layout override "%s" must not be used in roll publications`, strings.Join(layout, " "))
			}
			spreads := map[string]bool{}
			for _, p := range props {
				p = strings.TrimPrefix(p, "rendition:")
				if p == "page-spread-right" || p == "page-spread-left" || p == "page-spread-center" {
					spreads[p] = true
				}
			}
			if len(spreads) > 1 {
				errorf(r, `Properties "page-spread-right", "page-spread-left" and "rendition:page-spread-center" are mutually exclusive`)
			}
		}
	}
}

// checkMetaProperty applies the rules for a meta element's property.
func (c *checker) checkMetaProperty(m, parent *node, prev []*node, inCollection bool, refines string, hasRefines bool, errorf, warnf func(*node, string, ...any)) {
	ns := normalizeSpace
	p := ns(m.attr("property"))
	v := ns(m.text())
	sibling := func(match func(*node) bool) bool { return slices.ContainsFunc(parent.kids, match) }
	refinesID := func(k *node) bool { return "#"+ns(k.attr("id")) == refines }
	isDC := func(k *node, names ...string) bool {
		return k.name.Space == dcNS && slices.Contains(names, k.name.Local)
	}
	isMeta := func(k *node, prop string) bool {
		return k.name.Space == opfNS && k.name.Local == "meta" && ns(k.attr("property")) == prop
	}
	targetMeta := func(prop string) bool {
		return sibling(func(k *node) bool {
			return isMeta(k, prop) && ns(k.attr("id")) == xpathTail(refines)
		})
	}
	repeated := func() bool {
		return slices.ContainsFunc(prev, func(k *node) bool { return isMeta(k, p) && ns(k.attr("refines")) == refines })
	}

	switch p {
	case "dcterms:modified":
		if !inCollection && !modifiedRE.MatchString(v) {
			errorf(m, `dcterms:modified illegal syntax (expecting: "CCYY-MM-DDThh:mm:ssZ")`)
		}
	case "authority":
		if !sibling(func(k *node) bool { return isDC(k, "subject") && refinesID(k) }) {
			errorf(m, `Property "authority" must refine a "subject" property.`)
		}
	case "term":
		if !sibling(func(k *node) bool { return isDC(k, "subject") && refinesID(k) }) {
			errorf(m, `Property "term" must refine a "subject" property.`)
		}
	case "belongs-to-collection":
		if hasRefines && !targetMeta("belongs-to-collection") {
			errorf(m, `Property "belongs-to-collection" can only refine other "belongs-to-collection" properties.`)
		}
	case "collection-type":
		if !targetMeta("belongs-to-collection") {
			errorf(m, `Property "collection-type" must refine a "belongs-to-collection" property.`)
		}
		if repeated() {
			errorf(m, `Property "collection-type" cannot be declared more than once to refine the same "belongs-to-collection" expression.`)
		}
	case "display-seq", "file-as", "group-position":
		if repeated() {
			errorf(m, `Property "%s" cannot be declared more than once to refine the same expression.`, p)
		}
	case "identifier-type":
		if !sibling(func(k *node) bool { return isDC(k, "identifier", "source") && refinesID(k) }) {
			errorf(m, `Property "identifier-type" must refine an "identifier" or "source" property.`)
		}
		if repeated() {
			errorf(m, `Property "identifier-type" cannot be declared more than once to refine the same expression.`)
		}
	case "role":
		if !sibling(func(k *node) bool { return isDC(k, "creator", "contributor", "publisher") && refinesID(k) }) {
			errorf(m, `Property "role" must refine a "creator", "contributor", or "publisher" property.`)
		}
	case "source-of":
		if v != "pagination" {
			errorf(m, `The "source-of" property must have the value "pagination"`)
		}
		if !hasRefines || !sibling(func(k *node) bool { return isDC(k, "source") && ns(k.attr("id")) == xpathTail(refines) }) {
			errorf(m, `The "source-of" property must refine a "source" property.`)
		}
		if repeated() {
			errorf(m, `Property "source-of" cannot be declared more than once to refine the same "source" expression.`)
		}
	case "title-type":
		if !sibling(func(k *node) bool { return isDC(k, "title") && refinesID(k) }) {
			errorf(m, `Property "title-type" must refine a "title" property.`)
		}
		if repeated() {
			errorf(m, `Property "title-type" cannot be declared more than once to refine the same "title" expression.`)
		}
	case "media:duration":
		if !smilClock.MatchString(v) {
			errorf(m, "The value of the media:duration property must be a valid SMIL3 clock value")
		}
	case "media:active-class", "media:playback-active-class":
		name := strings.TrimPrefix(p, "media:")
		if hasRefines {
			errorf(m, " @refines must not be used with the %s property", p)
		}
		if strings.Contains(v, " ") {
			errorf(m, "the '%s' property must define a single class name", name)
		}
	case "meta-auth":
		if parent.name.Local == "metadata" {
			warnf(m, "Use of the meta-auth property is deprecated")
		}
	case "rendition:viewport":
		if !inCollection && !viewportValue.MatchString(v) {
			errorf(m, `The value of the "rendition:viewport" property must be of the form "width=x, height=y"`)
		}
	}
	if r, ok := renditionRules[p]; ok && !inCollection {
		if hasRefines {
			errorf(m, `The "%s" property must not be set on elements with a "refines" attribute`, p)
		}
		if !slices.Contains(r.values, v) {
			errorf(m, `The value of the "%s" property %s`, p, r.message)
		}
	}
}

// checkCollectionRefines requires @refines inside a collection's metadata
// to point within that collection.
func (c *checker) checkCollectionRefines(coll *node, errorf func(*node, string, ...any)) {
	ids := map[string]bool{}
	coll.walk(func(n *node) {
		if id, ok := n.plainAttr("id"); ok {
			ids[normalizeSpace(id)] = true
		}
	})
	coll.walk(func(n *node) {
		if n.name.Space != opfNS || n.name.Local != "collection" {
			return
		}
		for _, md := range n.kids {
			if md.name.Space != opfNS || md.name.Local != "metadata" {
				continue
			}
			for _, k := range md.kids {
				r, ok := k.plainAttr("refines")
				if !ok {
					continue
				}
				r = normalizeSpace(r)
				if !strings.HasPrefix(r, "#") || !ids[r[1:]] {
					errorf(k, " @refines must point to an element within the current collection ")
				}
			}
		}
	})
}

// xpathTail is XPath's substring(s, 2): s without its first character.
func xpathTail(s string) string {
	if s == "" {
		return ""
	}
	_, size := utf8.DecodeRuneInString(s)
	return s[size:]
}

// hasDescendant reports whether any element below n matches.
func (n *node) hasDescendant(match func(*node) bool) bool {
	for _, k := range n.kids {
		if match(k) || k.hasDescendant(match) {
			return true
		}
	}
	return false
}
