// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/otfabric/go-modbus"
	"github.com/otfabric/go-sunspec"
	"github.com/otfabric/go-sunspec/devicefile"
	"github.com/otfabric/go-sunspec/testutil"
)

// exampleDevice is a hand-written device file using every way to give a
// value: engineering values with scale factors, symbol names, bitfield names,
// null, raw registers for a point, repeating groups and a vendor model.
const exampleDevice = `{
  "unit_id": 3,
  "base_address": 40000,
  "models": [
    {"id": 1, "name": "ignored", "points": {"Mn": "Acme", "Md": "PV-5000", "SN": "A-1", "DA": 3}},
    {"id": 103, "points": {
      "A": 12.34, "A_SF": -2,
      "PhVphA": 230.1, "V_SF": -1,
      "W": 5000, "W_SF": 1,
      "Hz": null,
      "WH": 123456789, "WH_SF": 0,
      "St": "MPPT",
      "Evt1": ["GROUND_FAULT", "OVER_TEMP"],
      "TmpCab": [65036], "Tmp_SF": -1
    }},
    {"id": 160,
      "points": {"DCA_SF": -1, "DCV_SF": 0, "DCW_SF": 0, "N": 2},
      "groups": {"module": [
        {"points": {"ID": 1, "IDStr": "east", "DCA": 8.5, "DCV": 300, "DCW": 2550}},
        {"points": {"ID": 2, "IDStr": "west", "DCA": 7.5, "DCV": 310}}
      ]}},
    {"id": 123, "points": {"WMaxLimPct": 100, "WMaxLimPct_SF": -1, "Conn": "CONNECT"}},
    {"id": 64900, "registers": [1, 2, 65535]}
  ]
}`

// writeFile stores content in a temporary file and returns its path.
func writeFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "device.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// syncBuffer is a bytes.Buffer that the command and the test may use at the
// same time.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// waitFor polls until the buffer contains want.
func (b *syncBuffer) waitFor(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(b.String(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("output never contained %q:\n%s", want, b.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// serving is a serve command running in the background.
type serving struct {
	url            string
	stdout, stderr *syncBuffer
	cancel         context.CancelFunc
	// done is closed when the command has returned; err is then its error.
	done chan struct{}
	err  error
}

// stop ends the command the way a signal does and returns its error.
func (s *serving) stop(t *testing.T) error {
	t.Helper()
	s.cancel()
	select {
	case <-s.done:
		return s.err
	case <-time.After(15 * time.Second):
		t.Fatal("serve did not stop")
		return nil
	}
}

// startServe runs "serve --file path" with extra arguments on a free port and
// waits until it reports that it is serving.
func startServe(t *testing.T, path string, args ...string) *serving {
	t.Helper()
	s := &serving{
		url:    "tcp://127.0.0.1:" + strconv.Itoa(freePort(t)),
		stdout: &syncBuffer{}, stderr: &syncBuffer{}, done: make(chan struct{}),
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	root := newRootCmd()
	root.SetOut(s.stdout)
	root.SetErr(s.stderr)
	root.SetArgs(append([]string{"serve", "--file", path, "--url", s.url}, args...))
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

	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(s.stdout.String(), "LENGTH") && !strings.Contains(s.stdout.String(), "\n}\n") {
		select {
		case <-s.done:
			t.Fatalf("serve ended early: %v\nstderr: %s", s.err, s.stderr.String())
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("serve never reported that it is serving:\n%s", s.stdout.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
	return s
}

func TestServeExampleDevice(t *testing.T) {
	s := startServe(t, writeFile(t, exampleDevice))
	wantStatus := lines(
		"Serving SunSpec device on "+s.url+" (unit ID 3, base address 40000)",
		"ID     NAME                         START  LENGTH",
		"1      Common                       40002  66",
		"103    Inverter (Three Phase)       40070  50",
		"160    Multiple MPPT Inverter Extension Model  40122  48",
		"123    Immediate Controls           40172  24",
		"64900  unknown_64900                40198  3",
	)
	if got := s.stdout.String(); normalizeTable(got) != normalizeTable(wantStatus) {
		t.Errorf("status:\n%s\nwant:\n%s", got, wantStatus)
	}

	// The other commands read it like any device.
	r := runCLI(t, "models", "--url", s.url, "--unit-id", "3")
	if r.err != nil || !strings.Contains(r.stdout, "64900  unknown_64900") || !strings.Contains(r.stdout, "103    Inverter (Three Phase)") {
		t.Errorf("models: %v\n%s", r.err, r.stdout)
	}
	r = runCLI(t, "read", "--json", "--url", s.url, "--unit-id", "3")
	if r.err != nil || r.stderr != "" {
		t.Fatalf("read: %v, stderr %q", r.err, r.stderr)
	}
	var models []*sunspec.DecodedModel
	decodeJSON(t, r, &models)
	if len(models) != 5 {
		t.Fatalf("%d models read", len(models))
	}
	for _, dm := range models[:4] {
		if len(dm.Warnings) != 0 {
			t.Errorf("model %d: warnings %v", dm.ModelID, dm.Warnings)
		}
	}
	common, inv, mppt := models[0].Group, models[1].Group, models[2].Group
	if p := point(t, common, "Mn"); p.RawValue != "Acme" {
		t.Errorf("Mn = %v", p.RawValue)
	}
	if p := point(t, common, "Vr"); p.Implemented {
		t.Errorf("Vr = %+v, want not implemented", p)
	}
	scaled := func(g *sunspec.DecodedGroup, name string, want float64) {
		t.Helper()
		if p := point(t, g, name); p.ScaledValue == nil || *p.ScaledValue != want {
			t.Errorf("%s = %+v, want %v", name, p, want)
		}
	}
	scaled(inv, "A", 12.34)
	scaled(inv, "PhVphA", 230.1)
	scaled(inv, "W", 5000)
	scaled(inv, "WH", 123456789)
	scaled(inv, "TmpCab", -50)
	if p := point(t, inv, "W"); p.RawValue != float64(500) {
		t.Errorf("W raw = %v", p.RawValue)
	}
	if p := point(t, inv, "Hz"); p.Implemented {
		t.Errorf("Hz = %+v, want not implemented", p)
	}
	if p := point(t, inv, "St"); !reflect.DeepEqual(p.Symbols, []string{"MPPT"}) {
		t.Errorf("St = %+v", p)
	}
	if p := point(t, inv, "Evt1"); !reflect.DeepEqual(p.Symbols, []string{"GROUND_FAULT", "OVER_TEMP"}) {
		t.Errorf("Evt1 = %+v", p)
	}
	modules := mppt.GroupsNamed("module")
	if len(modules) != 2 {
		t.Fatalf("%d MPPT modules", len(modules))
	}
	scaled(modules[0], "DCA", 8.5)
	scaled(modules[1], "DCV", 310)
	if p := point(t, modules[1], "IDStr"); p.RawValue != "west" {
		t.Errorf("IDStr = %v", p.RawValue)
	}
	if p := point(t, modules[1], "DCW"); p.Implemented {
		t.Errorf("module 2 DCW = %+v, want not implemented", p)
	}
	if !reflect.DeepEqual(models[4].RawRegisters, []uint16{1, 2, 65535}) || models[4].Group != nil {
		t.Errorf("vendor model = %+v", models[4])
	}

	r = runCLI(t, "read-point", "--model", "123", "--point", "WMaxLimPct", "--url", s.url, "--unit-id", "3")
	wantOK(t, r, lines("Point:   WMaxLimPct", "Type:    uint16", "Raw:     1000", "Scaled:  100", "Units:   % WMax"))
	// Another unit ID is not this device.
	if r := runCLI(t, "detect", "--url", s.url); r.err == nil {
		t.Errorf("detect with unit 1 succeeded:\n%s", r.stdout)
	}

	if err := s.stop(t); err != nil {
		t.Errorf("serve returned %v", err)
	}
	if got := s.stderr.String(); got != "shutting down\n" {
		t.Errorf("stderr = %q", got)
	}
	// The port is free again.
	if r := runCLI(t, "detect", "--url", s.url, "--timeout", "1s"); r.err == nil {
		t.Error("the device still answers after serve stopped")
	}
}

// normalizeTable collapses runs of spaces, so that a table can be compared
// without depending on its column widths.
func normalizeTable(s string) string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		out = append(out, strings.Join(strings.Fields(line), " "))
	}
	return strings.Join(out, "\n")
}

func TestServeJSONAndOverrides(t *testing.T) {
	path := writeFile(t, exampleDevice)
	s := startServe(t, path, "--json", "--unit-id", "9", "--base-address", "50000")
	var status servedDevice
	if err := json.Unmarshal([]byte(s.stdout.String()), &status); err != nil {
		t.Fatalf("status is not JSON: %v\n%s", err, s.stdout.String())
	}
	if status.URL != s.url || status.UnitID != 9 || status.BaseAddress != 50000 || len(status.Models) != 5 {
		t.Errorf("status = %+v", status)
	}
	if m := status.Models[1]; m != (servedModel{ID: 103, Name: "Inverter (Three Phase)", StartAddress: 50070, Length: 50}) {
		t.Errorf("status model = %+v", m)
	}
	r := runCLI(t, "detect", "--url", s.url, "--unit-id", "9")
	if r.err != nil || !strings.Contains(r.stdout, "Base Address: 50000\n") {
		t.Errorf("detect: %v\n%s", r.err, r.stdout)
	}
	if err := s.stop(t); err != nil {
		t.Errorf("serve returned %v", err)
	}
}

func TestServeCheck(t *testing.T) {
	path := writeFile(t, exampleDevice)
	r := runCLI(t, "serve", "--file", path, "--check", "--url", "tcp://127.0.0.1:1")
	if r.err != nil || r.stderr != "" {
		t.Fatalf("serve --check: %v, stderr %q", r.err, r.stderr)
	}
	if !strings.HasPrefix(r.stdout, "Valid SunSpec device on tcp://127.0.0.1:1 (unit ID 3, base address 40000)\n") ||
		!strings.Contains(normalizeTable(r.stdout), "123 Immediate Controls 40172 24\n") {
		t.Errorf("serve --check:\n%s", r.stdout)
	}
	r = runCLI(t, "serve", "-f", path, "--check", "--json")
	var status servedDevice
	decodeJSON(t, r, &status)
	if status.UnitID != 3 || len(status.Models) != 5 || status.Models[4].Name != "unknown_64900" {
		t.Errorf("serve --check --json = %+v", status)
	}

	// A file with only the models: unit 1 at 40000. No models at all is a
	// valid, empty device.
	r = runCLI(t, "serve", "--check", "--file", writeFile(t, `{"models": [{"id": 1}]}`))
	if r.err != nil || !strings.Contains(r.stdout, "(unit ID 1, base address 40000)") {
		t.Errorf("minimal file: %v\n%s", r.err, r.stdout)
	}
	r = runCLI(t, "serve", "--check", "--json", "--file", writeFile(t, `{}`))
	decodeJSON(t, r, &status)
	if r.err != nil || len(status.Models) != 0 || !strings.Contains(r.stdout, `"Models": []`) {
		t.Errorf("empty file: %v\n%s", r.err, r.stdout)
	}
}

// TestReadmeDeviceFile keeps the device file shown in the README valid, and
// the layout printed next to it true.
func TestReadmeDeviceFile(t *testing.T) {
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(readme)
	start := strings.Index(text, "```json\n")
	if start < 0 {
		t.Fatal("README.md has no JSON example")
	}
	example := text[start+len("```json\n"):]
	example = example[:strings.Index(example, "```")]
	r := runCLI(t, "serve", "--check", "--file", writeFile(t, example), "--url", "tcp://127.0.0.1:1502")
	if r.err != nil {
		t.Fatalf("the README's device file is not valid: %v", r.err)
	}
	// The console example in the README shows this device being served.
	for _, line := range strings.Split(strings.TrimSpace(r.stdout), "\n")[1:] {
		if !strings.Contains(text, line+"\n") {
			t.Errorf("README.md does not show the layout line %q", line)
		}
	}
	if !strings.Contains(text, "Serving SunSpec device on tcp://127.0.0.1:1502 (unit ID 1, base address 40000)\n") {
		t.Error("README.md does not show the status line of the example")
	}
}

func TestServeClientWrites(t *testing.T) {
	path := writeFile(t, exampleDevice)
	for _, asJSON := range []bool{false, true} {
		var args []string
		if asJSON {
			args = append(args, "--json")
		}
		s := startServe(t, path, args...)
		client, err := modbus.New(modbus.Config{URL: s.url, Timeout: 2 * time.Second, Logger: modbus.NopLogger()})
		if err != nil {
			t.Fatal(err)
		}
		if err := client.Open(); err != nil {
			t.Fatal(err)
		}
		ctx := context.Background()
		// Model 123 starts at 40172: WMaxLimPct is its fourth data register,
		// Conn the third.
		if err := client.WriteRegisters(ctx, 3, 40176, []uint16{1, 505}); err != nil {
			t.Fatalf("write: %v", err)
		}
		// A read-only point (the scale factor) and the model's ID are
		// refused.
		for _, addr := range []uint16{40195, 40172} {
			if err := client.WriteRegister(ctx, 3, addr, 0); !errors.Is(err, modbus.ErrIllegalDataAddress) {
				t.Errorf("write to %d: %v, want illegal data address", addr, err)
			}
		}
		_ = client.Close()

		if asJSON {
			s.stdout.waitFor(t, `"Event": "write"`)
			text := s.stdout.String()
			var ev writeEvent
			if err := json.Unmarshal([]byte(text[strings.Index(text, "\n}\n")+3:]), &ev); err != nil {
				t.Fatalf("write event is not JSON: %v\n%s", err, text)
			}
			if ev.Event != "write" || ev.ModelID != 123 || ev.Address != 40176 || !reflect.DeepEqual(ev.Registers, []uint16{1, 505}) ||
				len(ev.Points) != 2 || ev.Points[1].Name != "WMaxLimPct" || *ev.Points[1].ScaledValue != 50.5 || ev.Client == "" {
				t.Errorf("write event = %+v", ev)
			}
		} else {
			s.stdout.waitFor(t, "write from ")
			text := s.stdout.String()
			line := text[strings.Index(text, "write from "):]
			if !strings.HasSuffix(line, ": model 123 at 40176: Conn = 1 [CONNECT], WMaxLimPct = 50.5\n") {
				t.Errorf("write line = %q", line)
			}
		}

		r := runCLI(t, "read-point", "--model", "123", "--point", "WMaxLimPct", "--url", s.url, "--unit-id", "3")
		if r.err != nil || !strings.Contains(r.stdout, "Scaled:  50.5\n") {
			t.Errorf("after the write: %v\n%s", r.err, r.stdout)
		}
		if err := s.stop(t); err != nil {
			t.Errorf("serve returned %v", err)
		}
	}
}

func TestWriteLoggerFormats(t *testing.T) {
	var out bytes.Buffer
	log := writeLogger(&out)
	half := 0.5
	log(&sunspec.ServerWrite{ClientAddr: "10.0.0.1", ModelID: 5, Address: 100, Points: []sunspec.DecodedPoint{
		{Name: "A", RawValue: uint16(5), ScaledValue: &half, Implemented: true},
		{Name: "B", RawValue: uint16(0xFFFF)},
		{Name: "C", RawValue: "text", Implemented: true},
		{Name: "D", RawValue: uint16(3), Implemented: true, Symbols: []string{"X", "Y"}},
	}})
	want := "write from 10.0.0.1: model 5 at 100: A = 0.5, B = not implemented, C = text, D = 3 [X, Y]\n"
	if out.String() != want {
		t.Errorf("got  %q\nwant %q", out.String(), want)
	}
}

func TestServeFileErrors(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{"syntax error", "{\n  \"models\": [\n    {\"id\": 1,}\n  ]\n}", []string{"line 3, column 14", "invalid character"}},
		{"not JSON", "hello", []string{"line 1, column 1"}},
		{"empty file", "", []string{"the file ends before the JSON document does"}},
		{"truncated", `{"models": [{"id": 1}`, []string{"the file ends before the JSON document does"}},
		{"trailing data", `{"models": []} {}`, []string{"data after the end of the JSON document"}},
		{"unknown member", `{"models": [{"id": 1, "point": {}}]}`, []string{`unknown field "point"`}},
		{"unknown top-level member", `{"unit": 1}`, []string{`unknown field "unit"`}},
		{"wrong type", "{\n\"unit_id\": \"one\"}", []string{"line 2, column", "unit_id"}},
		{"unit ID too large", `{"unit_id": 300}`, []string{"unit_id"}},
		{"unit ID zero", `{"unit_id": 0, "models": []}`, []string{"unit ID 0 is the Modbus broadcast address"}},
		{"register out of range", `{"models": [{"id": 64900, "registers": [70000]}]}`, []string{"registers", "uint16"}},
		{"missing id", `{"models": [{"id": 1}, {"points": {}}]}`, []string{`models[1]: "id" is missing`}},
		{"registers and points", `{"models": [{"id": 1, "registers": [1], "points": {"Mn": "x"}}]}`,
			[]string{`models[0] (model 1): give either "registers" or "points" and "groups", not both`}},
		{"registers and groups", `{"models": [{"id": 160, "registers": [1], "groups": {"module": []}}]}`, []string{"not both"}},
		{"unknown model", `{"models": [{"id": 1}, {"id": 64900, "points": {"A": 1}}]}`,
			[]string{`models[1] (model 64900): no schema is known for this model: give its data as "registers"`}},
		{"empty registers", `{"models": [{"id": 64900, "registers": []}]}`, []string{"64900", "data registers"}},
		{"unknown point", `{"models": [{"id": 1}, {"id": 103, "points": {"Watts": 1}}]}`,
			[]string{"model 1 of the chain", "model 103", "point Watts", "no point of that name"}},
		{"unknown group", `{"models": [{"id": 160, "groups": {"modules": [{}]}}]}`, []string{"model 160", `no nested group "modules"`}},
		{"point in a nested group", `{"models": [{"id": 160, "groups": {"module": [{}, {"points": {"DCW": true}}]}}]}`,
			[]string{"models[0] (model 160): group module[2]: point DCW: unsupported value true"}},
		{"value out of range", `{"models": [{"id": 103, "points": {"W": 400000, "W_SF": 0}}]}`,
			[]string{"model 103", "point W", "type int16", "value out of range"}},
		{"scaled value out of range", `{"models": [{"id": 103, "points": {"A": 700, "A_SF": -2}}]}`,
			[]string{"model 103", "point A", "70000 does not fit"}},
		{"nested value out of range", `{"models": [{"id": 160, "groups": {"module": [{"points": {"DCW": -1}}]}}]}`,
			[]string{"model 160", "module[1]", "point DCW", "negative"}},
		{"unknown symbol", `{"models": [{"id": 103, "points": {"St": "RUNNING"}}]}`, []string{"point St", `no symbol "RUNNING"`}},
		{"string too long", `{"models": [{"id": 1, "points": {"Vr": "0123456789abcdefg"}}]}`, []string{"point Vr", "17 bytes"}},
		{"number for a string", `{"models": [{"id": 1, "points": {"Mn": 5}}]}`, []string{"point Mn", "string"}},
		{"object value", `{"models": [{"id": 1, "points": {"Mn": {"a": 1}}}]}`, []string{"models[0] (model 1): point Mn: unsupported value", "not an object"}},
		{"mixed list", `{"models": [{"id": 103, "points": {"Evt1": ["GROUND_FAULT", 1]}}]}`, []string{"point Evt1", "not both"}},
		{"list of lists", `{"models": [{"id": 103, "points": {"Evt1": [[1]]}}]}`, []string{"point Evt1", "a list must hold"}},
		{"raw register out of range", `{"models": [{"id": 103, "points": {"W": [70000]}}]}`, []string{"point W", "register value 70000"}},
		{"raw register fraction", `{"models": [{"id": 103, "points": {"W": [1.5]}}]}`, []string{"point W", "register value 1.5"}},
		{"too many raw registers", `{"models": [{"id": 103, "points": {"W": [1, 2]}}]}`, []string{"point W", "2 raw registers"}},
		{"duplicate point", `{"models": [{"id": 103, "points": {"W": 1, "W": 2}}]}`, []string{"point W is given twice"}},
		{"points not an object", `{"models": [{"id": 103, "points": [1]}]}`, []string{`"points" must be an object`}},
		{"too many instances", `{"models": [{"id": 705, "points": {"NCrv": 1}, "groups": {"Crv": [{}, {}]}}]}`,
			[]string{"model 705", "point NCrv", "count is 1 but 2 instances are given"}},
		{"header given", `{"models": [{"id": 1, "points": {"ID": 1}}]}`, []string{"point ID", "set by the encoder"}},
		{"does not fit the address space", `{"base_address": 65500, "models": [{"id": 1}]}`, []string{"base address 65500", "invalid model layout"}},
	}
	for _, tc := range tests {
		path := writeFile(t, tc.content)
		for _, args := range [][]string{{"serve", "--file", path, "--check"}, {"serve", "--file", path, "--url", "tcp://127.0.0.1:1"}} {
			r := runCLI(t, args...)
			if r.err == nil {
				t.Errorf("%s: %v succeeded:\n%s", tc.name, args[len(args)-1], r.stdout)
				continue
			}
			wantNoCommandOutput(t, r)
			msg := r.err.Error()
			if !strings.HasPrefix(msg, "device file") || !strings.Contains(msg, path) {
				t.Errorf("%s: error does not name the file: %s", tc.name, msg)
			}
			for _, want := range tc.want {
				if !strings.Contains(msg, want) {
					t.Errorf("%s: error %q lacks %q", tc.name, msg, want)
				}
			}
		}
	}

	r := runCLI(t, "serve", "--file", filepath.Join(t.TempDir(), "missing.json"))
	if r.err == nil || !strings.HasPrefix(r.err.Error(), "device file: open ") {
		t.Errorf("missing file: %v", r.err)
	}
	wantFail(t, runCLI(t, "serve"), `required flag(s) "file" not set`)
	wantFail(t, runCLI(t, "serve", "--file", "x", "extra"), `unknown command "extra" for "sunspecctl serve"`)
}

func TestServeListenErrors(t *testing.T) {
	path := writeFile(t, exampleDevice)
	// A transport go-modbus has no server for.
	r := runCLI(t, "serve", "--file", path, "--url", "rtu:///dev/null")
	if r.err == nil || !strings.HasPrefix(r.err.Error(), "serve: ") {
		t.Errorf("rtu URL: %v", r.err)
	}
	wantNoCommandOutput(t, r)
	// TLS needs certificates this command cannot be given.
	r = runCLI(t, "serve", "--file", path, "--url", "tcp+tls://127.0.0.1:1")
	if r.err == nil || !strings.HasPrefix(r.err.Error(), "serve: ") {
		t.Errorf("tls URL: %v", r.err)
	}
	// An address that is taken.
	s := startServe(t, path)
	r = runCLI(t, "serve", "--file", path, "--url", s.url)
	if r.err == nil || !strings.HasPrefix(r.err.Error(), "serve: listen on "+s.url+": ") {
		t.Errorf("address in use: %v", r.err)
	}
	wantNoCommandOutput(t, r)
	if err := s.stop(t); err != nil {
		t.Errorf("serve returned %v", err)
	}
}

// failingWriter fails every write.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestServeJSONStatusWriteError(t *testing.T) {
	root := newRootCmd()
	root.SetOut(failingWriter{})
	root.SetErr(&bytes.Buffer{})
	url := "tcp://127.0.0.1:" + strconv.Itoa(freePort(t))
	root.SetArgs([]string{"serve", "--json", "--file", writeFile(t, exampleDevice), "--url", url})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Errorf("err = %v", err)
	}
	// The listener was released.
	if r := runCLI(t, "detect", "--url", url, "--timeout", "1s"); r.err == nil {
		t.Error("the device is still listening")
	}
}

// readAll returns what "read --json" prints for a device, decoded and as
// text.
func readAll(t *testing.T, url string, args ...string) ([]*sunspec.DecodedModel, string) {
	t.Helper()
	r := runCLI(t, append([]string{"read", "--json", "--url", url}, args...)...)
	if r.err != nil {
		t.Fatalf("read %s: %v", url, r.err)
	}
	// A device with a model that does not fit its schema reads with a
	// warning on stderr; the models are all there.
	var models []*sunspec.DecodedModel
	if err := json.Unmarshal([]byte(r.stdout), &models); err != nil {
		t.Fatalf("read %s: stdout is not JSON: %v", url, err)
	}
	return models, r.stdout
}

// decodedOnly returns the JSON of what was decoded from each model: its
// identity, groups and warnings, without the registers behind them.
func decodedOnly(t *testing.T, models []*sunspec.DecodedModel) string {
	t.Helper()
	type view struct {
		ModelID  uint16
		Name     string
		Address  uint16
		Group    *sunspec.DecodedGroup
		Warnings []string
		Raw      []uint16
	}
	var out []view
	for _, dm := range models {
		v := view{ModelID: dm.ModelID, Name: dm.Name, Address: dm.InstanceAddress, Group: dm.Group, Warnings: dm.Warnings}
		if dm.Group == nil {
			// Nothing was decoded: the registers are the values.
			v.Raw = dm.RawRegisters
		}
		out = append(out, v)
	}
	text, err := json.MarshalIndent(out, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	return string(text)
}

// oddDevice has what a capture must fall back to raw registers for: a Common
// model that is one register shorter than its schema, an inverter with
// registers the schema does not know, a vendor model, and a model whose
// strings are padded with spaces instead of NULs.
func oddDevice(t *testing.T) *testutil.SunSpecHandler {
	t.Helper()
	short := newModel(t, 1, 0).str("Mn", "Acme").str("Md", "Old").fixture()
	short.Length, short.Registers = 65, short.Registers[:65]
	long := newModel(t, 101, 0).set("A", 1234).set("A_SF", neg2).set("W", 5000).set("W_SF", 0).fixture()
	long.Length, long.Registers = 53, append(long.Registers, 7, 8, 9)
	padded := newModel(t, 1, 0).fixture()
	copy(padded.Registers, testutil.StringToRegisters("Spacey          ", 16))
	return testutil.NewSunSpecFixture(0, 1, short, long,
		testutil.FixtureModel{ID: 64900, Length: 4, Registers: []uint16{0xDEAD, 0xBEEF, 0, 1}},
		padded,
		newModel(t, 160, 2).set("DCA_SF", neg1).set("N", 2).rep(0, "ID", 1).rep(0, "DCA", 85).rep(1, "ID", 2).fixture(),
	)
}

func TestCaptureServeReproducesTheDevice(t *testing.T) {
	devices := map[string]*testutil.SunSpecHandler{
		"inverter": inverter(t),
		"der":      derDevice(t),
		"odd":      oddDevice(t),
	}
	for name, handler := range devices {
		for _, raw := range []bool{false, true} {
			original := startDevice(t, handler)
			want, wantText := readAll(t, original)

			args := []string{"capture", "--url", original}
			if raw {
				args = append(args, "--raw")
			}
			r := runCLI(t, args...)
			if r.err != nil || r.stderr != "" {
				t.Fatalf("%s: capture: %v, stderr %q", name, r.err, r.stderr)
			}
			var file devicefile.File
			if err := json.Unmarshal([]byte(r.stdout), &file); err != nil {
				t.Fatalf("%s: capture output is not a device file: %v", name, err)
			}
			if len(file.Models) != len(want) {
				t.Fatalf("%s: %d models captured, %d read", name, len(file.Models), len(want))
			}
			for i, m := range file.Models {
				if raw && m.Registers == nil {
					t.Errorf("%s: capture --raw wrote model %d (ID %d) as points", name, i, *m.ID)
				}
			}

			s := startServe(t, writeFile(t, r.stdout))
			got, gotText := readAll(t, s.url)
			if a, b := decodedOnly(t, want), decodedOnly(t, got); a != b {
				t.Errorf("%s (raw %v): the served capture decodes differently\noriginal:\n%s\nserved:\n%s", name, raw, a, b)
			}
			// Raw registers reproduce the device to the bit.
			if raw && gotText != wantText {
				t.Errorf("%s: capture --raw does not reproduce the registers", name)
			}
			// Capturing the copy gives the same file: the format is stable.
			again := runCLI(t, append(args[:1:1], append([]string{"--url", s.url}, args[3:]...)...)...)
			if again.err != nil || again.stdout != r.stdout {
				t.Errorf("%s (raw %v): capturing the served copy gives another file (err %v)\nfirst:\n%s\nsecond:\n%s",
					name, raw, again.err, r.stdout, again.stdout)
			}
			if err := s.stop(t); err != nil {
				t.Errorf("serve returned %v", err)
			}
		}
	}
}

func TestCaptureFileContents(t *testing.T) {
	url := startDevice(t, oddDevice(t))
	out := filepath.Join(t.TempDir(), "captured.json")
	r := runCLI(t, "capture", "--url", url, "--output", out)
	if r.err != nil || r.stdout != "" {
		t.Fatalf("capture -o: %v, stdout %q", r.err, r.stdout)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	file, err := devicefile.Parse(data)
	if err != nil {
		t.Fatalf("the captured file does not parse: %v\n%s", err, data)
	}
	if *file.UnitID != 1 || *file.BaseAddress != 0 || len(file.Models) != 5 {
		t.Fatalf("file = %+v", file)
	}
	// Short, over-long and vendor models are raw; the others are points.
	for i, wantRaw := range []bool{true, true, true, false, false} {
		if m := file.Models[i]; (m.Registers != nil) != wantRaw {
			t.Errorf("model %d (ID %d): raw = %v, want %v", i, *m.ID, m.Registers != nil, wantRaw)
		}
	}
	if m := file.Models[0]; len(m.Registers) != 65 || m.Name != "Common" {
		t.Errorf("short model: %d registers, name %q", len(m.Registers), m.Name)
	}
	if m := file.Models[1]; len(m.Registers) != 53 || m.Registers[52] != 9 {
		t.Errorf("long model: %d registers", len(m.Registers))
	}
	if m := file.Models[2]; !reflect.DeepEqual(m.Registers, []uint16{0xDEAD, 0xBEEF, 0, 1}) || m.Name != "unknown_64900" {
		t.Errorf("vendor model = %+v", m)
	}
	// Points are listed in schema order, with engineering values.
	text := string(data)
	mppt := text[strings.Index(text, `"id": 160`):]
	if !strings.Contains(mppt, `"DCA_SF": -1`) || !strings.Contains(mppt, `"DCA": 8.5`) ||
		strings.Index(mppt, `"DCA_SF"`) > strings.Index(mppt, `"N"`) {
		t.Errorf("MPPT model:\n%s", mppt)
	}
	if !strings.Contains(text, `"Mn": "Spacey"`) {
		t.Errorf("space-padded string was not captured as a value:\n%s", text)
	}

	// A file that cannot be written.
	r = runCLI(t, "capture", "--url", url, "-o", filepath.Join(t.TempDir(), "no", "such", "dir", "x.json"))
	if r.err == nil || !strings.HasPrefix(r.err.Error(), "write device file: ") {
		t.Errorf("unwritable output: %v", r.err)
	}
}

func TestCaptureErrors(t *testing.T) {
	// A model that cannot be read completely is not captured in part.
	r := runCLI(t, "capture", "--url", startDevice(t, brokenInverter(t)))
	if r.err == nil || !strings.HasPrefix(r.err.Error(), "read model 64950: sunspec: partial read") {
		t.Errorf("partial read: %v", r.err)
	}
	wantNoCommandOutput(t, r)
	r = runCLI(t, "capture", "--url", startDevice(t, &testutil.SunSpecHandler{UnitID: 1, Registers: map[uint16]uint16{0: 1, 1: 2}}))
	if r.err == nil || !strings.HasPrefix(r.err.Error(), "discover: ") {
		t.Errorf("no SunSpec device: %v", r.err)
	}
	r = runCLI(t, "capture", "--url", "tcp://127.0.0.1:"+strconv.Itoa(freePort(t)), "--timeout", "1s")
	if r.err == nil || !strings.HasPrefix(r.err.Error(), "connect: ") {
		t.Errorf("connection refused: %v", r.err)
	}
	wantFail(t, runCLI(t, "capture", "extra"), `unknown command "extra" for "sunspecctl capture"`)
}

func TestTemplate(t *testing.T) {
	r := runCLI(t, "template", "--model", "1,160", "--model", "705")
	if r.err != nil || r.stderr != "" {
		t.Fatalf("template: %v, stderr %q", r.err, r.stderr)
	}
	file, err := devicefile.Parse([]byte(r.stdout))
	if err != nil {
		t.Fatalf("the template does not parse: %v\n%s", err, r.stdout)
	}
	if *file.UnitID != 1 || *file.BaseAddress != 40000 || len(file.Models) != 3 {
		t.Fatalf("file = %+v", file)
	}
	common := file.Models[0]
	if common.Name != "Common" || len(common.Points) != 6 || common.Points[0].Name != "Mn" || common.Points[5].Name != "DA" || common.Points[0].Value != nil {
		t.Errorf("Common template = %+v", common)
	}
	if !strings.Contains(r.stdout, `"Mn": null`) || strings.Contains(r.stdout, `"Pad"`) || strings.Contains(r.stdout, `"L"`) {
		t.Errorf("template text:\n%s", r.stdout)
	}
	if mods := file.Models[1].Groups["module"]; len(mods) != 1 || mods[0].Points[0].Name != "ID" {
		t.Errorf("MPPT template groups = %+v", file.Models[1].Groups)
	}
	if pts := file.Models[2].Groups["Crv"][0].Groups["Pt"]; len(pts) != 1 || len(pts[0].Points) != 2 {
		t.Errorf("volt-var template = %+v", file.Models[2].Groups)
	}

	// The template is a valid device: everything not implemented.
	path := filepath.Join(t.TempDir(), "t.json")
	if r := runCLI(t, "template", "--model", "1,160,705", "-o", path); r.err != nil || r.stdout != "" {
		t.Fatalf("template -o: %v, stdout %q", r.err, r.stdout)
	}
	s := startServe(t, path)
	models, _ := readAll(t, s.url)
	if len(models) != 3 || len(models[2].Group.GroupsNamed("Crv")) != 1 || point(t, models[0].Group, "Mn").Implemented {
		t.Errorf("served template: %d models", len(models))
	}
	if err := s.stop(t); err != nil {
		t.Errorf("serve returned %v", err)
	}

	r = runCLI(t, "template", "--model", "1,64900")
	wantFail(t, r, "model 64900: no schema is known for this model")
	if r := runCLI(t, "template", "--model", "70000"); r.err == nil {
		t.Error("model 70000 accepted")
	}
	wantFail(t, runCLI(t, "template"), `required flag(s) "model" not set`)
	r = runCLI(t, "template", "--model", "1", "-o", filepath.Join(t.TempDir(), "no", "dir", "x.json"))
	if r.err == nil || !strings.HasPrefix(r.err.Error(), "write device file: ") {
		t.Errorf("unwritable output: %v", r.err)
	}
}

func TestWriteDeviceFile(t *testing.T) {
	// The format itself is tested in go-sunspec's devicefile package; this is
	// the choice between standard output and --output.
	file := devicefile.New(devicefile.DefaultUnitID, devicefile.DefaultBaseAddress)
	var buf bytes.Buffer
	if err := writeDeviceFile(&buf, "", file); err != nil || buf.String() != "{\n  \"unit_id\": 1,\n  \"base_address\": 40000,\n  \"models\": []\n}\n" {
		t.Errorf("to a writer: %v, %q", err, buf.String())
	}
	if err := writeDeviceFile(failingWriter{}, "", file); err == nil {
		t.Error("a failing writer went unnoticed")
	}
	path := filepath.Join(t.TempDir(), "d.json")
	if err := writeDeviceFile(failingWriter{}, path, file); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != buf.String() {
		t.Errorf("to a file: %v, %q", err, data)
	}
}

// TestServeStopsOnSignal runs the real binary and ends it the way a user or
// a service manager does.
func TestServeStopsOnSignal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signals cannot be sent to a child process on Windows")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := writeFile(t, exampleDevice)
	for _, sig := range []os.Signal{os.Interrupt, syscall.SIGTERM} {
		url := "tcp://127.0.0.1:" + strconv.Itoa(freePort(t))
		cmd := exec.Command(exe, "serve", "--file", path, "--url", url)
		cmd.Env = append(os.Environ(), "SUNSPECCTL_TEST_MAIN=1")
		stdout, stderr := &syncBuffer{}, &syncBuffer{}
		cmd.Stdout, cmd.Stderr = stdout, stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		deadline := time.Now().Add(20 * time.Second)
		for !strings.Contains(stdout.String(), "LENGTH") {
			if time.Now().After(deadline) {
				_ = cmd.Process.Kill()
				t.Fatalf("%v: serve never started:\n%s\n%s", sig, stdout.String(), stderr.String())
			}
			time.Sleep(10 * time.Millisecond)
		}
		// It serves.
		if r := runCLI(t, "read-point", "--model", "1", "--point", "Mn", "--url", url, "--unit-id", "3"); r.err != nil {
			t.Errorf("%v: read from the served device: %v", sig, r.err)
		}
		if err := cmd.Process.Signal(sig); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("%v: serve exited with %v\nstderr: %s", sig, err, stderr.String())
			}
		case <-time.After(20 * time.Second):
			_ = cmd.Process.Kill()
			t.Fatalf("%v: serve did not exit", sig)
		}
		if got := stderr.String(); got != "shutting down\n" {
			t.Errorf("%v: stderr = %q", sig, got)
		}
	}

	// An invalid file: exit status 3, the error on stderr, nothing served.
	stdout, stderr, code := runMain(t, "serve", "--file", writeFile(t, `{"models": [{"id": 103, "points": {"Watts": 1}}]}`))
	if code != exitInvalidInput || stdout != "" || !strings.HasPrefix(stderr, "Error: device file ") || !strings.Contains(stderr, "point Watts") {
		t.Errorf("invalid file: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}
