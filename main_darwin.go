package main

import "runtime"

// AppKit works only on the process's main thread. Locking it here, in
// init, keeps the main goroutine on that thread, so the native window can
// run from main.
func init() { runtime.LockOSThread() }
