// Copyright 2026 Syed Abdul Waheed
// SPDX-License-Identifier: Apache-2.0

// Package window shows the interface in a native window, with the web view
// the operating system provides: WebView2 on Windows and WKWebView on macOS.
// Elsewhere, and where the web view cannot start, Run reports it and the
// caller falls back to a browser.
//
// Both are loaded at run time without cgo, so the program stays one
// cross-compiled binary. Linux is left out on purpose: loading WebKitGTK
// would make the Linux binary depend on glibc.
package window

import "errors"

// Options describe the window.
type Options struct {
	Title         string
	URL           string
	Width, Height int // initial size in points
	MinW, MinH    int
	DataDir       string // where the web view may keep its data (Windows)
}

// ErrUnavailable means no native window can be shown here.
var ErrUnavailable = errors.New("no native web view on this system")

// Run shows the window and returns once it is closed. It must be called on
// the main thread, which on macOS means the main goroutine with the thread
// locked from init. An error means the window never opened.
func Run(o Options) error { return run(o) }

// Close closes the window from any goroutine; Run then returns.
func Close() { closeWindow() }
