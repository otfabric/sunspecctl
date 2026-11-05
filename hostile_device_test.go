// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-sunspec"
	"github.com/otfabric/go-sunspec/testutil"
)

// Tests in this file play a hostile or broken device: what it reports is
// untrusted input, and every command must end with a clear error and the
// documented exit code, in bounded time and without a panic.

// f32 returns the two registers of a float32.
func f32(v float32) []uint16 {
	b := math.Float32bits(v)
	return []uint16{uint16(b >> 16), uint16(b)}
}

// floatMeter returns a device with a float model (111, single phase
// inverter, float) whose points hold NaN, +Inf and -Inf, as a device may
// report them. NaN is also SunSpec's "not implemented" value for floats, so
// an ordinary device reports it too.
func floatMeter(t *testing.T) *testutil.SunSpecHandler {
	t.Helper()
	common := newModel(t, 1, 0).str("Mn", "Acme")
	inv := newModel(t, 111, 0).
		set("A", f32(float32(math.NaN()))...).
		set("AphA", f32(float32(math.Inf(1)))...).
		set("PhVphA", f32(float32(math.Inf(-1)))...).
		set("Hz", f32(50.01)...)
	return testutil.NewSunSpecFixture(40000, 1, common.fixture(), inv.fixture())
}

// jsonPoints indexes the decoded points of a JSON model document by name,
// as generic JSON.
func jsonPoints(t *testing.T, model map[string]any) map[string]map[string]any {
	t.Helper()
	group, ok := model["Group"].(map[string]any)
	if !ok {
		t.Fatalf("model without group: %v", model)
	}
	out := map[string]map[string]any{}
	for _, p := range group["Points"].([]any) {
		pm := p.(map[string]any)
		out[pm["Name"].(string)] = pm
	}
	return out
}

func TestHostileNonFiniteFloatsInJSON(t *testing.T) {
	url := startDevice(t, floatMeter(t))

	// read --json printed nothing and failed with "json: unsupported value:
	// NaN" for any device with a float point that is not implemented.
	r := runCLI(t, "read", "--json", "--url", url)
	var models []map[string]any
	decodeJSON(t, r, &models)
	if len(models) != 2 {
		t.Fatalf("%d models, want 2", len(models))
	}
	pts := jsonPoints(t, models[1])
	// go-sunspec encodes a value that is not a finite number as null; the
	// model's RawRegisters keep the bits.
	for _, name := range []string{"A", "AphA", "PhVphA"} {
		if v, ok := pts[name]["RawValue"]; !ok || v != nil {
			t.Errorf("%s RawValue = %#v, want null", name, v)
		}
	}
	if pts["A"]["Implemented"] != false || pts["AphA"]["Implemented"] != true {
		t.Errorf("Implemented: A %v, AphA %v", pts["A"]["Implemented"], pts["AphA"]["Implemented"])
	}
	// Finite values stay numbers, and the document keeps the field order.
	if hz, ok := pts["Hz"]["RawValue"].(float64); !ok || math.Abs(hz-50.01) > 1e-4 {
		t.Errorf("Hz RawValue = %#v", pts["Hz"]["RawValue"])
	}

	for _, args := range [][]string{
		{"read-model", "--id", "111"},
		{"poll-model", "--id", "111", "--count", "2", "--interval", "1ms"},
		{"poll", "--count", "1"},
	} {
		r := runCLI(t, append(args, "--json", "--url", url)...)
		if r.err != nil || jsonDocuments(t, r.stdout) == 0 {
			t.Errorf("%v: err %v\n%s", args, r.err, r.stdout)
		}
	}
	r = runCLI(t, "read-point", "--model", "111", "--point", "PhVphA", "--json", "--url", url)
	var p map[string]any
	decodeJSON(t, r, &p)
	if v, ok := p["RawValue"]; !ok || v != nil || p["Implemented"] != true {
		t.Errorf("read-point RawValue = %#v", p["RawValue"])
	}

	// Text output was fine already and stays so.
	r = runCLI(t, "read-point", "--model", "111", "--point", "AphA", "--url", url)
	if r.err != nil || !strings.Contains(r.stdout, "Raw:     +Inf\n") {
		t.Errorf("read-point text: %v\n%s", r.err, r.stdout)
	}
}

// jsonDocuments returns the number of JSON documents in s, which must hold
// nothing else.
func jsonDocuments(t *testing.T, s string) int {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(s))
	n := 0
	for {
		var v any
		err := dec.Decode(&v)
		if errors.Is(err, io.EOF) {
			return n
		}
		if err != nil {
			t.Errorf("document %d: %v", n+1, err)
			return 0
		}
		n++
	}
}

// A scale factor outside SunSpec's -10..10 is not applied (go-sunspec v0.9.0):
// the point keeps its raw value and has no scaled value, in JSON and in text.
// Before, it overflowed the scaled value to infinity, or to NaN for zero.
func TestHostileScaleFactorOverflow(t *testing.T) {
	common := newModel(t, 1, 0).str("Mn", "Acme")
	inv := newModel(t, 101, 0).
		set("W", 5).set("W_SF", 0x7FFF).
		set("A", 0).set("A_SF", 0x7FFF).
		set("Hz", 5001).set("Hz_SF", 0x8001) // -32767
	url := startDevice(t, testutil.NewSunSpecFixture(40000, 1, common.fixture(), inv.fixture()))

	r := runCLI(t, "read-model", "--id", "101", "--json", "--url", url)
	var model map[string]any
	decodeJSON(t, r, &model)
	pts := jsonPoints(t, model)
	for _, name := range []string{"W", "A", "Hz"} {
		if pts[name]["ScaledValue"] != nil || pts[name]["SFRawValue"] != nil {
			t.Errorf("%s: scale factor applied: %v", name, pts[name])
		}
	}
	if pts["W"]["RawValue"] != float64(5) || pts["Hz"]["RawValue"] != float64(5001) {
		t.Errorf("raw values: W %v, Hz %v", pts["W"]["RawValue"], pts["Hz"]["RawValue"])
	}
	r = runCLI(t, "read-point", "--model", "101", "--point", "W", "--url", url)
	if r.err != nil || !strings.Contains(r.stdout, "Raw:     5\n") || strings.Contains(r.stdout, "Inf") {
		t.Errorf("text: %v\n%s", r.err, r.stdout)
	}
}

// hostileStrings is a Common model whose strings carry terminal control
// sequences: clear screen, set the window title, a fake extra line, invalid
// UTF-8 and a right-to-left override.
func hostileStrings(t *testing.T) *testutil.SunSpecHandler {
	t.Helper()
	common := newModel(t, 1, 0).
		str("Mn", "\x1b[2J\x1b]0;pwn\x07").
		str("Md", "a\tb\nFAKE  line").
		str("Opt", "\xff\xfeok").
		str("Vr", "1\u202e2").
		str("SN", "\x9b31m")
	return testutil.NewSunSpecFixture(40000, 1, common.fixture())
}

// wantTerminalSafe fails when s holds a control character other than a
// newline, or a Unicode format character.
func wantTerminalSafe(t *testing.T, what, s string) {
	t.Helper()
	for _, r := range s {
		if r == '\n' {
			continue
		}
		if r < 0x20 || r == 0x7F || (r >= 0x80 && r <= 0x9F) || r == '\u202e' || r == '\uFFFD' {
			t.Errorf("%s holds %U:\n%q", what, r, s)
			return
		}
	}
}

func TestHostileStringsAreEscapedInText(t *testing.T) {
	url := startDevice(t, hostileStrings(t))

	r := runCLI(t, "read", "--url", url)
	if r.err != nil {
		t.Fatal(r.err)
	}
	wantTerminalSafe(t, "read", r.stdout)
	for _, want := range []string{
		`    Mn   string  \x1b[2J\x1b]0;pwn\x07`,
		`    Md   string  a\tb\nFAKE  line`,
		`    Opt  string  \xff\xfeok`,
		`    Vr   string  1\u202e2`,
		`    SN   string  \x9b31m`,
	} {
		if !strings.Contains(r.stdout, want+"\n") {
			t.Errorf("read lacks %q:\n%s", want, r.stdout)
		}
	}
	if strings.Contains(r.stdout, "\nFAKE") {
		t.Errorf("a device string started a new line:\n%s", r.stdout)
	}

	r = runCLI(t, "read-point", "--model", "1", "--point", "Mn", "--url", url)
	if r.err != nil || r.stdout != "Point:   Mn\nType:    string\nRaw:     \\x1b[2J\\x1b]0;pwn\\x07\n" {
		t.Errorf("read-point: %v\n%q", r.err, r.stdout)
	}
	r = runCLI(t, "poll-point", "--model", "1", "--point", "Md", "--count", "1", "--url", url)
	wantTerminalSafe(t, "poll-point", r.stdout)

	// JSON keeps the strings as they are: encoding/json escapes the control
	// characters itself, and decoding gives back the device's string.
	r = runCLI(t, "read-model", "--id", "1", "--json", "--url", url)
	var model map[string]any
	decodeJSON(t, r, &model)
	pts := jsonPoints(t, model)
	for name, want := range map[string]string{"Mn": "\x1b[2J\x1b]0;pwn\x07", "Md": "a\tb\nFAKE  line", "Vr": "1\u202e2"} {
		if pts[name]["RawValue"] != want {
			t.Errorf("JSON %s = %q, want %q", name, pts[name]["RawValue"], want)
		}
	}
	if strings.ContainsAny(r.stdout, "\x1b\x07\t") {
		t.Errorf("raw control characters in JSON:\n%q", r.stdout)
	}
}

func TestHostileStringsInServeWriteLog(t *testing.T) {
	newRootCmd() // text output: reset --json of earlier tests
	var out bytes.Buffer
	log := writeLogger(&out)
	log(&sunspec.ServerWrite{ClientAddr: "10.0.0.1:5", ModelID: 1, Address: 40002, Points: []sunspec.DecodedPoint{
		{Name: "Mn", RawValue: "\x1b[31mred\r\nwrite from 6.6.6.6: forged", Implemented: true},
	}})
	want := `write from 10.0.0.1:5: model 1 at 40002: Mn = \x1b[31mred\r\nwrite from 6.6.6.6: forged` + "\n"
	if out.String() != want {
		t.Errorf("got  %q\nwant %q", out.String(), want)
	}
}

func TestHostileErrorMessagesAreEscaped(t *testing.T) {
	// A device file names a point with an escape sequence: the error quotes
	// the name, and the terminal must not act on it.
	path := writeFile(t, `{"models": [{"id": 1, "points": {"\u001b]0;pwn\u0007": 1}}]}`)
	r := runCLI(t, "serve", "--file", path, "--check")
	if r.err == nil {
		t.Fatal("accepted")
	}
	if code := exitCode(r.err); code != exitInvalidInput {
		t.Errorf("exit %d, want %d", code, exitInvalidInput)
	}
	wantTerminalSafe(t, "stderr", r.stderr)
	wantTerminalSafe(t, "error", r.err.Error())
	if !strings.Contains(r.stderr, `\x1b]0;pwn\x07`) {
		t.Errorf("stderr:\n%s", r.stderr)
	}
}

// silentDevice accepts connections and reads requests but never answers.
func silentDevice(t *testing.T) string {
	t.Helper()
	return fakeDevice(t, func(c net.Conn) {
		_, _ = io.Copy(io.Discard, c)
	})
}

// fakeDevice serves each connection with handle on a local port and returns
// the URL. Connections are closed when the test ends.
func fakeDevice(t *testing.T, handle func(net.Conn)) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var (
		mu    sync.Mutex
		conns []net.Conn
		wg    sync.WaitGroup
	)
	wg.Go(func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
			wg.Go(func() { handle(c) })
		}
	})
	t.Cleanup(func() {
		_ = l.Close()
		mu.Lock()
		for _, c := range conns {
			_ = c.Close()
		}
		mu.Unlock()
		wg.Wait()
	})
	return "tcp://" + l.Addr().String()
}

// runCancelled runs args against url and cancels the context after delay; it
// fails when the command does not return within limit.
func runCancelled(t *testing.T, delay, limit time.Duration, args ...string) (result, time.Duration) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	time.AfterFunc(delay, cancel)
	done := make(chan result, 1)
	start := time.Now()
	go func() { done <- runCLIContext(t, ctx, args...) }()
	select {
	case r := <-done:
		return r, time.Since(start)
	case <-time.After(limit):
		t.Fatalf("%v did not stop within %s of being interrupted", args, limit)
		return result{}, 0
	}
}

// Every command that talks to a device stops promptly when it is
// interrupted, also while the device keeps it waiting. The poll commands
// discovered with a context of their own, so Ctrl-C during discovery waited
// for --timeout to pass.
func TestHostileSilentDeviceInterrupt(t *testing.T) {
	url := silentDevice(t)
	for _, args := range [][]string{
		{"detect"}, {"models"}, {"read"}, {"read-model", "--id", "1"}, {"read-point", "--model", "1", "--point", "Mn"},
		{"capture"},
		{"poll"}, {"poll-model", "--id", "1"}, {"poll-point", "--model", "1", "--point", "Mn"},
	} {
		t.Run(args[0], func(t *testing.T) {
			r, took := runCancelled(t, 100*time.Millisecond, 5*time.Second, append(args, "--url", url, "--timeout", "1m")...)
			// The poll commands end without error when interrupted (exit 0,
			// docs/exitcodes.md); the others exit 130.
			if strings.HasPrefix(args[0], "poll") {
				if r.err != nil {
					t.Errorf("err = %v after %s, want nil", r.err, took)
				}
			} else if code := exitCode(r.err); r.err == nil || code != exitInterrupted {
				t.Errorf("exit %d (%v) after %s, want %d", code, r.err, took, exitInterrupted)
			}
			if r.stdout != "" && !strings.HasPrefix(r.stdout, "Usage:") {
				t.Errorf("stdout: %q", r.stdout)
			}
		})
	}
}

// A device that never answers ends a command with exit 5 after --timeout.
func TestHostileSilentDeviceTimeout(t *testing.T) {
	url := silentDevice(t)
	start := time.Now()
	r := runCLI(t, "poll", "--count", "1", "--url", url, "--timeout", "300ms")
	if code := exitCode(r.err); code != exitTimeout {
		t.Errorf("exit %d (%v), want %d", code, r.err, exitTimeout)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("took %s", took)
	}
}

// A device that drops the connection in the middle of a response ends the
// command with a transport error.
func TestHostileDeviceDropsConnection(t *testing.T) {
	url := fakeDevice(t, func(c net.Conn) {
		buf := make([]byte, 12)
		if _, err := io.ReadFull(c, buf); err != nil {
			return
		}
		// Half an MBAP header for the right transaction, then gone.
		_, _ = c.Write(buf[:4])
		_ = c.Close()
	})
	for _, args := range [][]string{{"detect"}, {"read"}, {"poll", "--count", "3", "--interval", "1ms"}} {
		start := time.Now()
		r := runCLI(t, append(args, "--url", url, "--timeout", "5s")...)
		if r.err == nil {
			t.Errorf("%v: no error", args)
			continue
		}
		if code := exitCode(r.err); code != exitTransport && code != exitFindings && code != exitProtocol {
			t.Errorf("%v: exit %d (%v)", args, code, r.err)
		}
		if took := time.Since(start); took > 4*time.Second {
			t.Errorf("%v took %s", args, took)
		}
	}
}

// A device that sends a response one byte at a time, slower than --timeout
// allows, is a timeout and not a hang.
func TestHostileTricklingDevice(t *testing.T) {
	url := fakeDevice(t, func(c net.Conn) {
		buf := make([]byte, 12)
		if _, err := io.ReadFull(c, buf); err != nil {
			return
		}
		resp := []byte{buf[0], buf[1], 0, 0, 0, 7, buf[6], 3, 4, 0x53, 0x75, 0x6E, 0x53}
		for _, b := range resp {
			if _, err := c.Write([]byte{b}); err != nil {
				return
			}
			time.Sleep(200 * time.Millisecond)
		}
	})
	start := time.Now()
	r := runCLI(t, "detect", "--url", url, "--timeout", "500ms")
	if code := exitCode(r.err); r.err == nil || code != exitTimeout {
		t.Errorf("exit %d (%v), want %d", code, r.err, exitTimeout)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("took %s", took)
	}
}

// Broken model chains: a header whose length runs past the address space, a
// chain without an end, and a model that claims more registers than the
// device has. Each is a protocol error (exit 6), never a panic or a hang.
func TestHostileModelChains(t *testing.T) {
	suns := map[uint16]uint16{40000: 0x5375, 40001: 0x6E53}
	with := func(extra map[uint16]uint16) *testutil.SunSpecHandler {
		regs := map[uint16]uint16{}
		for k, v := range suns {
			regs[k] = v
		}
		for k, v := range extra {
			regs[k] = v
		}
		return &testutil.SunSpecHandler{UnitID: 1, Registers: regs}
	}
	endless := map[uint16]uint16{}
	for a := uint16(40002); a < 40002+2*400; a += 2 {
		endless[a], endless[a+1] = 64900, 0
	}
	tests := []struct {
		name string
		h    *testutil.SunSpecHandler
	}{
		{"length past 0xFFFF", with(map[uint16]uint16{40002: 1, 40003: 0xFFFF})},
		{"no end marker", with(endless)},
		{"length longer than the device", with(map[uint16]uint16{40002: 1, 40003: 66, 40070: 0xFFFF, 40071: 0})},
		{"model ID 0", with(map[uint16]uint16{40002: 0, 40003: 0})},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			url := startDevice(t, tc.h)
			for _, args := range [][]string{{"models"}, {"read", "--json"}, {"capture"}} {
				start := time.Now()
				r := runCLI(t, append(args, "--url", url, "--timeout", "5s")...)
				if took := time.Since(start); took > 5*time.Second {
					t.Errorf("%v took %s", args, took)
				}
				if r.err == nil {
					// A device whose chain is readable but short is not an
					// error; the output must then still be well formed.
					if args[len(args)-1] == "--json" && !json.Valid([]byte(r.stdout)) {
						t.Errorf("%v: invalid JSON", args)
					}
					continue
				}
				code := exitCode(r.err)
				if code != exitProtocol && code != exitTransport {
					t.Errorf("%v: exit %d (%v)", args, code, r.err)
				}
			}
		})
	}
}

// A bad marker is "not SunSpec": exit 1, or a result for detect.
func TestHostileBadMarker(t *testing.T) {
	url := startDevice(t, &testutil.SunSpecHandler{UnitID: 1, Registers: map[uint16]uint16{
		0: 0x5375, 1: 0x6E54, 40000: 0x0000, 40001: 0x0000, 50000: 0xFFFF, 50001: 0xFFFF,
	}})
	r := runCLI(t, "models", "--url", url)
	if !errors.Is(r.err, sunspec.ErrNotSunSpec) || exitCode(r.err) != exitFindings {
		t.Errorf("models: %v", r.err)
	}
	// detect fails the same way. docs/exitcodes.md says it reports this as
	// a result and exits 0; the code and TestNotSunSpec say exit 1.
	r = runCLI(t, "detect", "--json", "--url", url)
	if !errors.Is(r.err, sunspec.ErrNotSunSpec) || exitCode(r.err) != exitFindings || r.stdout != "" && !strings.HasPrefix(r.stdout, "Usage:") {
		t.Errorf("detect: %v\n%s", r.err, r.stdout)
	}
}
