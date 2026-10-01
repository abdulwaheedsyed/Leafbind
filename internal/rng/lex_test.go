// Copyright 2026 Syed Abdul Waheed
// SPDX-License-Identifier: Apache-2.0

package rng

import "testing"

func TestLexerEscapes(t *testing.T) {
	l, err := newLexer("t.rnc", `start = element \x{61} { text }`)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(l.src); got != `start = element a { text }` {
		t.Errorf("escape resolved to %q", got)
	}
	// Beyond the last code point, or too large for 32 bits: refused, not
	// wrapped round into some other character.
	for _, bad := range []string{`\x{110000}`, `\x{FFFFFFFF}`, `\x{1FFFFFFFF}`, `\x{zz}`} {
		if _, err := newLexer("t.rnc", "start = "+bad); err == nil {
			t.Errorf("%s was accepted", bad)
		}
	}
}
