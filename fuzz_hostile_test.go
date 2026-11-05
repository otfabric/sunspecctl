// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	sunspec "github.com/otfabric/go-sunspec"
	"github.com/otfabric/go-sunspec/registry"
)

// wantNoControl fails when s holds anything a terminal acts on, other than
// the newlines and the tabs the table layout writes itself when allowTab.
func wantNoControl(t *testing.T, s string, allowTab bool) {
	t.Helper()
	if !utf8.ValidString(s) {
		t.Fatalf("output is not UTF-8: %q", s)
	}
	for _, r := range s {
		if r == '\n' || allowTab && r == '\t' {
			continue
		}
		if r != ' ' && !unicode.IsPrint(r) {
			t.Fatalf("output holds %U: %q", r, s)
		}
	}
}

// FuzzTermSafe checks the escaping of device strings for the terminal: the
// result holds printable runes only, and a printable string is left alone.
func FuzzTermSafe(f *testing.F) {
	for _, s := range []string{"", "Acme", "\x1b[2J", "\x1b]0;title\x07", "a\tb\nc\r", "\xff\xfe", "1\u202e2", "\u009b31m", "ü€😀", `\x41`, "\x00\x7f"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		got := termSafe(s)
		wantNoControl(t, got, false)
		if utf8.ValidString(s) && !strings.ContainsFunc(s, func(r rune) bool { return r != ' ' && !unicode.IsPrint(r) }) && got != s {
			t.Fatalf("printable %q changed to %q", s, got)
		}
		if len(got) < len(s) {
			t.Fatalf("escaping lost bytes: %q -> %q", s, got)
		}
	})
}

// FuzzTextOutput decodes registers as a device reports them and prints them
// as the text commands do: whatever the device sends, the output must not
// hold a control sequence.
func FuzzTextOutput(f *testing.F) {
	f.Add(uint16(1), []byte("\x00\x01\x00\x42\x1b[2J\x1b]0;x\x07\xff\xfe\n\tSN"))
	f.Add(uint16(160), []byte{0x00, 0xA0, 0x00, 0x30, 0x9b, 0x9b})
	f.Add(uint16(111), []byte{0x00, 0x6F, 0x00, 0x3C, 0x7F, 0xC0, 0, 0, 0x7F, 0x80, 0, 0})
	f.Fuzz(func(t *testing.T, id uint16, data []byte) {
		meta := registry.ByID(id)
		if meta == nil {
			return
		}
		regs := make([]uint16, len(data)/2)
		for i := range regs {
			regs[i] = uint16(data[2*i])<<8 | uint16(data[2*i+1])
		}
		dm, _ := sunspec.DecodeModel(regs, meta, 40000)
		if dm == nil {
			return
		}
		var out bytes.Buffer
		printDecodedModel(&out, dm)
		// printGroup's tabwriter pads with spaces; the only tabs left would
		// be a device's.
		wantNoControl(t, out.String(), false)
		if dm.Group != nil {
			for _, p := range dm.Group.Points {
				out.Reset()
				printPoint(&out, &p)
				wantNoControl(t, out.String(), false)
			}
		}
	})
}
