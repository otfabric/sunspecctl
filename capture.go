// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"io"

	"github.com/otfabric/go-sunspec"
	"github.com/otfabric/go-sunspec/devicefile"
	"github.com/otfabric/go-sunspec/registry"
	"github.com/spf13/cobra"
)

// captureCmd returns the command that reads a device and writes it as a
// device file.
func captureCmd() *cobra.Command {
	var output string

	cmd := &cobra.Command{
		Use:   "capture",
		Short: "Read a device and write it as a device file for serve",
		Long: `Read every model of the SunSpec device at --url and write a device file
that "sunspecctl serve" turns back into the same device.

Models that decode cleanly are written as point values, which are easy to
read and edit. A model without a known schema, or one that does not match its
schema exactly, is written as raw registers, as are all models with --raw. In
both cases serving the file reproduces what was read.

The file goes to standard output, or to the file named with --output.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, cleanup, err := newClient()
			if err != nil {
				return err
			}
			defer cleanup()

			ctx, cancel := context.WithTimeout(context.Background(), flagTimeout)
			defer cancel()

			device, err := sunspec.Discover(ctx, client, discoverOpts())
			if err != nil {
				return fmt.Errorf("discover: %w", err)
			}

			// A model that does not fit its schema is still captured, from its
			// registers; any other read error ends the capture, already
			// prefixed with "read model <id>".
			file, err := devicefile.Capture(ctx, device, flagRaw)
			if err != nil {
				return err
			}
			if err := writeDeviceFile(cmd.OutOrStdout(), output, file); err != nil {
				return fmt.Errorf("write device file: %w", err)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&output, "output", "o", "", "Write the device file here instead of standard output")
	return cmd
}

// templateCmd returns the command that writes a device file skeleton.
func templateCmd() *cobra.Command {
	var (
		modelIDs []uint
		output   string
	)

	cmd := &cobra.Command{
		Use:   "template",
		Short: "Write a device file skeleton for the given models",
		Long: `Write a device file with the given SunSpec models, every point present and
set to null (not implemented), and one instance of each repeating group.
Fill in the values and run it with "sunspecctl serve".

The file goes to standard output, or to the file named with --output.`,
		Example: "  sunspecctl template --model 1,103,160 -o device.json",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			file := devicefile.New(devicefile.DefaultUnitID, devicefile.DefaultBaseAddress)
			for _, id := range modelIDs {
				if id > 0xFFFF || !registry.Known(uint16(id)) {
					return fmt.Errorf("model %d: no schema is known for this model", id)
				}
				file.Models = append(file.Models, devicefile.Template(registry.ByID(uint16(id))))
			}
			if err := writeDeviceFile(cmd.OutOrStdout(), output, file); err != nil {
				return fmt.Errorf("write device file: %w", err)
			}
			return nil
		},
	}

	cmd.Flags().UintSliceVar(&modelIDs, "model", nil, "Model IDs to include, in order (repeat or separate with commas; required)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Write the device file here instead of standard output")
	_ = cmd.MarkFlagRequired("model")
	return cmd
}

// writeDeviceFile writes the file as indented JSON, to the named file or,
// when path is empty, to out.
func writeDeviceFile(out io.Writer, path string, file *devicefile.File) error {
	if path == "" {
		return file.Write(out)
	}
	return file.WriteFile(path)
}
