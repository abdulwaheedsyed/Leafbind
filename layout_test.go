// Copyright 2026 Syed Abdul Waheed
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"strings"
	"testing"
)

func TestLayoutPages(t *testing.T) {
	sizes := []Size{{400, 600}, {800, 600}, {620, 600}}
	pages := layoutPages([]int{0, 1, 2}, sizes, true, false, nil)
	want := []bookPage{
		{0, 0, image.Rect(0, 0, 400, 600)},
		{1, 1, image.Rect(0, 0, 400, 600)}, {1, 2, image.Rect(400, 0, 800, 600)},
		{2, 0, image.Rect(0, 0, 620, 600)}, // barely wider than tall: not a spread
	}
	if len(pages) != len(want) {
		t.Fatalf("got %v", pages)
	}
	for i := range want {
		if pages[i] != want[i] {
			t.Errorf("page %d: %v, want %v", i, pages[i], want[i])
		}
	}
	rtl := layoutPages([]int{1}, []Size{{800, 600}}, true, true, nil)
	if rtl[0].crop.Min.X != 400 || rtl[1].crop.Min.X != 0 {
		t.Errorf("right to left, the right half comes first: %v", rtl)
	}
	if got := layoutPages([]int{1}, []Size{{800, 600}}, false, false, nil); len(got) != 1 {
		t.Error("split without being asked")
	}
}

func page(w, h int, bg color.RGBA, ink image.Rectangle) *image.RGBA {
	m := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := bg
			if image.Pt(x, y).In(ink) {
				c = color.RGBA{20, 20, 20, 255}
			}
			m.SetRGBA(x, y, c)
		}
	}
	return m
}

func TestFindContent(t *testing.T) {
	cream := color.RGBA{240, 232, 210, 255}
	m := page(200, 300, cream, image.Rect(40, 60, 160, 240))
	m.SetRGBA(5, 5, color.RGBA{0, 0, 0, 255}) // a speck of dust
	c := findContent(m, m.Rect)
	near := func(a, b float64) bool { return math.Abs(a-b) < 0.011 }
	if c.blank || c.bleed || !near(c.box.x0, 0.2) || !near(c.box.y0, 0.2) || !near(c.box.x1, 0.8) || !near(c.box.y1, 0.8) {
		t.Errorf("found %+v, want the box 0.2-0.8 on both axes", c)
	}
	if c := findContent(page(100, 100, cream, image.Rectangle{}), image.Rect(0, 0, 100, 100)); !c.blank {
		t.Errorf("a blank page: %+v", c)
	}
	if c := findContent(page(100, 100, cream, image.Rect(0, 0, 100, 100)), image.Rect(0, 0, 100, 100)); !c.blank {
		// all ink is just a dark background
		t.Errorf("a uniform page: %+v", c)
	}
	photo := image.NewRGBA(image.Rect(0, 0, 100, 100))
	for y := range 100 {
		for x := range 100 {
			v := uint8((x*7 + y*13) % 256)
			photo.SetRGBA(x, y, color.RGBA{v, v, v, 255})
		}
	}
	if c := findContent(photo, photo.Rect); !c.bleed {
		t.Errorf("content to every edge should bleed: %+v", c)
	}
}

func TestTrimPages(t *testing.T) {
	r := image.Rect(0, 0, 1000, 1000)
	pages := []bookPage{{0, 0, r}, {1, 0, r}, {2, 0, r}, {3, 0, r}}
	pages = append(pages, bookPage{4, 0, r})
	found := []content{
		{box: box{0.30, 0.1, 0.9, 0.9}, bg: 250},  // right-hand page: wide inner margin on the left
		{box: box{0.1, 0.1, 0.70, 0.85}, bg: 248}, // left-hand page
		{bleed: true}, // a photograph
		{blank: true, bg: 251},
		{box: box{0.2, 0.3, 0.8, 0.5}, bg: 60}, // a dark cover
	}
	if n := trimPages(pages, found); n != 3 {
		t.Errorf("trimmed %d pages, want 3", n)
	}
	// Both sides take the larger size, 0.67 x 0.87 with the margin.
	for _, i := range []int{0, 1, 3} {
		if w, h := pages[i].crop.Dx(), pages[i].crop.Dy(); w < 669 || w > 672 || h < 869 || h > 872 {
			t.Errorf("page %d cropped to %dx%d, want 670x870", i, w, h)
		}
	}
	if pages[4].crop != r {
		t.Error("the cover, on another background, was trimmed")
	}
	if pages[0].crop.Min.X <= pages[1].crop.Min.X {
		t.Errorf("the pages keep their own sides: %v and %v", pages[0].crop, pages[1].crop)
	}
	if pages[2].crop != r {
		t.Error("the photograph was trimmed")
	}
}

// Trimming makes the content fill the page, and splitting makes a spread
// two pages in reading order, through the whole pipeline.
func TestConvertTrimAndSplit(t *testing.T) {
	text := &[4]float64{0.2, 0.4, 0.8, 0.6}
	pages := []testPage{{W: 400, H: 600, Box: text}, {W: 400, H: 600, Box: text}}
	_, data := convertBytes(t, makePDF(pages), "36", "--trim")
	sizes := pageImages(t, data)
	// 60% x 20% of a portrait page, plus a margin: wider than tall now.
	if s := sizes[0]; s.W <= s.H {
		t.Errorf("trimmed page is %v, want it cropped to the wide box", s)
	}

	// A cover, then a spread with ink on its left half only.
	spread := []testPage{{W: 400, H: 600, Bar: true}, {W: 800, H: 600, Box: &[4]float64{0.1, 0.2, 0.4, 0.8}}}
	inkFirst := func(args ...string) bool {
		_, data := convertBytes(t, makePDF(spread), "36", args...)
		if n := len(pageImages(t, data)); n != 3 {
			t.Fatalf("%v: %d pages, want 3", args, n)
		}
		m, err := jpeg.Decode(bytes.NewReader(zipFile(t, data, "OEBPS/images/page-002.jpg")))
		if err != nil {
			t.Fatal(err)
		}
		var sum uint64
		b := m.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				v, _, _, _ := m.At(x, y).RGBA()
				sum += uint64(v >> 8)
			}
		}
		nav := string(zipFile(t, data, "OEBPS/nav.xhtml"))
		list := nav[strings.Index(nav, `epub:type="page-list"`):]
		if !strings.Contains(list, `page-002.xhtml">2</a>`) || strings.Contains(list, "page-003") {
			t.Errorf("%v: the page list names the spread's first half only:\n%s", args, list)
		}
		return sum/uint64(b.Dx()*b.Dy()) < 200
	}
	if !inkFirst("--split") {
		t.Error("left to right, the left half should come first")
	}
	if inkFirst("--split", "--rtl") {
		t.Error("right to left, the right half should come first")
	}
}

// spread draws a two-page spread: text blocks on either side of a gutter
// at the given fraction, and a dark fold line there when fold is set.
func spread(gutter float64, fold bool) *image.RGBA {
	const w, h = 400, 300
	m := page(w, h, color.RGBA{246, 244, 238, 255}, image.Rectangle{})
	g := int(gutter * w)
	ink := color.RGBA{30, 30, 30, 255}
	for y := 40; y < 260; y += 6 {
		for x := 30; x < g-25; x++ {
			m.SetRGBA(x, y, ink)
			m.SetRGBA(x, y+1, ink)
		}
		for x := g + 25; x < w-30; x++ {
			m.SetRGBA(x, y, ink)
			m.SetRGBA(x, y+1, ink)
		}
	}
	if fold {
		for y := range h {
			for x := g - 2; x <= g+2; x++ {
				m.SetRGBA(x, y, color.RGBA{90, 88, 84, 255})
			}
		}
	}
	return m
}

func TestFindGutter(t *testing.T) {
	near := func(got, want float64) bool { return math.Abs(got-want) < 0.015 }
	if g := findGutter(spread(0.46, false)); !near(g, 0.46) {
		t.Errorf("a blank gutter off centre: found %.3f, want 0.46", g)
	}
	if g := findGutter(spread(0.54, true)); !near(g, 0.54) {
		t.Errorf("a scanned fold: found %.3f, want 0.54", g)
	}
	// Text across the middle and no fold: the centre.
	m := page(400, 300, color.RGBA{250, 250, 250, 255}, image.Rect(30, 40, 370, 260))
	if g := findGutter(m); g != 0.5 {
		t.Errorf("no gutter to find: %.3f, want the centre", g)
	}
	// A gutter far from the middle is not looked for; the centre is used.
	if g := findGutter(spread(0.3, false)); g != 0.5 {
		t.Errorf("a gap outside the middle band: %.3f, want the centre", g)
	}
}

func TestLayoutAtGutter(t *testing.T) {
	pages := layoutPages([]int{0}, []Size{{800, 600}}, true, false, []float64{0.45})
	if pages[0].crop.Max.X != 360 || pages[1].crop.Min.X != 360 {
		t.Errorf("split at %v, want 360", pages)
	}
}
