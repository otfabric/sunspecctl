// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/otfabric/go-modbus"
	"github.com/otfabric/go-sunspec"
	"github.com/otfabric/go-sunspec/devicefile"
	"github.com/otfabric/go-sunspec/registry"
	"github.com/spf13/cobra"
)

// shutdownTimeout bounds how long serve waits for its connections to end.
const shutdownTimeout = 5 * time.Second

// servedDevice is what serve reports about the device it runs. With --json
// it is printed as it is.
type servedDevice struct {
	// URL is the address the device listens on.
	URL string
	// UnitID is the Modbus unit ID it answers.
	UnitID uint8
	// BaseAddress is the register address of the "SunS" marker.
	BaseAddress uint16
	// Models lists the models in chain order.
	Models []servedModel
}

// servedModel is one model of a served device.
type servedModel struct {
	// ID is the SunSpec model ID.
	ID uint16
	// Name is the model's name, or "unknown_<ID>" without a schema.
	Name string
	// StartAddress is the address of the model's ID register.
	StartAddress uint16
	// Length is the number of data registers after the ID and length
	// registers.
	Length uint16
}

// describeServer lists what srv serves.
func describeServer(url string, srv *sunspec.Server) servedDevice {
	d := servedDevice{URL: url, UnitID: srv.UnitID(), BaseAddress: srv.BaseAddress(), Models: []servedModel{}}
	for _, h := range srv.Models() {
		d.Models = append(d.Models, servedModel{ID: h.ID, Name: modelName(h.ID), StartAddress: h.StartAddress, Length: h.Length})
	}
	return d
}

// modelName returns the registry's label for a model ID, as discovery
// reports it, or "unknown_<ID>" for a model without schema.
func modelName(id uint16) string {
	meta := registry.ByID(id)
	if meta == nil {
		return fmt.Sprintf("unknown_%d", id)
	}
	return meta.Label
}

// printServedDevice writes the device's address and model table.
func printServedDevice(out io.Writer, verb string, d servedDevice) {
	_, _ = fmt.Fprintf(out, "%s SunSpec device on %s (unit ID %d, base address %d)\n", verb, d.URL, d.UnitID, d.BaseAddress)
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "ID\tNAME\tSTART\tLENGTH")
	for _, m := range d.Models {
		_, _ = fmt.Fprintf(w, "%d\t%s\t%d\t%d\n", m.ID, m.Name, m.StartAddress, m.Length)
	}
	_ = w.Flush()
}

// writeEvent is what serve prints, with --json, for a client write.
type writeEvent struct {
	// Event is always "write".
	Event string
	// Client is the address of the client that wrote.
	Client string
	// ModelID is the SunSpec ID of the written model.
	ModelID uint16
	// Address is the first register written.
	Address uint16
	// Registers holds the values written.
	Registers []uint16
	// Points holds the written points, decoded.
	Points []sunspec.DecodedPoint
}

// serverWriteHook adapts a write observer to the library's hook: every write
// the device accepts is reported, none is refused.
func serverWriteHook(observe func(*sunspec.ServerWrite)) func(context.Context, *sunspec.ServerWrite) error {
	return func(_ context.Context, w *sunspec.ServerWrite) error {
		observe(w)
		return nil
	}
}

// writeLogger returns an observer that prints client writes to out, one line
// (or, with --json, one document) per write. It is safe for concurrent use.
func writeLogger(out io.Writer) func(*sunspec.ServerWrite) {
	var mu sync.Mutex
	return func(w *sunspec.ServerWrite) {
		mu.Lock()
		defer mu.Unlock()
		if flagJSON {
			_ = printJSON(out, writeEvent{
				Event: "write", Client: w.ClientAddr, ModelID: w.ModelID, Address: w.Address, Registers: w.Registers, Points: w.Points,
			})
			return
		}
		parts := make([]string, 0, len(w.Points))
		for _, p := range w.Points {
			val := fmt.Sprintf("%v", p.RawValue)
			switch {
			case !p.Implemented:
				val = "not implemented"
			case p.ScaledValue != nil:
				val = fmt.Sprintf("%g", *p.ScaledValue)
			}
			if p.Implemented && len(p.Symbols) > 0 {
				val += " [" + strings.Join(p.Symbols, ", ") + "]"
			}
			parts = append(parts, p.Name+" = "+val)
		}
		_, _ = fmt.Fprintf(out, "write from %s: model %d at %d: %s\n", w.ClientAddr, w.ModelID, w.Address, strings.Join(parts, ", "))
	}
}

// serveCmd returns the command that runs a SunSpec device from a file.
func serveCmd() *cobra.Command {
	var (
		file        string
		baseAddress uint16
		check       bool
	)

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve a simulated SunSpec device from a device file",
		Long: `Serve a SunSpec device over Modbus TCP from a JSON device file.

The file lists the device's models with their point values, by SunSpec point
name and in engineering units; see the README for the format. Write one by
hand, start from "sunspecctl template", or copy a real device with
"sunspecctl capture".

The device listens on --url (tcp://host:port, or tcp+tls://host:port with
--tls-cert, --tls-key and --tls-ca) and answers the unit ID and base address
of the file; --unit-id and --base-address override them. The file
is checked completely before the device starts; --check stops there.

Clients can read every register of the device and write the points that
SunSpec defines as writable. Accepted writes are printed. The device runs
until it is interrupted (Ctrl-C) or terminated.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			df, err := devicefile.Load(file)
			if err != nil {
				return fmt.Errorf("device file: %w", err)
			}
			var unitID *uint8
			if cmd.Flags().Changed("unit-id") {
				unitID = &flagUnitID
			}
			var base *uint16
			if cmd.Flags().Changed("base-address") {
				base = &baseAddress
			}
			out := cmd.OutOrStdout()
			srv, err := df.Server(&devicefile.ServerOptions{
				UnitID:      unitID,
				BaseAddress: base,
				OnWrite:     serverWriteHook(writeLogger(out)),
			})
			if err != nil {
				return fmt.Errorf("device file %s: %w", file, err)
			}
			device := describeServer(flagURL, srv)

			if check {
				if flagJSON {
					return printJSON(out, device)
				}
				printServedDevice(out, "Valid", device)
				return nil
			}

			cert, clientCAs, err := loadTLS(flagURL)
			if err != nil {
				return fmt.Errorf("serve: %w", err)
			}
			server, err := modbus.NewServer(&modbus.ServerConfig{
				URL:           flagURL,
				Logger:        modbus.NopLogger(),
				TLSServerCert: cert,
				TLSClientCAs:  clientCAs,
			}, srv)
			if err != nil {
				return fmt.Errorf("serve: %w", err)
			}
			if err := server.Start(); err != nil {
				return fmt.Errorf("serve: listen on %s: %w", flagURL, err)
			}
			if flagJSON {
				if err := printJSON(out, device); err != nil {
					_ = server.Stop()
					return err
				}
			} else {
				printServedDevice(out, "Serving", device)
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			<-ctx.Done()

			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "shutting down")
			sctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
			defer cancel()
			if err := server.Shutdown(sctx); err != nil {
				return fmt.Errorf("serve: shutdown: %w", err)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&file, "file", "f", "", "Device file to serve (required)")
	cmd.Flags().Uint16Var(&baseAddress, "base-address", devicefile.DefaultBaseAddress, "Register address of the SunSpec marker (overrides the file)")
	cmd.Flags().BoolVar(&check, "check", false, "Validate the device file, print the layout and exit")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}
