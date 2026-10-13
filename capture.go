// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/otfabric/go-sunspec"
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

			unitID, base := device.UnitID, device.Discovery.BaseAddress
			file := &deviceFile{UnitID: &unitID, BaseAddress: &base, Models: []fileModel{}}
			for _, inst := range device.Discovery.Models {
				dm, err := device.ReadModel(ctx, inst)
				// A model that does not fit its schema is still captured,
				// from its registers. Anything else means the registers are
				// not all there.
				if err != nil && !errors.Is(err, sunspec.ErrDecode) {
					return fmt.Errorf("read model %d: %w", inst.Header.ID, err)
				}
				file.Models = append(file.Models, fileModelFromDecoded(dm, flagRaw))
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
			unitID, base := uint8(1), uint16(defaultBaseAddress)
			file := &deviceFile{UnitID: &unitID, BaseAddress: &base, Models: []fileModel{}}
			for _, id := range modelIDs {
				if id > 0xFFFF || !registry.Known(uint16(id)) {
					return fmt.Errorf("model %d: no schema is known for this model", id)
				}
				file.Models = append(file.Models, templateModel(registry.ByID(uint16(id))))
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
