// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"strings"

	"github.com/otfabric/go-modbus"
	"github.com/otfabric/go-sunspec"
)

// Process exit codes: the organisation table (docs/exitcodes.md).
const (
	exitOK           = 0
	exitFindings     = 1 // the device answered and is not a SunSpec device
	exitUsage        = 2
	exitInvalidInput = 3
	exitTransport    = 4
	exitTimeout      = 5
	exitProtocol     = 6
	exitUnsupported  = 8 // the device does not have the requested model
	exitPermission   = 9
	exitInternal     = 10
	exitInterrupted  = 130 // 128 + SIGINT, as a shell reports it
)

// exitError gives an error chain an exit code. Interruption and timeouts in
// the chain take precedence, see exitCode.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

// withExit wraps err so that exitCode returns code.
func withExit(code int, err error) error { return &exitError{code: code, err: err} }

// commandEntered records that the command line was accepted: cobra's errors
// before that are usage errors. The root command's pre-run sets it.
var commandEntered bool

// exitCode maps the error of a run to the process exit code.
func exitCode(err error) int {
	if err == nil {
		return exitOK
	}
	if !commandEntered || strings.HasPrefix(err.Error(), "required flag") {
		return exitUsage
	}
	var ne net.Error
	switch {
	case errors.Is(err, context.Canceled):
		return exitInterrupted
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, modbus.ErrRequestTimedOut),
		errors.As(err, &ne) && ne.Timeout():
		return exitTimeout
	}
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code
	}
	switch {
	case errors.Is(err, sunspec.ErrNotSunSpec):
		return exitFindings
	case errors.Is(err, modbus.ErrConfigurationError), errors.Is(err, sunspec.ErrPointNotFound):
		return exitInvalidInput
	case errors.Is(err, sunspec.ErrUnknownModel):
		return exitUnsupported
	case errors.Is(err, fs.ErrPermission):
		return exitPermission
	case errors.As(err, &ne), errors.Is(err, modbus.ErrClientNotOpen), errors.Is(err, modbus.ErrSerialPortNotOpen),
		errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return exitTransport
	case isProtocol(err):
		return exitProtocol
	}
	return exitInternal
}

// isProtocol reports a Modbus exception, a malformed frame, or a device
// whose registers do not decode as SunSpec.
func isProtocol(err error) bool {
	for _, target := range []error{
		modbus.ErrIllegalFunction, modbus.ErrIllegalDataAddress, modbus.ErrIllegalDataValue,
		modbus.ErrServerDeviceFailure, modbus.ErrAcknowledge, modbus.ErrServerDeviceBusy,
		modbus.ErrMemoryParityError, modbus.ErrGWPathUnavailable, modbus.ErrGWTargetFailedToRespond,
		modbus.ErrBadCRC, modbus.ErrBadLRC, modbus.ErrShortFrame, modbus.ErrProtocolError,
		modbus.ErrBadUnitID, modbus.ErrBadTransactionID, modbus.ErrUnknownProtocolID,
		modbus.ErrInvalidMBAPLength, modbus.ErrUnexpectedParameters,
		sunspec.ErrDecode, sunspec.ErrModelLayout, sunspec.ErrPartialRead, sunspec.ErrModelChanged,
		sunspec.ErrModelChainInvalid, sunspec.ErrModelChainLimitExceeded,
	} {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}

// applyFormatEnv sets --json, when not given, from SUNSPECCTL_FORMAT
// (canonical) or SUNSPECCTL_OUTPUT_FORMAT.
func applyFormatEnv(jsonGiven bool) error {
	v := os.Getenv("SUNSPECCTL_FORMAT")
	if v == "" {
		v = os.Getenv("SUNSPECCTL_OUTPUT_FORMAT")
	}
	switch v {
	case "", "text":
		return nil
	case "json":
		if !jsonGiven {
			flagJSON = true
		}
		return nil
	}
	return withExit(exitUsage, fmt.Errorf("invalid output format %q: use text or json", v))
}
