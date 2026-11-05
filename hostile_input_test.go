// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/otfabric/go-sunspec/devicefile"
)

// Tests in this file give the command line, and the files it reads and
// writes, values a user or a script should not have given.

// Flag values that cannot be meant are usage errors, found before any I/O:
// they used to fail later and differently (--timeout 0 as a timeout, a
// negative --count as success without output, --interval 0 as polling as
// fast as the device answers, --unit-id 0 as a read of unit 1).
func TestHostileFlagValues(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"read", "--timeout", "0s"}, "invalid --timeout 0s: use a positive duration such as 10s"},
		{[]string{"detect", "--timeout", "-1s"}, "invalid --timeout -1s: use a positive duration such as 10s"},
		{[]string{"poll", "--count", "1", "--timeout", "0"}, "invalid --timeout 0s: use a positive duration such as 10s"},
		{[]string{"capture", "--timeout", "-5m"}, "invalid --timeout -5m0s: use a positive duration such as 10s"},
		{[]string{"poll", "--interval", "0s"}, "invalid --interval 0s: use a positive duration such as 5s"},
		{[]string{"poll-model", "--id", "1", "--interval", "-1s"}, "invalid --interval -1s: use a positive duration such as 5s"},
		{[]string{"poll-point", "--model", "1", "--point", "Mn", "--count", "-1"}, "invalid --count -1: use 0 (poll until interrupted) or more"},
		{[]string{"models", "--unit-id", "0"}, "invalid --unit-id 0: that is the Modbus broadcast address, which a device does not answer; use 1-255"},
		{[]string{"read-point", "--model", "1", "--point", "Mn", "--unit-id", "0"}, "invalid --unit-id 0: that is the Modbus broadcast address, which a device does not answer; use 1-255"},
	}
	for _, tc := range tests {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			// A URL nothing listens on: the error must come before any I/O.
			r := runCLI(t, append(tc.args, "--url", "tcp://127.0.0.1:1")...)
			wantFail(t, r, tc.want)
			if code := exitCode(r.err); code != exitUsage {
				t.Errorf("exit %d, want %d", code, exitUsage)
			}
		})
	}

	// serve takes --unit-id 0 from the device file's check.
	r := runCLI(t, "serve", "--file", writeFile(t, exampleDevice), "--unit-id", "0", "--check")
	if r.err == nil || exitCode(r.err) != exitInvalidInput || !strings.Contains(r.err.Error(), "broadcast") {
		t.Errorf("serve --unit-id 0: %v", r.err)
	}
}

// Text output whose writes fail ends the command with exit 10, as JSON
// output did already; before, the command printed nothing and exited 0.
func TestHostileTextOutputWriteError(t *testing.T) {
	url := startDevice(t, inverter(t))
	for _, args := range [][]string{
		{"version"}, {"detect"}, {"models"}, {"read"}, {"read-model", "--id", "101"},
		{"read-point", "--model", "101", "--point", "W"},
		{"poll", "--count", "3", "--interval", "1ms"},
		{"poll-model", "--id", "101", "--count", "3", "--interval", "1ms"},
		{"poll-point", "--model", "101", "--point", "W", "--count", "3", "--interval", "1ms"},
		{"serve", "--file", writeFile(t, exampleDevice), "--check"},
	} {
		t.Run(args[0], func(t *testing.T) {
			root := newRootCmd()
			var stderr bytes.Buffer
			root.SetOut(failingWriter{})
			root.SetErr(&stderr)
			root.SetArgs(append(args, "--url", url))
			err := root.Execute()
			if err == nil || exitCode(err) != exitInternal || !strings.Contains(err.Error(), "disk full") {
				t.Errorf("err = %v (exit %d), want exit %d", err, exitCode(err), exitInternal)
			}
		})
	}
	// JSON output kept its behaviour.
	root := newRootCmd()
	root.SetOut(failingWriter{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"version", "--json"})
	if err := root.Execute(); err == nil || exitCode(err) != exitInternal {
		t.Errorf("version --json: %v", err)
	}
}

// capture and template write a new file with mode 0600, never through a
// symbolic link and never half. They used to write mode 0644 (a capture
// holds serial numbers), follow a link to wherever it pointed, and truncate
// the old file before writing.
func TestHostileOutputFile(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "device.json")

	if r := runCLI(t, "template", "--model", "1", "-o", out); r.err != nil {
		t.Fatal(r.err)
	}
	if runtime.GOOS != "windows" {
		if fi, err := os.Stat(out); err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("mode = %v, %v; want 0600", fi.Mode(), err)
		}
	}
	// Replacing an existing, more open file gives the new mode too.
	if err := os.Chmod(out, 0o644); err != nil {
		t.Fatal(err)
	}
	if r := runCLI(t, "template", "--model", "1,103", "-o", out); r.err != nil {
		t.Fatal(r.err)
	}
	df, err := devicefile.Load(out)
	if err != nil || len(df.Models) != 2 {
		t.Fatalf("rewritten file: %v", err)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(out); fi.Mode().Perm() != 0o600 {
			t.Errorf("mode after replacing = %v", fi.Mode())
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("temporary files left behind: %v", entries)
	}

	// A symbolic link is refused, and what it names is left alone.
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("no symbolic links here: %v", err)
	}
	r := runCLI(t, "template", "--model", "1", "-o", link)
	if r.err == nil || exitCode(r.err) != exitInternal || !strings.Contains(r.err.Error(), "symbolic link") {
		t.Errorf("symlink: %v", r.err)
	}
	if data, _ := os.ReadFile(target); string(data) != "keep" {
		t.Errorf("the link target was written: %q", data)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&fs.ModeSymlink == 0 {
		t.Errorf("the link was replaced: %v %v", fi, err)
	}

	// A dangling link too: it would create the file it names.
	dangling := filepath.Join(dir, "dangling.json")
	if err := os.Symlink(filepath.Join(dir, "created-elsewhere"), dangling); err != nil {
		t.Fatal(err)
	}
	if r := runCLI(t, "template", "--model", "1", "-o", dangling); r.err == nil {
		t.Error("a dangling link was written through")
	}
	if _, err := os.Stat(filepath.Join(dir, "created-elsewhere")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("file created through a dangling link: %v", err)
	}

	// A directory, or a device such as /dev/null, is not a file to replace.
	r = runCLI(t, "template", "--model", "1", "-o", dir)
	if r.err == nil || !strings.Contains(r.err.Error(), "not a regular file") {
		t.Errorf("directory: %v", r.err)
	}
	if runtime.GOOS != "windows" {
		r = runCLI(t, "template", "--model", "1", "-o", os.DevNull)
		if r.err == nil || !strings.Contains(r.err.Error(), "not a regular file") {
			t.Errorf("%s: %v", os.DevNull, r.err)
		}
	}
}

// When the new file cannot be written, the old one is kept whole.
func TestHostileOutputFileKeptOnFailure(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory that refuses new files")
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "device.json")
	if err := os.WriteFile(out, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	r := runCLI(t, "template", "--model", "1", "-o", out)
	if r.err == nil || exitCode(r.err) != exitInternal {
		t.Errorf("err = %v", r.err)
	}
	if data, _ := os.ReadFile(out); string(data) != "old" {
		t.Errorf("old file = %q", data)
	}
}

func TestWriteFileAtomicErrors(t *testing.T) {
	dir := t.TempDir()
	// Lstat fails for a reason other than a missing file.
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(filepath.Join(file, "below"), []byte("x")); err == nil {
		t.Error("a path below a file was written")
	}
	if err := writeFileAtomic(filepath.Join(dir, "missing", "x"), []byte("x")); err == nil {
		t.Error("a path in a missing directory was written")
	}
}

// Files the commands read are bounded: a wrong path such as /dev/zero, or a
// huge file, is an error and not a process that eats all memory.
func TestHostileHugeInputFiles(t *testing.T) {
	huge := filepath.Join(t.TempDir(), "huge.json")
	f, err := os.Create(huge)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxDeviceFileSize + 1); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	paths := []string{huge}
	if runtime.GOOS != "windows" {
		if _, err := os.Stat("/dev/zero"); err == nil {
			paths = append(paths, "/dev/zero")
		}
	}
	for _, p := range paths {
		r := runCLI(t, "serve", "--file", p, "--check")
		if r.err == nil || exitCode(r.err) != exitInvalidInput || !strings.Contains(r.err.Error(), "file too large") {
			t.Errorf("serve --file %s: %v", p, r.err)
		}
		ca := newTestCA(t, "CA")
		cert, key := ca.issue("client")
		for _, flags := range [][]string{
			{"--tls-cert", p, "--tls-key", key, "--tls-ca", ca.file},
			{"--tls-cert", cert, "--tls-key", p, "--tls-ca", ca.file},
			{"--tls-cert", cert, "--tls-key", key, "--tls-ca", p},
		} {
			r := runCLI(t, append([]string{"models", "--url", "tcp+tls://127.0.0.1:1"}, flags...)...)
			if r.err == nil || exitCode(r.err) != exitInvalidInput || !strings.Contains(r.err.Error(), "file too large") {
				t.Errorf("%v: %v", flags, r.err)
			}
		}
	}
	// A directory cannot be read as a file.
	if _, err := readFileLimited(t.TempDir(), 10); err == nil {
		t.Error("a directory was read")
	}
	// The limit itself is allowed.
	small := writeFile(t, "0123456789")
	if data, err := readFileLimited(small, 10); err != nil || string(data) != "0123456789" {
		t.Errorf("at the limit: %q, %v", data, err)
	}
	if _, err := readFileLimited(small, 9); !errors.Is(err, errTooLarge) {
		t.Errorf("over the limit: %v", err)
	}
	// A CA file without certificates keeps its message.
	ca := newTestCA(t, "CA")
	cert, key := ca.issue("client")
	r := runCLI(t, "models", "--url", "tcp+tls://127.0.0.1:1", "--tls-cert", cert, "--tls-key", key, "--tls-ca", writeFile(t, ""))
	if r.err == nil || !strings.HasSuffix(r.err.Error(), ": no certificate found") {
		t.Errorf("empty CA file: %v", r.err)
	}
}

// A failing last step (the rename) removes the temporary file and keeps the
// old one; a file that does not encode is not written at all.
func TestWriteDeviceFileFailures(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "device.json")
	if err := os.WriteFile(out, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	file := devicefile.New(devicefile.DefaultUnitID, devicefile.DefaultBaseAddress)

	saved := rename
	rename = func(string, string) error { return errors.New("rename refused") }
	err := writeDeviceFile(nil, out, file)
	rename = saved
	if err == nil || err.Error() != "rename refused" {
		t.Errorf("err = %v", err)
	}
	if data, _ := os.ReadFile(out); string(data) != "old" {
		t.Errorf("old file = %q", data)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("temporary file left behind: %v", entries)
	}

	id := uint16(1)
	file.Models = append(file.Models, devicefile.Model{ID: &id, Group: devicefile.Group{Points: devicefile.Points{{Name: "Mn", Value: func() {}}}}})
	if err := writeDeviceFile(nil, out, file); err == nil {
		t.Error("an unencodable file was written")
	}
	if data, _ := os.ReadFile(out); string(data) != "old" {
		t.Errorf("old file = %q", data)
	}
}

func TestTermSafe(t *testing.T) {
	for in, want := range map[string]string{
		"":               "",
		"Acme PV-5000 ü": "Acme PV-5000 ü",
		"\x00\x7f":       `\x00\x7f`,
		"a\U000E0001b":   `a\U000e0001b`,
		"\u00a0":         `\u00a0`,
		`C:\dir`:         `C:\dir`,
	} {
		if got := termSafe(in); got != want {
			t.Errorf("termSafe(%q) = %q, want %q", in, got, want)
		}
	}
	if err := safeError(nil); err != nil {
		t.Errorf("safeError(nil) = %v", err)
	}
	plain := errors.New("plain")
	if safeError(plain) != plain {
		t.Error("an error without control characters was wrapped")
	}
}

// serve stops listening when it cannot print the layout as text, as it did
// for JSON.
func TestServeTextStatusWriteError(t *testing.T) {
	root := newRootCmd()
	root.SetOut(failingWriter{})
	root.SetErr(&bytes.Buffer{})
	url := "tcp://127.0.0.1:" + strconv.Itoa(freePort(t))
	root.SetArgs([]string{"serve", "--file", writeFile(t, exampleDevice), "--url", url})
	if err := root.Execute(); err == nil || exitCode(err) != exitInternal || !strings.Contains(err.Error(), "disk full") {
		t.Errorf("err = %v", err)
	}
	if r := runCLI(t, "detect", "--url", url, "--timeout", "1s"); r.err == nil {
		t.Error("the device is still listening")
	}
}
