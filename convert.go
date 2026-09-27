package main

// The conversion pipeline, independent of how it is presented. The command
// line and the GUI both drive it and render its progress events their own way.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/image/draw"
)

// Stages reported while converting, in order.
const (
	stageMeasuring  = "measuring"
	stageSplitting  = "splitting"
	stageTrimming   = "trimming"
	stageRendering  = "rendering"
	stagePackaging  = "packaging"
	stageValidating = "validating"
	stageEpubcheck  = "epubcheck"
)

// Plan is what the converter decided once every page was measured.
type Plan struct {
	DPI     int            // resolution rendered at
	Scan    bool           // the PDF is a scan and DPI is its native resolution
	Pages   int            // pages in the book
	Sources int            // PDF pages they are made from
	Of      int            // pages in the PDF
	Trimmed int            // pages whose margins were trimmed
	Tally   map[string]int // pages by orientation
	Canvas  Size
	Orient  Orientation
	Mixed   bool
}

// Event reports progress. Plan is set once, on the first rendering event.
type Event struct {
	Stage       string
	Done, Total int
	Plan        *Plan
}

// PageInfo describes one converted page.
type PageInfo struct {
	Source Size   // as rendered
	Size   Size   // as encoded
	Orient string // of the source page
}

// Result describes a finished conversion.
type Result struct {
	Plan
	PageInfo  []PageInfo
	Bytes     int
	Flattened int
	Elapsed   time.Duration
	Cover     []byte // small JPEG of the first page, for previews
	TOC       int    // entries taken from the PDF's outline; 0 lists every page

	Validated bool      // built-in validation ran
	Problems  []Problem // built-in findings
	Epubcheck Epubcheck
	Passed    bool // everything that ran passed
}

// convertBook converts o.Input to o.Output. It returns an error when no EPUB
// could be written; a written EPUB that fails validation is reported in the
// Result, and the file is kept so it can be inspected.
func convertBook(ctx context.Context, eng *engine, o Options, report func(Event)) (*Result, error) {
	start := time.Now()
	if report == nil {
		report = func(Event) {}
	}

	pdf, err := os.ReadFile(o.Input)
	if err != nil {
		return nil, err
	}

	report(Event{Stage: stageMeasuring})
	spans, err := parsePages(o.Pages)
	if err != nil {
		return nil, err
	}
	first, err := eng.newWorker(pdf, o.Password)
	if err != nil {
		return nil, err
	}
	defer first.Close()
	total, err := first.pageCount()
	if err != nil {
		return nil, err
	}
	if total == 0 {
		return nil, errors.New("the PDF has no pages")
	}
	// sel holds the PDF pages the book is made from.
	sel, err := selectPages(spans, total)
	if err != nil {
		return nil, err
	}

	plan := Plan{Sources: len(sel), Of: total, Tally: map[string]int{}, Mixed: o.Mixed, DPI: o.DPI}
	if o.DPI == dpiAuto {
		plan.DPI = defaultDPI
		if ppi, ok := first.scanPPI(sel); ok {
			plan.DPI, plan.Scan = ppi, true
		}
	}
	o.DPI = plan.DPI

	// Measure every page before rendering any, so the canvas is known up
	// front and only the encoded pages need to be held in memory.
	srcSizes := make([]Size, len(sel))
	for k := range sel {
		if srcSizes[k], err = first.pageSize(sel[k], o.DPI); err != nil {
			return nil, err
		}
		// Measuring loads and parses each page, which takes a while on a
		// long book, so it reports progress too.
		report(Event{Stage: stageMeasuring, Done: k + 1, Total: len(sel)})
	}

	// The book's pages: spreads split at their gutters and margins
	// trimmed, when asked.
	var gutters []float64
	if o.Split {
		report(Event{Stage: stageSplitting})
		if gutters, err = findGutters(ctx, eng, pdf, first, srcSizes, sel, o, func(done int) {
			report(Event{Stage: stageSplitting, Done: done, Total: len(sel)})
		}); err != nil {
			return nil, err
		}
	}
	pages := layoutPages(sel, srcSizes, o.Split, o.Direction == "rtl", gutters)
	n := len(pages)
	plan.Pages = n
	if o.Trim {
		report(Event{Stage: stageTrimming})
		found, err := analyseContent(ctx, eng, pdf, first, pages, srcSizes, sel, o, func(done int) {
			report(Event{Stage: stageTrimming, Done: done, Total: len(sel)})
		})
		if err != nil {
			return nil, err
		}
		plan.Trimmed = trimPages(pages, found)
	}
	sizes := make([]Size, n)
	for i, p := range pages {
		sizes[i] = p.size()
		plan.Tally[sizes[i].Orientation()]++
	}

	// A damaged outline is not worth failing the book over; it falls back
	// to one entry per page. Bookmarks open the first page made from their
	// PDF page, and the page list names that page after it.
	firstOf := map[int]int{}
	for i, p := range pages {
		if _, ok := firstOf[p.src]; !ok {
			firstOf[p.src] = i
		}
	}
	var toc []TOCEntry
	if o.TOC == tocBookmarks {
		toc, _ = first.outline(func(src int) int {
			if i, ok := firstOf[src]; ok {
				return i
			}
			return -1
		})
	}
	srcLabels := first.pageLabels(sel)
	labels := make([]string, n)
	for k, src := range sel {
		labels[firstOf[src]] = srcLabels[k]
	}

	plan.Canvas = chooseCanvas(sizes, o.MaxEdge)
	plan.Orient = bookOrientation(plan.Canvas, o.Orient, o.Mixed)
	report(Event{Stage: stageRendering, Total: n, Plan: &plan})

	encoded, flattened, err := buildPages(ctx, eng, pdf, first, pages, srcSizes, sel, plan.Canvas, o, func(done int) {
		report(Event{Stage: stageRendering, Done: done, Total: n})
	})
	if err != nil {
		return nil, err
	}

	report(Event{Stage: stagePackaging})
	book := &Book{
		Title:     o.Title,
		Lang:      o.Lang,
		Direction: o.Direction,
		Orient:    plan.Orient,
		Canvas:    plan.Canvas,
		Mixed:     o.Mixed,
		Pages:     encoded,
		TOC:       toc,
		Labels:    labels,
		Modified:  time.Now(),
		ID:        newUUID(),
	}
	data, err := writeAtomically(o.Output, book)
	if err != nil {
		return nil, err
	}

	res := &Result{
		Plan:      plan,
		Bytes:     len(data),
		Flattened: flattened,
		Cover:     encoded[0].Thumb,
		TOC:       countEntries(toc),
		Passed:    true,
	}
	for _, p := range encoded {
		res.PageInfo = append(res.PageInfo, PageInfo{p.Source, p.Size, p.Orient})
	}

	if o.Validate {
		report(Event{Stage: stageValidating})
		want := Expect{Pages: n, Grayscale: o.Grayscale}
		if !o.Mixed {
			want.Canvas = &plan.Canvas
		}
		res.Validated = true
		res.Problems = validatePackage(data, want)
		for _, p := range res.Problems {
			if p.Fails() {
				res.Passed = false
			}
		}

		if o.Epubcheck {
			report(Event{Stage: stageEpubcheck})
			res.Epubcheck = runEpubcheck(ctx, o.Output)
			if res.Epubcheck.Ran && !res.Epubcheck.Passed {
				res.Passed = false
			}
		}
	}
	res.Elapsed = time.Since(start)
	return res, nil
}

// eachSource runs fn on every PDF page in srcs, in parallel. Each goroutine
// owns a PDF engine instance; first, already open, is one of them and is
// left open for the caller.
func eachSource(ctx context.Context, eng *engine, pdf []byte, first *worker, srcs int, o Options, fn func(w *worker, k int) error) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	next := make(chan int)
	go func() {
		defer close(next)
		for k := range srcs {
			select {
			case next <- k:
			case <-ctx.Done():
				return
			}
		}
	}()

	var wg sync.WaitGroup
	for g := range min(o.Jobs, srcs) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := first
			if g > 0 {
				var err error
				if w, err = eng.newWorker(pdf, o.Password); err != nil {
					cancel(err)
					return
				}
				defer w.Close()
			}
			for k := range next {
				if err := fn(w, k); err != nil {
					cancel(err)
					return
				}
			}
		}()
	}
	wg.Wait()
	return context.Cause(ctx)
}

// analysisDPI is enough to find margins and gutters, and quick to render.
const analysisDPI = 48

// findGutters finds where each spread folds, from a low-resolution
// rendering; pages that are not spreads are left at 0.
func findGutters(ctx context.Context, eng *engine, pdf []byte, first *worker, srcSizes []Size, sel []int, o Options, tick func(done int)) ([]float64, error) {
	gutters := make([]float64, len(sel))
	var done atomic.Int64
	var tickMu sync.Mutex
	err := eachSource(ctx, eng, pdf, first, len(sel), o, func(w *worker, k int) error {
		if isSpread(srcSizes[k]) {
			img, err := w.render(sel[k], min(analysisDPI, o.DPI))
			if err != nil {
				return err
			}
			gutters[k] = findGutter(img)
		}
		tickMu.Lock()
		tick(int(done.Add(1)))
		tickMu.Unlock()
		return nil
	})
	return gutters, err
}

// partsOf lists the book pages cut from each selected PDF page.
func partsOf(pages []bookPage, sel []int) [][]int {
	parts := make([][]int, len(sel))
	slot := slotOf(sel)
	for i, p := range pages {
		k := slot(p.src)
		parts[k] = append(parts[k], i)
	}
	return parts
}

// analyseContent finds where the content is on each book page, from a
// low-resolution rendering of its PDF page.
func analyseContent(ctx context.Context, eng *engine, pdf []byte, first *worker, pages []bookPage, srcSizes []Size, sel []int, o Options, tick func(done int)) ([]content, error) {
	found := make([]content, len(pages))
	parts := partsOf(pages, sel)
	var done atomic.Int64
	var tickMu sync.Mutex
	err := eachSource(ctx, eng, pdf, first, len(sel), o, func(w *worker, k int) error {
		img, err := w.render(sel[k], min(analysisDPI, o.DPI))
		if err != nil {
			return err
		}
		sx := float64(img.Rect.Dx()) / float64(srcSizes[k].W)
		sy := float64(img.Rect.Dy()) / float64(srcSizes[k].H)
		for _, i := range parts[k] {
			c := pages[i].crop
			r := image.Rect(int(float64(c.Min.X)*sx), int(float64(c.Min.Y)*sy),
				int(math.Ceil(float64(c.Max.X)*sx)), int(math.Ceil(float64(c.Max.Y)*sy)))
			found[i] = findContent(img, r)
		}
		tickMu.Lock()
		tick(int(done.Add(1)))
		tickMu.Unlock()
		return nil
	})
	return found, err
}

// buildPages renders every PDF page once and makes the book pages cut from
// it: processed, fitted to the canvas and encoded, in parallel.
func buildPages(ctx context.Context, eng *engine, pdf []byte, first *worker, pages []bookPage, srcSizes []Size, sel []int, canvas Size, o Options, tick func(done int)) ([]Page, int, error) {
	out := make([]Page, len(pages))
	parts := partsOf(pages, sel)
	var flattened, done atomic.Int64
	var tickMu sync.Mutex
	err := eachSource(ctx, eng, pdf, first, len(sel), o, func(w *worker, k int) error {
		img, err := w.render(sel[k], o.DPI)
		if err != nil {
			return err
		}
		if got := (Size{img.Rect.Dx(), img.Rect.Dy()}); got != srcSizes[k] {
			return fmt.Errorf("page %d rendered at %dx%d but measured %dx%d", sel[k]+1, got.W, got.H, srcSizes[k].W, srcSizes[k].H)
		}
		for _, i := range parts[k] {
			p, flat, err := buildPage(img, pages[i], i == 0, canvas, o)
			if err != nil {
				return err
			}
			out[i] = p
			if flat {
				flattened.Add(1)
			}
			tickMu.Lock() // keep reports in order and never concurrent
			tick(int(done.Add(1)))
			tickMu.Unlock()
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return out, int(flattened.Load()), nil
}

// buildPage makes one book page from its PDF page, rendered whole as full;
// the book's first page also gets a thumbnail.
func buildPage(full *image.RGBA, bp bookPage, first bool, canvas Size, o Options) (Page, bool, error) {
	img := full
	if bp.crop != full.Rect {
		img = image.NewRGBA(image.Rect(0, 0, bp.crop.Dx(), bp.crop.Dy()))
		draw.Draw(img, img.Rect, full, bp.crop.Min, draw.Src)
	}
	size, i := bp.size(), bp.src

	target := canvas
	if o.Mixed {
		target = size
	}

	// Detection and pad colour both look at the page as rendered, before it
	// is scaled, so neither is skewed by resampling.
	var fl Flatten
	if o.FlattenBG {
		fl = detectFlatten(img)
	}
	fitted := fitAndPad(img, target, padColour(img, target))

	var enc image.Image = fitted
	if o.Grayscale {
		g := toGray(fitted)
		if fl.Active {
			flattenGray(g, fl)
		}
		enc = g
	} else if fl.Active {
		flattenRGB(fitted, fl)
	}

	var buf bytes.Buffer
	if err := encodeJPEG(&buf, enc, o.Quality); err != nil {
		return Page{}, false, fmt.Errorf("page %d: encoding: %w", i+1, err)
	}
	p := Page{JPEG: buf.Bytes(), Size: target, Source: size, Orient: size.Orientation()}
	if first {
		p.Thumb = thumbnail(enc, 360)
	}
	return p, fl.Active, nil
}

// thumbnail is a small JPEG of m, at most maxW pixels wide.
func thumbnail(m image.Image, maxW int) []byte {
	b := m.Bounds()
	w, h := b.Dx(), b.Dy()
	if w > maxW {
		w, h = maxW, max(1, h*maxW/w)
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), m, b, draw.Src, nil)
	var buf bytes.Buffer
	if encodeJPEG(&buf, dst, 82) != nil {
		return nil
	}
	return buf.Bytes()
}

// writeAtomically writes the EPUB next to its destination and renames it into
// place, so an interrupted run never leaves a half-written book behind or
// destroys the previous one. It returns the bytes written.
func writeAtomically(dst string, b *Book) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeEPUB(&buf, b); err != nil {
		return nil, fmt.Errorf("packaging: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(dst), ".leafbind-*.tmp")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename

	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
