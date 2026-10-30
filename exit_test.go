// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"testing"

	"github.com/otfabric/go-modbus"
	"github.com/otfabric/go-sunspec"
)

func TestExitCode(t *testing.T) {
	commandEntered = true
	t.Cleanup(func() { commandEntered = false })
	for _, c := range []struct {
		err  error
		want int
	}{
		{nil, exitOK},
		{errors.New("required flag(s) \"file\" not set"), exitUsage},
		{fmt.Errorf("detect: %w", sunspec.ErrNotSunSpec), exitFindings},
		{fmt.Errorf("x: %w", context.Canceled), exitInterrupted},
		{withExit(exitTransport, fmt.Errorf("x: %w", context.Canceled)), exitInterrupted},
		{fmt.Errorf("x: %w", context.DeadlineExceeded), exitTimeout},
		{modbus.ErrRequestTimedOut, exitTimeout},
		{modbus.ErrConfigurationError, exitInvalidInput},
		{sunspec.ErrPointNotFound, exitInvalidInput},
		{sunspec.ErrUnknownModel, exitUnsupported},
		{fs.ErrPermission, exitPermission},
		{&net.OpError{Op: "dial", Err: errors.New("refused")}, exitTransport},
		{io.EOF, exitTransport},
		{modbus.ErrIllegalDataAddress, exitProtocol},
		{sunspec.ErrDecode, exitProtocol},
		{withExit(exitUnsupported, errors.New("model 7 not found")), exitUnsupported},
		{errors.New("boom"), exitInternal},
	} {
		if got := exitCode(c.err); got != c.want {
			t.Errorf("exitCode(%v) = %d, want %d", c.err, got, c.want)
		}
	}
}

func TestCommandExitCodes(t *testing.T) {
	for _, c := range []struct {
		name string
		args []string
		want int
	}{
		{"unknown command", []string{"nope"}, exitUsage},
		{"missing required flag", []string{"serve"}, exitUsage},
		{"version", []string{"version"}, exitOK},
		{"missing device file", []string{"serve", "--file", "/does/not/exist"}, exitInvalidInput},
		{"invalid parity", []string{"detect", "--url", "tcp://127.0.0.1:1", "--parity", "x"}, exitUsage},
	} {
		if got := exitCode(runCLI(t, c.args...).err); got != c.want {
			t.Errorf("%s: exit %d, want %d", c.name, got, c.want)
		}
	}
}

func TestFormatEnv(t *testing.T) {
	t.Setenv("SUNSPECCTL_FORMAT", "")
	t.Setenv("SUNSPECCTL_OUTPUT_FORMAT", "json")
	runCLI(t, "version")
	if !flagJSON {
		t.Error("OUTPUT_FORMAT ignored")
	}
	t.Setenv("SUNSPECCTL_FORMAT", "text")
	runCLI(t, "version")
	if flagJSON {
		t.Error("FORMAT does not win")
	}
	t.Setenv("SUNSPECCTL_FORMAT", "xml")
	if got := exitCode(runCLI(t, "version").err); got != exitUsage {
		t.Errorf("invalid format: exit %d", got)
	}
}
