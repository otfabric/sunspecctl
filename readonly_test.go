// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/otfabric/go-modbus"
	"github.com/otfabric/go-sunspec/testutil"
)

// writeCounter serves a SunSpec fixture and counts the write requests that
// reach it.
type writeCounter struct {
	*testutil.SunSpecHandler
	writes atomic.Int32
}

func (h *writeCounter) HandleHoldingRegisters(ctx context.Context, req *modbus.HoldingRegistersRequest) ([]uint16, error) {
	if req.IsWrite {
		h.writes.Add(1)
		return nil, nil
	}
	return h.SunSpecHandler.HandleHoldingRegisters(ctx, req)
}

func startCountingDevice(t *testing.T) (string, *writeCounter) {
	t.Helper()
	h := &writeCounter{SunSpecHandler: inverter(t)}
	url := "tcp://127.0.0.1:" + strconv.Itoa(freePort(t))
	server, err := modbus.NewServer(&modbus.ServerConfig{URL: url, MaxClients: 16}, h)
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	if err := server.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	t.Cleanup(func() { _ = server.Stop() })
	return url, h
}

func TestClientIsReadOnly(t *testing.T) {
	url, device := startCountingDevice(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// The device accepts writes from a client without the restriction, so a
	// write that got through would be counted.
	plain, err := modbus.New(modbus.Config{URL: url, Timeout: 5 * time.Second, Logger: modbus.NopLogger()})
	if err != nil {
		t.Fatal(err)
	}
	if err := plain.Open(); err != nil {
		t.Fatal(err)
	}
	if err := plain.WriteRegister(ctx, 1, 40002, 1); err != nil {
		t.Fatalf("unrestricted write: %v", err)
	}
	_ = plain.Close()
	if n := device.writes.Load(); n != 1 {
		t.Fatalf("the device saw %d writes from the unrestricted client, want 1", n)
	}
	device.writes.Store(0)

	parseFlags(t, "--url", url, "--timeout", "5s")
	client, cleanup, err := newClient()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if _, err := client.ReadRegisters(ctx, 1, 40000, 2, modbus.HoldingRegister); err != nil {
		t.Errorf("read: %v", err)
	}
	if err := client.WriteRegister(ctx, 1, 40002, 1); !errors.Is(err, modbus.ErrFunctionCodeNotAllowed) {
		t.Errorf("WriteRegister: %v, want ErrFunctionCodeNotAllowed", err)
	}
	if err := client.WriteRegisters(ctx, 1, 40002, []uint16{1, 2}); !errors.Is(err, modbus.ErrFunctionCodeNotAllowed) {
		t.Errorf("WriteRegisters: %v, want ErrFunctionCodeNotAllowed", err)
	}
	if err := client.WriteCoil(ctx, 1, 0, true); !errors.Is(err, modbus.ErrFunctionCodeNotAllowed) {
		t.Errorf("WriteCoil: %v, want ErrFunctionCodeNotAllowed", err)
	}

	// The reading commands work through the restricted client.
	for _, args := range [][]string{{"detect"}, {"models"}, {"read"}, {"read-model", "--id", "1"}, {"poll", "--count", "1", "--interval", "1ms"}, {"read-point", "--model", "1", "--point", "Mn"}, {"capture"}} {
		if r := runCLI(t, append(args, "--url", url)...); r.err != nil {
			t.Errorf("%v: %v", args, r.err)
		}
	}
	if n := device.writes.Load(); n != 0 {
		t.Errorf("the device saw %d writes", n)
	}
}
