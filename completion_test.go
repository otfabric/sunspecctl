// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// complete runs the hidden __complete command and returns the candidates
// (without descriptions) and the directive line.
func complete(t *testing.T, args ...string) ([]string, string) {
	t.Helper()
	r := runCLI(t, append([]string{"__complete"}, args...)...)
	if r.err != nil {
		t.Fatalf("__complete %v: %v", args, r.err)
	}
	var values []string
	var directive string
	for _, line := range strings.Split(strings.TrimSpace(r.stdout), "\n") {
		if strings.HasPrefix(line, ":") {
			directive = line
			continue
		}
		v, _, _ := strings.Cut(line, "\t")
		values = append(values, v)
	}
	return values, directive
}

func wantCandidates(t *testing.T, args []string, want ...string) {
	t.Helper()
	got, _ := complete(t, args...)
	for _, w := range want {
		if !slices.Contains(got, w) {
			t.Errorf("__complete %v = %v, missing %q", args, got, w)
		}
	}
}

func TestCompleteShells(t *testing.T) {
	got, _ := complete(t, "completion", "")
	if want := []string{"bash", "zsh", "fish", "powershell"}; !slices.Equal(got, want) {
		t.Fatalf("completion shells %v, want %v", got, want)
	}
}

func TestCompleteFlagValues(t *testing.T) {
	wantCandidates(t, []string{"detect", "--parity", ""}, "none", "even", "odd")
	wantCandidates(t, []string{"read", "--stop-bits", ""}, "1", "2")
	wantCandidates(t, []string{"read", "--url", ""}, "tcp://", "rtu://")
	wantCandidates(t, []string{"read-model", "--id", ""}, "1", "103")
	wantCandidates(t, []string{"template", "--model", ""}, "1", "160")
	wantCandidates(t, []string{"read-point", "--model", "1", "--point", ""}, "Mn", "Md", "SN")
	wantCandidates(t, []string{"poll-point", "--model", "1", "--point", ""}, "Mn")
	if got, _ := complete(t, "read-point", "--point", ""); len(got) != 0 {
		t.Errorf("--point without --model offers %v", got)
	}

	got, directive := complete(t, "serve", "--file", "")
	if !slices.Equal(got, []string{"json"}) || directive != fmt.Sprintf(":%d", cobra.ShellCompDirectiveFilterFileExt) {
		t.Errorf("serve --file: %v %s", got, directive)
	}
	if _, directive := complete(t, "poll", "--interval", ""); directive != fmt.Sprintf(":%d", cobra.ShellCompDirectiveNoFileComp) {
		t.Errorf("poll --interval directive %s, want no file completion", directive)
	}
}

// valueSetUsage matches a usage string that lists the values of a flag.
var valueSetUsage = regexp.MustCompile(`\w+\|\w+|one of|: \w+, \w+,? (or|and) \w+|\w+ or \w+ \(`)

// TestEveryValueFlagCompletes walks every command: a flag that takes a value
// has a completion function, so a flag whose usage lists a value set cannot
// lack one.
func TestEveryValueFlagCompletes(t *testing.T) {
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		check := func(f *pflag.Flag) {
			if f.Value.Type() == "bool" || f.Name == "help" {
				return
			}
			if _, ok := c.GetFlagCompletionFunc(f.Name); !ok {
				kind := "value"
				if valueSetUsage.MatchString(f.Usage) {
					kind = "value set"
				}
				t.Errorf("%s --%s (%s flag) has no completion: %s", c.CommandPath(), f.Name, kind, f.Usage)
			}
		}
		c.LocalFlags().VisitAll(check)
		c.InheritedFlags().VisitAll(check)
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(newRootCmd())
}
