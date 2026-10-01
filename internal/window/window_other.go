// Copyright 2026 Syed Abdul Waheed
// SPDX-License-Identifier: Apache-2.0

//go:build !windows && !darwin

package window

func run(Options) error { return ErrUnavailable }

func closeWindow() {}
