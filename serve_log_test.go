// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-modbus"
)

// gatedWriter is a standard output whose first write that gate selects waits
// until release is closed; blocked is closed when that write starts waiting.
// The wait ends after ten seconds too, so that a failing test does not hang.
type gatedWriter struct {
	gate    func(p string) bool
	once    sync.Once
	blocked chan struct{}
	release chan struct{}

	mu      sync.Mutex
	buf     strings.Builder
	changed chan struct{} // closed and replaced by every write
}

func newGatedWriter(gate func(p string) bool) *gatedWriter {
	return &gatedWriter{gate: gate, blocked: make(chan struct{}), release: make(chan struct{}), changed: make(chan struct{})}
}

func (g *gatedWriter) Write(p []byte) (int, error) {
	first := false
	if g.gate(string(p)) {
		g.once.Do(func() { first = true })
	}
	if first {
		close(g.blocked)
		select {
		case <-g.release:
		case <-time.After(10 * time.Second):
		}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.buf.Write(p)
	close(g.changed)
	g.changed = make(chan struct{})
	return len(p), nil
}

func (g *gatedWriter) String() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.buf.String()
}

// waitFor returns once the output contains want.
func (g *gatedWriter) waitFor(t *testing.T, want string) {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		g.mu.Lock()
		got, changed := g.buf.String(), g.changed
		g.mu.Unlock()
		if strings.Contains(got, want) {
			return
		}
		select {
		case <-changed:
		case <-timeout:
			t.Fatalf("output never contained %q:\n%s", want, got)
		}
	}
}

// waitClosed returns once ch is closed.
func waitClosed(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatalf("never happened: %s", what)
	}
}

// serveWith runs "serve" with the example device on a free port, with stdout
// as its standard output, and returns at once, without waiting for the
// status. Flags are read when the command starts: the test must not run
// another command while it is serving.
func serveWith(t *testing.T, stdout io.Writer, args ...string) *serving {
	t.Helper()
	s := &serving{
		url:    "tcp://127.0.0.1:" + strconv.Itoa(freePort(t)),
		stderr: &syncBuffer{}, done: make(chan struct{}),
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	root := newRootCmd()
	root.SetOut(stdout)
	root.SetErr(s.stderr)
	root.SetArgs(append([]string{"serve", "--file", writeFile(t, exampleDevice), "--url", s.url}, args...))
	go func() {
		s.err = root.ExecuteContext(ctx)
		close(s.done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-s.done:
		case <-time.After(15 * time.Second):
			t.Error("serve did not stop")
		}
	})
	return s
}

// openClient opens a Modbus client to url, closed when the test ends.
func openClient(t *testing.T, url string) *modbus.Client {
	t.Helper()
	c, err := modbus.New(modbus.Config{URL: url, Timeout: 10 * time.Second, Logger: modbus.NopLogger()})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Open(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// writeInBackground writes values at addr of unit 3 on a connection of its
// own and closes done when the write has been answered; *err is then its
// result.
func writeInBackground(t *testing.T, url string, addr uint16, values []uint16, err *error) (done chan struct{}) {
	t.Helper()
	c := openClient(t, url)
	done = make(chan struct{})
	go func() {
		defer close(done)
		*err = c.WriteRegisters(context.Background(), 3, addr, values)
	}()
	return done
}

// waitForWriteInDevice returns once a client write has reached the served
// device and waits there for a lock, or done is closed (the write has been
// answered).
func waitForWriteInDevice(t *testing.T, done <-chan struct{}) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	buf := make([]byte, 1<<20)
	for {
		select {
		case <-done:
			return
		default:
		}
		n := runtime.Stack(buf, true)
		for g := range strings.SplitSeq(string(buf[:n]), "\n\n") {
			if strings.Contains(g, "sync.(*Mutex).Lock(") && strings.Contains(g, ".HandleHoldingRegisters(") {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the write never reached the device:\n%s", buf[:n])
		}
		runtime.Gosched()
	}
}

// isWriteEvent reports whether p is the write of a write event.
func isWriteEvent(p string) bool {
	return strings.HasPrefix(p, "write from ") || strings.Contains(p, `"Event": "write"`)
}

// writtenWMaxLimPct returns the WMaxLimPct values of the write events in a
// serve output, in order.
func writtenWMaxLimPct(t *testing.T, asJSON bool, out string) []string {
	t.Helper()
	var values []string
	if !asJSON {
		for line := range strings.SplitSeq(out, "\n") {
			if _, v, ok := strings.Cut(line, ": model 123 at 40177: WMaxLimPct = "); ok && strings.HasPrefix(line, "write from ") {
				values = append(values, v)
			}
		}
		return values
	}
	dec := json.NewDecoder(strings.NewReader(out))
	var status servedDevice
	if err := dec.Decode(&status); err != nil {
		t.Fatalf("status: %v\n%s", err, out)
	}
	for dec.More() {
		var ev writeEvent
		if err := dec.Decode(&ev); err != nil {
			t.Fatalf("write event: %v\n%s", err, out)
		}
		if ev.Event != "write" || ev.ModelID != 123 || ev.Address != 40177 || len(ev.Points) != 1 || ev.Points[0].ScaledValue == nil {
			t.Fatalf("write event = %+v", ev)
		}
		values = append(values, strconv.FormatFloat(*ev.Points[0].ScaledValue, 'g', -1, 64))
	}
	return values
}

// Two clients write the same point of one model at the same time while
// standard output is slow. Each write that took effect is printed once, in
// the order the writes took effect, so that the last event is what the device
// holds; a write that did not take effect is not printed.
func TestServeConcurrentWritesToOneModel(t *testing.T) {
	for _, asJSON := range []bool{false, true} {
		t.Run("json="+strconv.FormatBool(asJSON), func(t *testing.T) {
			out := newGatedWriter(isWriteEvent)
			var args []string
			statusEnd := "40198  3\n"
			if asJSON {
				args, statusEnd = []string{"--json"}, "\n}\n"
			}
			s := serveWith(t, out, args...)
			out.waitFor(t, statusEnd)

			// WMaxLimPct of model 123 (scale factor -1) is at 40177. The first
			// write's event waits on standard output; the second write arrives
			// meanwhile.
			var errs [2]error
			first := writeInBackground(t, s.url, 40177, []uint16{600}, &errs[0])
			waitClosed(t, out.blocked, "the first write event")
			second := writeInBackground(t, s.url, 40177, []uint16{700}, &errs[1])
			waitForWriteInDevice(t, second)
			close(out.release)
			waitClosed(t, first, "the first write answered")
			waitClosed(t, second, "the second write answered")

			held, err := openClient(t, s.url).ReadRegister(context.Background(), 3, 40177, modbus.HoldingRegister)
			if err != nil {
				t.Fatal(err)
			}
			printed := writtenWMaxLimPct(t, asJSON, out.String())
			for i, v := range []string{"60", "70"} {
				n := 0
				for _, p := range printed {
					if p == v {
						n++
					}
				}
				switch {
				case errs[i] == nil && n != 1:
					t.Errorf("the write of %s took effect and is printed %d times", v, n)
				case errs[i] != nil && n != 0:
					t.Errorf("the write of %s was answered with %v and is printed", v, errs[i])
				}
			}
			if len(printed) == 0 || printed[len(printed)-1] != strconv.Itoa(int(held)/10) {
				t.Errorf("events %v, the device holds %d (raw)", printed, held)
			}
			if err := s.stop(t); err != nil {
				t.Errorf("serve returned %v", err)
			}
		})
	}
}

// A client write that arrives while the status is being printed is printed
// after the whole status: the text table is not interrupted, and with --json
// the first document is the served device.
func TestServeWriteDuringStatus(t *testing.T) {
	for _, asJSON := range []bool{false, true} {
		t.Run("json="+strconv.FormatBool(asJSON), func(t *testing.T) {
			// Standard output stops inside the status: at the first cell of the
			// Common row, or at the JSON document.
			gate := func(p string) bool { return p == "Common" }
			var args []string
			if asJSON {
				gate = func(p string) bool { return strings.HasPrefix(p, "{\n  \"URL\"") }
				args = []string{"--json"}
			}
			out := newGatedWriter(gate)
			s := serveWith(t, out, args...)
			waitClosed(t, out.blocked, "the status")

			var werr error
			done := writeInBackground(t, s.url, 40176, []uint16{1, 505}, &werr)
			waitForWriteInDevice(t, done)
			close(out.release)
			waitClosed(t, done, "the write answered")
			if werr != nil {
				t.Fatalf("write: %v", werr)
			}

			got := out.String()
			if asJSON {
				dec := json.NewDecoder(strings.NewReader(got))
				var status servedDevice
				var ev writeEvent
				if err := dec.Decode(&status); err != nil || status.URL != s.url || len(status.Models) != 5 {
					t.Errorf("first document: %v, %+v\n%s", err, status, got)
				}
				if err := dec.Decode(&ev); err != nil || ev.Event != "write" || ev.Address != 40176 || dec.More() {
					t.Errorf("second document: %v, %+v\n%s", err, ev, got)
				}
			} else {
				wantStatus := lines(
					"Serving SunSpec device on "+s.url+" (unit ID 3, base address 40000)",
					"ID     NAME                         START  LENGTH",
					"1      Common                       40002  66",
					"103    Inverter (Three Phase)       40070  50",
					"160    Multiple MPPT Inverter Extension Model  40122  48",
					"123    Immediate Controls           40172  24",
					"64900  unknown_64900                40198  3",
				)
				status, event, _ := strings.Cut(got, "write from ")
				if normalizeTable(status) != normalizeTable(wantStatus) ||
					!strings.HasSuffix(event, ": model 123 at 40176: Conn = 1 [CONNECT], WMaxLimPct = 50.5\n") || strings.Count(event, "\n") != 1 {
					t.Errorf("output:\n%s\nwant the status, then the write event", got)
				}
			}
			if err := s.stop(t); err != nil {
				t.Errorf("serve returned %v", err)
			}
		})
	}
}

// serve accepts 10 clients at a time. A connection beyond that is closed at
// once, and the Modbus server's warning about it is printed on stderr.
func TestServeReportsRefusedConnection(t *testing.T) {
	out := newGatedWriter(func(string) bool { return false })
	s := serveWith(t, out)
	out.waitFor(t, "40198  3\n")
	ctx := context.Background()
	for i := range 10 {
		if _, err := openClient(t, s.url).ReadRegister(ctx, 3, 40000, modbus.HoldingRegister); err != nil {
			t.Fatalf("client %d: %v", i+1, err)
		}
	}
	if _, err := openClient(t, s.url).ReadRegister(ctx, 3, 40000, modbus.HoldingRegister); err == nil {
		t.Fatal("an eleventh client was served")
	}
	if err := s.stop(t); err != nil {
		t.Errorf("serve returned %v", err)
	}
	got := strings.Split(s.stderr.String(), "\n")
	if len(got) != 3 || !strings.HasPrefix(got[0], "modbus-server(127.0.0.1:") ||
		!strings.Contains(got[0], "[warn]: max. number of concurrent connections reached, rejecting 127.0.0.1:") ||
		got[1] != "shutting down" || got[2] != "" {
		t.Errorf("stderr = %q, want the refused connection, then shutting down", s.stderr.String())
	}
}

// failingEventWriter is a standard output whose write of a write event does
// not return normally.
type failingEventWriter struct{ *gatedWriter }

func (w failingEventWriter) Write(p []byte) (int, error) {
	if strings.HasPrefix(string(p), "write from ") {
		panic("standard output failed")
	}
	return w.gatedWriter.Write(p)
}

// Code the Modbus server calls that does not return normally (here the write
// log, on a standard output that fails that way) is reported on stderr, once,
// on one line with its stack, and the request is answered with exception 4.
func TestServeReportsHandlerFailure(t *testing.T) {
	out := failingEventWriter{newGatedWriter(func(string) bool { return false })}
	s := serveWith(t, out)
	out.waitFor(t, "40198  3\n")
	err := openClient(t, s.url).WriteRegisters(context.Background(), 3, 40176, []uint16{1, 505})
	if !errors.Is(err, modbus.ErrServerDeviceFailure) {
		t.Errorf("write: %v, want server device failure", err)
	}
	if err := s.stop(t); err != nil {
		t.Errorf("serve returned %v", err)
	}
	got := strings.Split(s.stderr.String(), "\n")
	if len(got) != 3 || !strings.Contains(got[0], "[error]: ") || !strings.Contains(got[0], " for FC 0x10 from 127.0.0.1:") ||
		!strings.Contains(got[0], ": standard output failed\\ngoroutine ") || got[1] != "shutting down" {
		t.Errorf("stderr = %q, want one line for the handler, then shutting down", s.stderr.String())
	}
}

// The go-modbus logger of serve prints warnings and errors, escaped, one line
// each, and drops debug and info entries, which go-modbus writes per frame.
func TestServeLogger(t *testing.T) {
	var buf strings.Builder
	l := &serveLogger{w: &buf}
	l.Debugf("RX: % X", []byte{1, 2})
	l.Infof("info %d", 1)
	l.Warnf("TLS handshake with %s failed: %v", "127.0.0.1:5", errors.New("name \x1b[31mred"))
	l.Errorf("handler: %v\n%s", "value", "goroutine 1 [running]:\n\tmain.go:1")
	want := `TLS handshake with 127.0.0.1:5 failed: name \x1b[31mred` + "\n" +
		`handler: value\ngoroutine 1 [running]:\n\tmain.go:1` + "\n"
	if got := buf.String(); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}
