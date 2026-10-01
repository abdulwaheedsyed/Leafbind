// Copyright 2026 Syed Abdul Waheed
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package main

import (
	"fmt"
	"io"
)

// detachConsole only matters on Windows.
func detachConsole() {}

func reportGUIError(stderr io.Writer, err error) {
	fmt.Fprintf(stderr, "error: %v\n", err)
}
