// Copyright 2026 Syed Abdul Waheed
// SPDX-License-Identifier: Apache-2.0

package main

// Converting several PDFs in one run: leafbind --out books/ a.pdf b.pdf
// scans/. Every book shares one PDF engine, and a book that fails does not
// stop the others.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type batchItem struct{ in, out, title string }

// expandInputs lists the PDFs to convert: files as given, and the PDFs
// directly inside each folder, in name order.
func expandInputs(paths []string) ([]string, error) {
	var out []string
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if !st.IsDir() {
			out = append(out, p)
			continue
		}
		entries, err := os.ReadDir(p)
		if err != nil {
			return nil, err
		}
		var found []string
		for _, e := range entries {
			if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".pdf") {
				found = append(found, filepath.Join(p, e.Name()))
			}
		}
		if len(found) == 0 {
			return nil, fmt.Errorf("%s has no PDFs", p)
		}
		sort.Strings(found)
		out = append(out, found...)
	}
	return out, nil
}

// planBatch names each book after its PDF, in dir. Two PDFs with the same
// name would write the same book, so that is refused before anything runs.
func planBatch(inputs []string, dir string) ([]batchItem, error) {
	var items []batchItem
	byOut := map[string]string{}
	seen := map[string]bool{}
	for _, in := range inputs {
		abs, _ := filepath.Abs(in)
		if seen[abs] {
			continue // the same file named twice, or in a folder given too
		}
		seen[abs] = true
		base := filepath.Base(in)
		title := strings.TrimSuffix(base, filepath.Ext(base))
		out := filepath.Join(dir, title+".epub")
		key := strings.ToLower(out) // case-insensitive file systems
		if prev, ok := byOut[key]; ok {
			return nil, fmt.Errorf("%s and %s would both become %s", prev, in, out)
		}
		byOut[key] = in
		items = append(items, batchItem{in, out, title})
	}
	return items, nil
}

// convertBatch converts every input into o.OutDir. It reports false when
// any book failed, after trying them all.
func convertBatch(ctx context.Context, o Options, out io.Writer) (bool, error) {
	inputs, err := expandInputs(o.Inputs)
	if err != nil {
		return false, err
	}
	items, err := planBatch(inputs, o.OutDir)
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(o.OutDir, 0o755); err != nil {
		return false, err
	}

	fmt.Fprintf(out, "Converting %d PDFs into %s\n", len(items), o.OutDir)
	fmt.Fprintf(out, "Starting PDF engine...\n")
	eng, err := newEngine(ctx, o.Jobs)
	if err != nil {
		return false, err
	}
	defer eng.Close()

	var failed []string
	for i, it := range items {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		fmt.Fprintf(out, "\n[%d/%d] %s\n", i+1, len(items), it.in)
		bo := o
		bo.Input, bo.Output, bo.Title = it.in, it.out, it.title
		ok, err := convertWith(ctx, eng, bo, out)
		switch {
		case errors.Is(err, context.Canceled):
			return false, err
		case err != nil:
			fmt.Fprintf(out, "error: %v\n", describeError(err))
			failed = append(failed, fmt.Sprintf("%s: %v", it.in, describeError(err)))
		case !ok:
			failed = append(failed, it.in+": validation failed")
		}
	}

	fmt.Fprintf(out, "\nConverted %d of %d.\n", len(items)-len(failed), len(items))
	for _, f := range failed {
		fmt.Fprintf(out, "  failed: %s\n", f)
	}
	return len(failed) == 0, nil
}

// describeError adds how to fix an error, where there is a way.
func describeError(err error) error {
	if errors.Is(err, errPasswordNeeded) {
		return fmt.Errorf("%w; give its password with --password, or in LEAFBIND_PASSWORD", err)
	}
	return err
}
