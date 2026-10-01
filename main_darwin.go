// Copyright 2026 Syed Abdul Waheed
// SPDX-License-Identifier: Apache-2.0

package main

import "runtime"

// AppKit works only on the process's main thread. Locking it here, in
// init, keeps the main goroutine on that thread, so the native window can
// run from main.
func init() { runtime.LockOSThread() }
