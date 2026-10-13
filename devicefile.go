// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/otfabric/go-sunspec"
	"github.com/otfabric/go-sunspec/registry"
)

// defaultBaseAddress is where a device file without "base_address" puts the
// SunSpec marker.
const defaultBaseAddress = 40000

// deviceFile is the JSON document that describes a SunSpec device: what
// `serve` runs and what `capture` and `template` write.
//
//	{
//	  "unit_id": 1,
//	  "base_address": 40000,
//	  "models": [
//	    {"id": 1, "points": {"Mn": "Acme", "Md": "PV-5000"}},
//	    {"id": 103, "points": {"W": 4200, "W_SF": 0, "St": "MPPT"}},
//	    {"id": 160, "groups": {"module": [{"points": {"DCW": 2100}}]}},
//	    {"id": 64001, "registers": [1, 2, 3]}
//	  ]
//	}
type deviceFile struct {
	// UnitID is the Modbus unit ID the device answers; 1 when absent.
	UnitID *uint8 `json:"unit_id,omitempty"`
	// BaseAddress is the register address of the "SunS" marker; 40000 when
	// absent.
	BaseAddress *uint16 `json:"base_address,omitempty"`
	// Models lists the device's models in chain order.
	Models []fileModel `json:"models"`
}

// fileModel is one model of a device file: given by point values, which are
// encoded with the model's schema, or by its raw data registers.
type fileModel struct {
	// ID is the SunSpec model ID.
	ID *uint16 `json:"id"`
	// Name is the model's name. It is written for the reader's benefit and
	// ignored when the file is loaded.
	Name string `json:"name,omitempty"`
	fileGroup
	// Registers holds the data registers that follow the model's ID and
	// length registers. It replaces points and groups, and is the only way
	// to describe a model without a schema.
	Registers []uint16 `json:"registers,omitempty"`
}

// fileGroup holds the values of one group instance.
type fileGroup struct {
	// Points maps point names to values.
	Points pointList `json:"points,omitempty"`
	// Groups maps the name of a nested group to its instances.
	Groups map[string][]*fileGroup `json:"groups,omitempty"`
}

// pointEntry is one point of a group in a device file.
type pointEntry struct {
	name string
	// value is, after loading, what encoding/json makes of the JSON value
	// with UseNumber: json.Number, string, []interface{}, nil, bool or a
	// map. For writing it is any value json.Marshal accepts.
	value interface{}
}

// pointList is a JSON object of point values that keeps the order of its
// members, so that a written file lists the points in schema order, and that
// notices a point given twice.
type pointList []pointEntry

// MarshalJSON writes the points as one object, in order.
func (l pointList) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, e := range l {
		if i > 0 {
			buf.WriteByte(',')
		}
		// A string always marshals.
		name, _ := json.Marshal(e.name)
		value, err := json.Marshal(e.value)
		if err != nil {
			return nil, fmt.Errorf("point %s: %w", e.name, err)
		}
		buf.Write(name)
		buf.WriteByte(':')
		buf.Write(value)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// UnmarshalJSON reads an object of point values, keeping its order.
func (l *pointList) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if tok == nil {
		*l = nil
		return nil
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return errors.New(`"points" must be an object of point names and values`)
	}
	var out pointList
	seen := make(map[string]bool)
	for dec.More() {
		// An object's keys are strings: the decoder guarantees it.
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		name := tok.(string)
		if seen[name] {
			return fmt.Errorf("point %s is given twice", name)
		}
		seen[name] = true
		var value interface{}
		if err := dec.Decode(&value); err != nil {
			return err
		}
		out = append(out, pointEntry{name: name, value: value})
	}
	*l = out
	return nil
}

// loadDeviceFile reads and parses a device file. Errors name the file and,
// for a syntax or type error, the line and column.
func loadDeviceFile(path string) (*deviceFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	file, err := parseDeviceFile(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return file, nil
}

// parseDeviceFile parses a device file. Unknown members are errors: a
// misspelt "points" must not silently serve an empty model.
func parseDeviceFile(data []byte) (*deviceFile, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var file deviceFile
	if err := dec.Decode(&file); err != nil {
		var syntaxErr *json.SyntaxError
		var typeErr *json.UnmarshalTypeError
		switch {
		case errors.As(err, &syntaxErr):
			// The offset counts the bytes read, the offending one included.
			return nil, fmt.Errorf("%s: %w", position(data, syntaxErr.Offset-1), err)
		case errors.As(err, &typeErr):
			return nil, fmt.Errorf("%s: %w", position(data, typeErr.Offset), err)
		case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
			return nil, fmt.Errorf("the file ends before the JSON document does: %w", err)
		}
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("data after the end of the JSON document")
	}
	return &file, nil
}

// position formats a byte offset in data as "line L, column C".
func position(data []byte, offset int64) string {
	if offset > int64(len(data)) {
		offset = int64(len(data))
	}
	if offset < 0 {
		offset = 0
	}
	line, col := 1, 1
	for _, b := range data[:offset] {
		if b == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return fmt.Sprintf("line %d, column %d", line, col)
}

// serverModels converts the file's models to what the library serves. Errors
// say which model, by position and ID, and which point they concern. The
// values themselves are checked when the device is built.
func (f *deviceFile) serverModels() ([]sunspec.ServerModel, error) {
	models := make([]sunspec.ServerModel, 0, len(f.Models))
	for i := range f.Models {
		m := &f.Models[i]
		if m.ID == nil {
			return nil, fmt.Errorf("models[%d]: \"id\" is missing", i)
		}
		where := fmt.Sprintf("models[%d] (model %d)", i, *m.ID)
		sm := sunspec.ServerModel{ID: *m.ID}
		switch {
		case m.Registers != nil:
			if m.Points != nil || m.Groups != nil {
				return nil, fmt.Errorf("%s: give either \"registers\" or \"points\" and \"groups\", not both", where)
			}
			sm.Registers = m.Registers
		case !registry.Known(*m.ID):
			return nil, fmt.Errorf("%s: no schema is known for this model: give its data as \"registers\"", where)
		default:
			values, err := m.values("")
			if err != nil {
				return nil, fmt.Errorf("%s: %w", where, err)
			}
			sm.Values = values
		}
		models = append(models, sm)
	}
	return models, nil
}

// values converts a group instance of the file. path names the instance in
// errors.
func (g *fileGroup) values(path string) (*sunspec.Values, error) {
	v := &sunspec.Values{}
	for _, e := range g.Points {
		value, err := pointValue(e.value)
		if err != nil {
			return nil, fmt.Errorf("%spoint %s: %w", path, e.name, err)
		}
		v.Set(e.name, value)
	}
	for name, instances := range g.Groups {
		for i, inst := range instances {
			if inst == nil {
				inst = &fileGroup{}
			}
			child, err := inst.values(fmt.Sprintf("%sgroup %s[%d]: ", path, name, i+1))
			if err != nil {
				return nil, err
			}
			v.Add(name, child)
		}
	}
	return v, nil
}

// pointValue converts a JSON value to a value the library encodes: a number,
// a string, a list of bitfield symbol names, a list of raw registers, or nil
// for a point that is not implemented.
func pointValue(value interface{}) (interface{}, error) {
	switch v := value.(type) {
	case nil, string, json.Number:
		return v, nil
	case []interface{}:
		names := make([]string, 0, len(v))
		regs := make([]uint16, 0, len(v))
		for _, el := range v {
			switch e := el.(type) {
			case string:
				names = append(names, e)
			case json.Number:
				n, err := strconv.ParseUint(string(e), 10, 16)
				if err != nil {
					return nil, fmt.Errorf("register value %s is not a whole number from 0 to 65535", e)
				}
				regs = append(regs, uint16(n))
			default:
				return nil, errors.New("a list must hold bitfield symbol names or raw register values")
			}
		}
		if len(names) > 0 && len(regs) > 0 {
			return nil, errors.New("a list must hold either bitfield symbol names or raw register values, not both")
		}
		if len(regs) > 0 {
			return regs, nil
		}
		return names, nil
	case bool:
		return nil, fmt.Errorf("unsupported value %v: use a number, a string, a list or null", v)
	default:
		return nil, errors.New("unsupported value: use a number, a string, a list or null, not an object")
	}
}

// server builds the device the file describes, which validates every value
// and the layout. unitID and baseAddress, when not nil, replace the file's.
func (f *deviceFile) server(unitID *uint8, baseAddress *uint16, onWrite func(*sunspec.ServerWrite)) (*sunspec.Server, error) {
	models, err := f.serverModels()
	if err != nil {
		return nil, err
	}
	opts := &sunspec.ServerOptions{UnitID: 1, BaseAddress: defaultBaseAddress}
	if f.UnitID != nil {
		opts.UnitID = *f.UnitID
	}
	if unitID != nil {
		opts.UnitID = *unitID
	}
	if opts.UnitID == 0 {
		return nil, errors.New("unit ID 0 is the Modbus broadcast address: use 1 to 255")
	}
	if f.BaseAddress != nil {
		opts.BaseAddress = *f.BaseAddress
	}
	if baseAddress != nil {
		opts.BaseAddress = *baseAddress
	}
	if onWrite != nil {
		opts.OnWrite = serverWriteHook(onWrite)
	}
	return sunspec.NewServer(opts, models...)
}

// fileModelFromDecoded describes a model as it was read from a device. A
// model that decoded cleanly is described by its point values; any other one
// (no schema, shorter or longer than its schema, values that would not read
// back the same), and every model when raw is set, by its registers, so that
// serving the file reproduces the device exactly.
func fileModelFromDecoded(dm *sunspec.DecodedModel, raw bool) fileModel {
	id := dm.ModelID
	data := dm.RawRegisters
	// A model that went through the decoder carries its two header
	// registers; one that did not holds the data registers only.
	if dm.Schema != nil && len(data) >= 2 {
		data = data[2:]
	}
	rawModel := fileModel{ID: &id, Name: dm.Name, Registers: append([]uint16{}, data...)}
	if raw || dm.Group == nil || len(dm.Warnings) != 0 {
		return rawModel
	}
	m := fileModel{ID: &id, Name: dm.Name, fileGroup: groupFromValues(dm.Schema.Group, dm.Values())}
	if !reproduces(&m, dm) {
		return rawModel
	}
	return m
}

// groupFromValues lists the values of one group instance in schema order.
func groupFromValues(meta *registry.GroupMeta, v *sunspec.Values) fileGroup {
	var g fileGroup
	for _, pm := range meta.Points {
		if value, ok := v.Points[pm.Name]; ok {
			g.Points = append(g.Points, pointEntry{name: pm.Name, value: value})
		}
	}
	for _, child := range meta.Groups {
		for _, inst := range v.Groups[child.Name] {
			if g.Groups == nil {
				g.Groups = make(map[string][]*fileGroup)
			}
			cg := groupFromValues(child, inst)
			g.Groups[child.Name] = append(g.Groups[child.Name], &cg)
		}
	}
	return g
}

// reproduces reports whether m, written to a file and loaded again, encodes
// to a model that decodes to the same points as dm.
func reproduces(m *fileModel, dm *sunspec.DecodedModel) bool {
	text, err := json.Marshal(m)
	if err != nil {
		return false
	}
	file, err := parseDeviceFile([]byte(`{"models":[` + string(text) + `]}`))
	if err != nil {
		return false
	}
	values, err := file.Models[0].values("")
	if err != nil {
		return false
	}
	regs, err := sunspec.EncodeModel(dm.Schema, values)
	if err != nil || len(regs) != len(dm.RawRegisters) {
		return false
	}
	// The registers have the length of the device's, so they decode; the
	// decoded groups always marshal.
	again, _ := sunspec.DecodeModel(regs, dm.Schema, dm.InstanceAddress)
	want, _ := json.Marshal(dm.Group)
	got, _ := json.Marshal(again.Group)
	return bytes.Equal(want, got)
}

// templateModel describes a model with every point present and not
// implemented (null), and one instance of each nested group, as a starting
// point for a hand-written file.
func templateModel(meta *registry.ModelMeta) fileModel {
	id := meta.ID
	return fileModel{ID: &id, Name: modelName(id), fileGroup: templateGroup(meta.Group, true)}
}

func templateGroup(meta *registry.GroupMeta, top bool) fileGroup {
	g := fileGroup{Points: pointList{}}
	for _, pm := range meta.Points {
		// The ID and length registers are not values, and padding has none.
		if (top && pm.Offset < 2) || pm.Type == "pad" {
			continue
		}
		g.Points = append(g.Points, pointEntry{name: pm.Name})
	}
	for _, child := range meta.Groups {
		if g.Groups == nil {
			g.Groups = make(map[string][]*fileGroup)
		}
		cg := templateGroup(child, false)
		g.Groups[child.Name] = []*fileGroup{&cg}
	}
	return g
}

// writeDeviceFile writes the file as indented JSON, to the named file or,
// when path is empty, to out.
func writeDeviceFile(out io.Writer, path string, file *deviceFile) error {
	var buf bytes.Buffer
	if err := printJSON(&buf, file); err != nil {
		return err
	}
	if path == "" {
		_, err := out.Write(buf.Bytes())
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}
