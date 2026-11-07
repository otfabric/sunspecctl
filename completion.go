// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/otfabric/go-sunspec/registry"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// urlSchemes are the Modbus URL schemes --url completes.
var urlSchemes = []string{"tcp://", "tcp+tls://", "udp://", "rtu://", "ascii://", "rtuovertcp://", "rtuoverudp://", "asciiovertcp://"}

// fixedFlagValues are the value sets of the enum flags.
var fixedFlagValues = map[string][]string{
	"parity":    {"none", "even", "odd"},
	"data-bits": {"0", "5", "6", "7", "8"},
	"stop-bits": {"0", "1", "2"},
	"baud":      {"0", "1200", "2400", "4800", "9600", "19200", "38400", "57600", "115200"},
}

// fileFlagExtensions are the flags that name an input file of a known type.
var fileFlagExtensions = map[string][]string{
	"file":     {"json"},
	"tls-ca":   {"pem", "crt", "cer"},
	"tls-cert": {"pem", "crt", "cer"},
	"tls-key":  {"pem", "key"},
}

// registerCompletions gives every value flag of c and its subcommands a
// completion function: the values of an enum flag, the model IDs with a known
// schema for --id and --model, the point names of that model for --point,
// files of the right type for a file flag, the URL schemes for --url, and no
// file names for a free value (a number, a duration). A command without
// positional arguments completes no file names either.
func registerCompletions(c *cobra.Command) {
	if c.Runnable() && !c.HasSubCommands() && c.ValidArgs == nil && c.ValidArgsFunction == nil {
		c.ValidArgsFunction = cobra.NoFileCompletions
	}
	c.LocalNonPersistentFlags().VisitAll(func(f *pflag.Flag) { registerFlagCompletion(c, f) })
	c.PersistentFlags().VisitAll(func(f *pflag.Flag) { registerFlagCompletion(c, f) })
	for _, sub := range c.Commands() {
		registerCompletions(sub)
	}
}

func registerFlagCompletion(c *cobra.Command, f *pflag.Flag) {
	if f.Value.Type() == "bool" {
		return
	}
	if _, ok := c.GetFlagCompletionFunc(f.Name); ok {
		return
	}
	var fn cobra.CompletionFunc
	switch {
	case fixedFlagValues[f.Name] != nil:
		fn = cobra.FixedCompletions(fixedFlagValues[f.Name], cobra.ShellCompDirectiveNoFileComp)
	case fileFlagExtensions[f.Name] != nil:
		fn = cobra.FixedCompletions(fileFlagExtensions[f.Name], cobra.ShellCompDirectiveFilterFileExt)
	case f.Name == "output":
		fn = cobra.FixedCompletions(nil, cobra.ShellCompDirectiveDefault)
	case f.Name == "url":
		fn = cobra.FixedCompletions(urlSchemes, cobra.ShellCompDirectiveNoFileComp|cobra.ShellCompDirectiveNoSpace)
	case f.Name == "id" || f.Name == "model":
		fn = completeModelIDs
	case f.Name == "point":
		fn = completePointNames
	default:
		fn = cobra.NoFileCompletions
	}
	_ = c.RegisterFlagCompletionFunc(f.Name, fn)
}

// completeModelIDs offers the model IDs go-sunspec has a schema for, with the
// model label as the description.
func completeModelIDs(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	all := registry.All()
	ids := make([]int, 0, len(all))
	for id := range all {
		ids = append(ids, int(id))
	}
	slices.Sort(ids)
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		m := all[uint16(id)]
		out = append(out, fmt.Sprintf("%d\t%s", id, m.Label))
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

// completePointNames offers the point names of the model given with --model.
func completePointNames(cmd *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	f := cmd.Flags().Lookup("model")
	if f == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	id, err := strconv.ParseUint(f.Value.String(), 10, 16)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	m := registry.ByID(uint16(id))
	if m == nil || m.Group == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	out := make([]string, 0, len(m.Group.Points))
	for _, p := range m.Group.Points {
		if p.Label != "" {
			out = append(out, p.Name+"\t"+p.Label)
		} else {
			out = append(out, p.Name)
		}
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}
