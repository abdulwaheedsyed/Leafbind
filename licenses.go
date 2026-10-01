// Copyright 2026 Syed Abdul Waheed
// SPDX-License-Identifier: Apache-2.0

package main

import _ "embed"

// The license, the NOTICE file the license requires to be passed on, and
// the third-party notices travel inside the binary, so a copy distributed
// on its own still carries them. Print them with --licenses.

//go:embed LICENSE
var licenseText string

//go:embed NOTICE
var noticeText string

//go:embed THIRD_PARTY_NOTICES.md
var noticesText string
