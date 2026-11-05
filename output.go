// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"
)

// termSafe returns s with everything a terminal could act on escaped: control
// characters (C0, DEL, C1), Unicode format characters such as bidirectional
// overrides, any other non-printable rune, and bytes that are not UTF-8.
// Device strings, and errors that quote them, pass through it before they
// are printed as text; JSON output escapes on its own and keeps the original.
//
// Tab, newline and carriage return become \t, \n and \r, other ASCII
// controls and invalid bytes \xNN, other runes \uNNNN or \UNNNNNNNN. A
// string of printable characters is returned as it is.
func termSafe(s string) string {
	i := 0
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if !printable(r, size) {
			break
		}
		i += size
	}
	if i == len(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 16)
	b.WriteString(s[:i])
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case printable(r, size):
			b.WriteString(s[i : i+size])
		case r == utf8.RuneError && size == 1:
			_, _ = fmt.Fprintf(&b, `\x%02x`, s[i])
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r < 0x80:
			_, _ = fmt.Fprintf(&b, `\x%02x`, r)
		case r <= 0xFFFF:
			_, _ = fmt.Fprintf(&b, `\u%04x`, r)
		default:
			_, _ = fmt.Fprintf(&b, `\U%08x`, r)
		}
		i += size
	}
	return b.String()
}

// printable reports whether the rune decoded with the given size can be
// printed as it is.
func printable(r rune, size int) bool {
	if r == utf8.RuneError && size == 1 {
		return false
	}
	return r == ' ' || unicode.IsPrint(r)
}

// termSafeError is an error whose message is printed through termSafe. It
// unwraps to the original error, so the exit code does not change.
type termSafeError struct{ err error }

func (e *termSafeError) Error() string { return termSafe(e.err.Error()) }
func (e *termSafeError) Unwrap() error { return e.err }

// safeError wraps err so that its message is terminal safe. It returns err
// itself when nothing needs escaping, and nil for nil.
func safeError(err error) error {
	if err == nil {
		return nil
	}
	if msg := err.Error(); termSafe(msg) == msg {
		return err
	}
	return &termSafeError{err: err}
}

// outputWriter remembers the first error of the writer it wraps and refuses
// every write after it, so that text output, which is printed piecewise with
// the errors ignored, can still end the command with exit 10 when standard
// output fails (a full disk, a closed file).
type outputWriter struct {
	w   io.Writer
	err error
}

// newOutputWriter wraps w.
func newOutputWriter(w io.Writer) *outputWriter { return &outputWriter{w: w} }

func (o *outputWriter) Write(p []byte) (int, error) {
	if o.err != nil {
		return 0, o.err
	}
	n, err := o.w.Write(p)
	if err != nil {
		o.err = err
	}
	return n, err
}

// result returns nil, or the first write error as an internal error.
func (o *outputWriter) result() error {
	if o.err == nil {
		return nil
	}
	return withExit(exitInternal, fmt.Errorf("write output: %w", o.err))
}
