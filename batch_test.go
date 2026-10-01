// Copyright 2026 Syed Abdul Waheed
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseArgsBatch(t *testing.T) {
	o, err := parseArgs([]string{"--out", "books", "a.pdf", "scans"})
	if err != nil || o.OutDir != "books" || strings.Join(o.Inputs, ",") != "a.pdf,scans" {
		t.Fatalf("got %+v, %v", o, err)
	}
	for name, args := range map[string][]string{
		"no inputs":      {"--out", "books"},
		"title in batch": {"-o", "books", "--title", "X", "a.pdf"},
		"PDF as output":  {"a.pdf", "b.pdf"},
		"three names":    {"a.pdf", "b.pdf", "c.pdf"},
	} {
		if _, err := parseArgs(args); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestPlanBatch(t *testing.T) {
	items, err := planBatch([]string{"x/One.pdf", "y/Two.PDF", "x/One.pdf"}, "out")
	if err != nil || len(items) != 2 || items[1].out != filepath.Join("out", "Two.epub") || items[1].title != "Two" {
		t.Fatalf("got %+v, %v", items, err)
	}
	if _, err := planBatch([]string{"x/Same.pdf", "y/same.pdf"}, "out"); err == nil {
		t.Error("two PDFs that would write the same book were accepted")
	}
}

// A batch converts every PDF given and every PDF in a folder, carries on
// past one that fails, and reports failure at the end.
func TestConvertBatch(t *testing.T) {
	if testing.Short() {
		t.Skip("starts the PDF engine; skipped with -short")
	}
	dir := t.TempDir()
	scans := filepath.Join(dir, "scans")
	os.Mkdir(scans, 0o755)
	page := []testPage{{W: 400, H: 600}}
	os.WriteFile(filepath.Join(dir, "Deck.pdf"), makePDF([]testPage{{W: 960, H: 540}}), 0o644)
	os.WriteFile(filepath.Join(scans, "A.pdf"), makePDF(page), 0o644)
	os.WriteFile(filepath.Join(scans, "B.pdf"), makePDF(page), 0o644)
	os.WriteFile(filepath.Join(scans, "notes.txt"), []byte("not a PDF"), 0o644)
	os.WriteFile(filepath.Join(dir, "Broken.pdf"), []byte("%PDF-1.4 nonsense"), 0o644)

	outDir := filepath.Join(dir, "books")
	o, err := parseArgs([]string{"--dpi", "36", "--jobs", "1", "--out", outDir, filepath.Join(dir, "Deck.pdf"), filepath.Join(dir, "Broken.pdf"), scans})
	if err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	ok, err := convertBatch(context.Background(), o, &log)
	if err != nil || ok {
		t.Fatalf("ok=%v err=%v, want a reported failure\n%s", ok, err, log.String())
	}
	for _, name := range []string{"Deck.epub", "A.epub", "B.epub"} {
		data, err := os.ReadFile(filepath.Join(outDir, name))
		if err != nil {
			t.Errorf("%s was not written", name)
			continue
		}
		title := strings.TrimSuffix(name, ".epub")
		if !strings.Contains(string(zipFile(t, data, "OEBPS/content.opf")), "<dc:title>"+title+"</dc:title>") {
			t.Errorf("%s is not titled %s", name, title)
		}
	}
	if !strings.Contains(log.String(), "Converted 3 of 4.") || !strings.Contains(log.String(), "failed: "+filepath.Join(dir, "Broken.pdf")) {
		t.Errorf("summary is missing:\n%s", log.String())
	}
}
