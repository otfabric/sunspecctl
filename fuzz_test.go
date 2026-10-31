// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	sunspec "github.com/otfabric/go-sunspec"
	"github.com/otfabric/go-sunspec/registry"
)

// FuzzPrintDecodedModel takes registers as a device may report them through
// what the read commands do with them: decode, print as text and as JSON. A
// device decides these bytes, and the output must always be valid JSON.
func FuzzPrintDecodedModel(f *testing.F) {
	for _, id := range []uint16{1, 101, 103, 160, 705} {
		meta := registry.ByID(id)
		if meta == nil {
			continue
		}
		regs, err := sunspec.EncodeModel(meta, nil)
		if err != nil {
			f.Fatal(err)
		}
		b := make([]byte, 2*len(regs))
		for i, r := range regs {
			b[2*i], b[2*i+1] = byte(r>>8), byte(r)
		}
		f.Add(id, b)
	}
	f.Add(uint16(103), []byte{})
	f.Fuzz(func(t *testing.T, id uint16, data []byte) {
		meta := registry.ByID(id)
		if meta == nil {
			return
		}
		regs := make([]uint16, len(data)/2)
		for i := range regs {
			regs[i] = uint16(data[2*i])<<8 | uint16(data[2*i+1])
		}
		dm, err := sunspec.DecodeModel(regs, meta, 40000)
		if dm == nil {
			if err == nil {
				t.Fatal("no model and no error")
			}
			return
		}
		printDecodedModel(io.Discard, dm)
		var out bytes.Buffer
		if err := printJSON(&out, dm); err != nil {
			t.Fatalf("a decoded model does not marshal: %v", err)
		}
		if !json.Valid(out.Bytes()) {
			t.Fatalf("invalid JSON: %s", out.Bytes())
		}
	})
}

// FuzzServeCheck takes arbitrary device files through serve --check, which is
// what runs on a file before a device starts. It needs no network.
func FuzzServeCheck(f *testing.F) {
	f.Add([]byte(`{"unit_id":1,"base_address":40000,"models":[{"id":1,"points":{"Mn":"OT Fabric","Md":"X"}},{"id":103,"points":{"A":1.5,"A_SF":-1}}]}`))
	f.Add([]byte(`{"models":[{"id":160,"groups":{"module":[{"ID":1}]}}]}`))
	f.Add([]byte(`{"models":[{"id":64900,"registers":[1,2,3]}]}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`[]`))
	f.Add([]byte(`{"unit_id":0,"base_address":65535,"models":[{"id":1}]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		path := filepath.Join(t.TempDir(), "device.json")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		root := newRootCmd()
		root.SetArgs([]string{"serve", "--file", path, "--check", "--json"})
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(io.Discard)
		if err := root.Execute(); err != nil {
			return // a refused file: cobra prints the usage on the same stream
		}
		if !json.Valid(out.Bytes()) {
			t.Fatalf("invalid JSON: %s", out.Bytes())
		}
	})
}
