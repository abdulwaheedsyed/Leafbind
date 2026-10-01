// Copyright 2026 Syed Abdul Waheed
// SPDX-License-Identifier: Apache-2.0

package main

// Deciding the book's pages from the PDF's: splitting two-page spreads
// into single pages, and trimming empty margins, both only when asked.

import (
	"image"
	"math"
)

// bookPage is one page of the book: a whole PDF page or half of a spread,
// cropped to crop, in pixels of the PDF page rendered at the book's DPI.
type bookPage struct {
	src  int // index of the PDF page
	part int // 0 the whole page; 1 and 2 the halves, in reading order
	crop image.Rectangle
}

func (p bookPage) size() Size { return Size{p.crop.Dx(), p.crop.Dy()} }

// isSpread reports whether a page is wide enough to be two pages side by
// side. A single landscape page, such as a slide, is too; --split is for
// books that are spreads throughout, and is off by default.
func isSpread(s Size) bool { return s.W*10 > s.H*11 }

// layoutPages lists the book's pages for the selected PDF pages, which
// render at sizes. With split, each spread becomes its two halves, the
// right-hand one first in a right-to-left book, divided at gutters[k], a
// fraction of the width; nil gutters divide at the centre.
func layoutPages(sel []int, sizes []Size, split, rtl bool, gutters []float64) []bookPage {
	var pages []bookPage
	for k, src := range sel {
		s := sizes[k]
		whole := image.Rect(0, 0, s.W, s.H)
		if !split || !isSpread(s) {
			pages = append(pages, bookPage{src: src, crop: whole})
			continue
		}
		at := s.W / 2
		if gutters != nil && gutters[k] > 0 {
			at = int(math.Round(gutters[k] * float64(s.W)))
		}
		left, right := image.Rect(0, 0, at, s.H), image.Rect(at, 0, s.W, s.H)
		if rtl {
			left, right = right, left
		}
		pages = append(pages, bookPage{src, 1, left}, bookPage{src, 2, right})
	}
	return pages
}

// ----- gutters -----

// The gutter is looked for in the middle of a spread only: a book's fold is
// never far from it, and text columns elsewhere must not be mistaken for it.
const gutterBand = 0.10 // each side of the centre, as a fraction of the width

// findGutter locates where a spread folds, as a fraction of its width. A
// scan shows the fold as a narrow dark line down the page; a spread made
// digitally shows it as a blank gap between the pages. Failing both, the
// centre is as good a guess as any.
func findGutter(img *image.RGBA) float64 {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 20 || h < 20 {
		return 0.5
	}
	bg := backgroundLuma(img, b)
	lo, hi := int(float64(w)*(0.5-gutterBand)), int(math.Ceil(float64(w)*(0.5+gutterBand)))
	ink := make([]float64, hi-lo) // share of each column that is not background
	for x := lo; x < hi; x++ {
		n := 0
		for y := b.Min.Y; y < b.Max.Y; y++ {
			l := int(lumaOf(at(img, b.Min.X+x, y)))
			if l-bg > contentThreshold || bg-l > contentThreshold {
				n++
			}
		}
		ink[x-lo] = float64(n) / float64(h)
	}
	centre := func(a, z int) float64 { return (float64(lo+a) + float64(z-a)/2) / float64(w) }

	// A fold: a run of columns dark nearly all the way down, and narrow.
	maxFold := max(1, w*3/100)
	best, bestDist := -1.0, math.Inf(1)
	for a := 0; a < len(ink); {
		if ink[a] < 0.6 {
			a++
			continue
		}
		z := a
		for z < len(ink) && ink[z] >= 0.6 {
			z++
		}
		if z-a <= maxFold {
			if c := centre(a, z); math.Abs(c-0.5) < bestDist {
				best, bestDist = c, math.Abs(c-0.5)
			}
		}
		a = z
	}
	if best > 0 {
		return best
	}

	// A gap: the widest run of blank columns, the nearer the centre the
	// better when two are as wide.
	bestLen := 0
	for a := 0; a < len(ink); {
		if ink[a] > 0.01 {
			a++
			continue
		}
		z := a
		for z < len(ink) && ink[z] <= 0.01 {
			z++
		}
		c := centre(a, z)
		if z-a > bestLen || z-a == bestLen && math.Abs(c-0.5) < bestDist {
			best, bestLen, bestDist = c, z-a, math.Abs(c-0.5)
		}
		a = z
	}
	if best > 0 && bestLen >= max(2, w/100) {
		return best
	}
	return 0.5
}

// backgroundLuma is the most common luma in r.
func backgroundLuma(img *image.RGBA, r image.Rectangle) int {
	var hist [256]int
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			hist[lumaOf(at(img, x, y))]++
		}
	}
	bg := 0
	for v := range hist {
		if hist[v] > hist[bg] {
			bg = v
		}
	}
	return bg
}

// ----- margins -----

// box is a rectangle as fractions of the part of the page it is in.
type box struct{ x0, y0, x1, y1 float64 }

func (b box) union(c box) box {
	return box{math.Min(b.x0, c.x0), math.Min(b.y0, c.y0), math.Max(b.x1, c.x1), math.Max(b.y1, c.y1)}
}

// content is what the analysis found on one book page.
type content struct {
	box   box
	blank bool // nothing but background
	bleed bool // content reaches the edges, like a photograph or a cover
	bg    int  // luma of the background
}

// contentThreshold is how far from the background, in luma, a pixel must
// be to count as content. Scans have paper texture and faint show-through
// well below it.
const contentThreshold = 48

// findContent locates what is not background within r of img. A pixel
// counts only beside another that differs too, so specks of dust and
// scanner noise do not hold the margins open, while a thin page number
// still does.
func findContent(img *image.RGBA, r image.Rectangle) content {
	r = r.Intersect(img.Bounds())
	w, h := r.Dx(), r.Dy()
	if w < 4 || h < 4 {
		return content{blank: true, bg: 255}
	}
	bg := backgroundLuma(img, r)
	ink := make([]bool, w*h)
	for y := range h {
		for x := range w {
			l := int(lumaOf(at(img, r.Min.X+x, r.Min.Y+y)))
			ink[y*w+x] = l-bg > contentThreshold || bg-l > contentThreshold
		}
	}
	rows, cols := make([]int, h), make([]int, w)
	for y := range h {
		for x := range w {
			if !ink[y*w+x] {
				continue
			}
			alone := true
			for dy := -1; dy <= 1 && alone; dy++ {
				for dx := -1; dx <= 1; dx++ {
					nx, ny := x+dx, y+dy
					if (dx != 0 || dy != 0) && nx >= 0 && ny >= 0 && nx < w && ny < h && ink[ny*w+nx] {
						alone = false
						break
					}
				}
			}
			if !alone {
				rows[y]++
				cols[x]++
			}
		}
	}
	span := func(counts []int) (int, int, bool) {
		lo, hi := -1, -1
		for i, n := range counts {
			if n > 0 {
				if lo < 0 {
					lo = i
				}
				hi = i + 1
			}
		}
		return lo, hi, lo >= 0
	}
	y0, y1, ok := span(rows)
	x0, x1, ok2 := span(cols)
	if !ok || !ok2 {
		return content{blank: true, bg: bg}
	}
	b := box{float64(x0) / float64(w), float64(y0) / float64(h), float64(x1) / float64(w), float64(y1) / float64(h)}
	edge := 0.01
	return content{box: b, bg: bg, bleed: b.x0 <= edge && b.y0 <= edge && b.x1 >= 1-edge && b.y1 >= 1-edge}
}

// trimMargin is the space kept around the content, as a fraction of the
// page, so text never runs to the edge of the screen.
const trimMargin = 0.035

// trimPages crops every page to the content of its group, so pages keep
// one size and position as the reader turns them. Left-hand and right-hand
// pages are grouped apart, since books have wider inner margins, and then
// made the same size. A page whose content fills it, such as a
// photograph, or whose background is not the book's, such as a coloured
// cover, is left whole, and is not counted towards the group. It returns
// how many pages were trimmed.
func trimPages(pages []bookPage, found []content) int {
	// The book's background is the one most of its pages share.
	var votes [256 / 8]int
	for _, c := range found {
		if !c.bleed {
			votes[c.bg/8]++
		}
	}
	common := 0
	for v := range votes {
		if votes[v] > votes[common] {
			common = v
		}
	}
	whole := make([]bool, len(found))
	for i, c := range found {
		d := c.bg - (common*8 + 4)
		whole[i] = c.bleed || d > 32 || d < -32
	}

	// Group by kind and side: whole pages by their position in the book,
	// halves of spreads by which half they are.
	key := func(i int) int {
		if pages[i].part == 0 {
			return i % 2
		}
		return 1 + pages[i].part // 2 or 3
	}
	groups := map[int]box{}
	have := map[int]bool{}
	for i, c := range found {
		if c.blank || whole[i] {
			continue
		}
		k := key(i)
		if have[k] {
			groups[k] = groups[k].union(c.box)
		} else {
			groups[k], have[k] = c.box, true
		}
	}
	for k, b := range groups {
		b = box{math.Max(0, b.x0-trimMargin), math.Max(0, b.y0-trimMargin), math.Min(1, b.x1+trimMargin), math.Min(1, b.y1+trimMargin)}
		if b.x1-b.x0 >= 0.97 && b.y1-b.y0 >= 0.97 {
			delete(groups, k) // nothing worth trimming
			continue
		}
		groups[k] = b
	}
	// Pair the sides: both take the larger width and height, grown around
	// their own centre and kept inside the page.
	for _, pair := range [][2]int{{0, 1}, {2, 3}} {
		a, okA := groups[pair[0]]
		b, okB := groups[pair[1]]
		if !okA || !okB {
			continue
		}
		w := math.Max(a.x1-a.x0, b.x1-b.x0)
		h := math.Max(a.y1-a.y0, b.y1-b.y0)
		groups[pair[0]], groups[pair[1]] = resize(a, w, h), resize(b, w, h)
	}

	trimmed := 0
	for i := range pages {
		b, ok := groups[key(i)]
		if !ok || whole[i] {
			continue
		}
		r := pages[i].crop
		w, h := float64(r.Dx()), float64(r.Dy())
		pages[i].crop = image.Rect(
			r.Min.X+int(math.Floor(b.x0*w)), r.Min.Y+int(math.Floor(b.y0*h)),
			r.Min.X+int(math.Ceil(b.x1*w)), r.Min.Y+int(math.Ceil(b.y1*h)),
		).Intersect(r)
		trimmed++
	}
	return trimmed
}

// resize grows b to w x h about its centre, shifting it back inside the
// unit square where it would stick out.
func resize(b box, w, h float64) box {
	fit := func(lo, hi, size float64) (float64, float64) {
		c := (lo + hi) / 2
		lo, hi = c-size/2, c+size/2
		if lo < 0 {
			lo, hi = 0, size
		}
		if hi > 1 {
			lo, hi = 1-size, 1
		}
		return math.Max(0, lo), math.Min(1, hi)
	}
	x0, x1 := fit(b.x0, b.x1, w)
	y0, y1 := fit(b.y0, b.y1, h)
	return box{x0, y0, x1, y1}
}
