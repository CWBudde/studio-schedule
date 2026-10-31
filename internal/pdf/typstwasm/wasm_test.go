//go:build !js

package typstwasm

import (
	"bytes"
	"slices"
)

// A minimal encoder for hand-crafted WebAssembly modules (binary format of
// the core spec, chapter 5): enough to give the renderer modules that trap,
// block or lie about their memory, deterministically and in microseconds.
// Every value is an i32.

// Instructions used by the crafted modules.
const (
	opUnreachable = 0x00
	opLoop        = 0x03 // loop <blocktype> … end
	opIf          = 0x04 // if <blocktype> … end
	opEnd         = 0x0b
	opBr          = 0x0c // br <label index>
	opCall        = 0x10 // call <function index>
	opDrop        = 0x1a
	opGlobalGet   = 0x23 // global.get <global index>
	opGlobalSet   = 0x24 // global.set <global index>
	opMemoryGrow  = 0x40 // memory.grow <memory index 0x00>: pages → old pages or -1
	opI32Const    = 0x41 // i32.const <signed LEB128>
	opI32Eqz      = 0x45 // i32.eqz: 1 if the operand is 0, else 0

	blockEmpty = 0x40 // block type of a loop or if without results
	typeI32    = 0x7f
)

// wasmFunc is a function with i32 parameters and results; name exports it.
type wasmFunc struct {
	name            string
	params, results int
	body            []byte // instructions without the final end
}

// wasmImport is an imported host function.
type wasmImport struct {
	module, name    string
	params, results int
}

// wasmModule is a module with one memory, exported as "memory". Imported
// functions take the first indices, funcs follow.
type wasmModule struct {
	imports []wasmImport
	funcs   []wasmFunc
	pages   uint32  // initial memory pages (64 KiB each), no maximum
	globals []int32 // mutable i32 globals and their initial values
	data    []wasmData
}

type wasmData struct {
	offset int32
	bytes  string
}

// renderOK is the PDF the crafted modules "render".
const renderOK = "%PDF-crafted"

// abiModule returns a module whose ABI functions all succeed: alloc gives
// address 1024, render status 0, the result is renderOK at address 0.
// Replace a function's body with set.
func abiModule() *wasmModule {
	return &wasmModule{
		pages: 1,
		funcs: []wasmFunc{
			{name: "alloc", params: 1, results: 1, body: i32(1024)},
			{name: "render", params: 2, results: 1, body: i32(0)},
			{name: "result_ptr", results: 1, body: i32(0)},
			{name: "result_len", results: 1, body: i32(int32(len(renderOK)))},
		},
		data: []wasmData{{0, renderOK}},
	}
}

// set replaces the body of the exported function name, or adds it as a
// function without parameters and results (e.g. _initialize).
func (m *wasmModule) set(name string, body ...[]byte) *wasmModule {
	code := bytes.Join(body, nil)

	for i := range m.funcs {
		if m.funcs[i].name == name {
			m.funcs[i].body = code

			return m
		}
	}

	m.funcs = append(m.funcs, wasmFunc{name: name, body: code})

	return m
}

// i32 is i32.const v.
func i32(v int32) []byte { return append([]byte{opI32Const}, sleb(int64(v))...) }

// op is a sequence of instruction bytes.
func op(b ...byte) []byte { return b }

func (m *wasmModule) encode() []byte {
	var types, imports, funcs, exports, globals, code, data [][]byte

	functype := func(params, results int) []byte {
		return slices.Concat([]byte{0x60}, vec(repeat(typeI32, params)...), vec(repeat(typeI32, results)...))
	}

	for i, imp := range m.imports {
		types = append(types, functype(imp.params, imp.results))
		imports = append(imports, slices.Concat(name(imp.module), name(imp.name), []byte{0x00}, uleb(uint64(i))))
	}

	for i, f := range m.funcs {
		idx := uint64(len(m.imports) + i)
		types = append(types, functype(f.params, f.results))
		funcs = append(funcs, uleb(idx)) // the type of the same index

		if f.name != "" {
			exports = append(exports, slices.Concat(name(f.name), []byte{0x00}, uleb(idx)))
		}

		body := slices.Concat([]byte{0x00}, f.body, []byte{opEnd}) // no locals
		code = append(code, slices.Concat(uleb(uint64(len(body))), body))
	}

	exports = append(exports, slices.Concat(name("memory"), []byte{0x02, 0x00}))

	for _, g := range m.globals {
		globals = append(globals, slices.Concat([]byte{typeI32, 0x01}, i32(g), []byte{opEnd}))
	}

	for _, d := range m.data {
		data = append(data, slices.Concat([]byte{0x00}, i32(d.offset), []byte{opEnd}, name(d.bytes)))
	}

	memory := [][]byte{slices.Concat([]byte{0x00}, uleb(uint64(m.pages)))} // limits: min only

	return slices.Concat(
		[]byte("\x00asm\x01\x00\x00\x00"), // magic, version 1
		section(1, types), section(2, imports), section(3, funcs), section(5, memory),
		section(6, globals), section(7, exports), section(10, code), section(11, data),
	)
}

// section encodes a section of the items; an empty one is left out.
func section(id byte, items [][]byte) []byte {
	if len(items) == 0 {
		return nil
	}

	content := vec(items...)

	return slices.Concat([]byte{id}, uleb(uint64(len(content))), content)
}

func vec(items ...[]byte) []byte {
	return slices.Concat(append([][]byte{uleb(uint64(len(items)))}, items...)...)
}

func name(s string) []byte { return slices.Concat(uleb(uint64(len(s))), []byte(s)) }

func repeat(b byte, n int) [][]byte {
	out := make([][]byte, n)
	for i := range out {
		out[i] = []byte{b}
	}

	return out
}

// uleb is unsigned LEB128: 7 bits per byte, low first, high bit = more.
func uleb(n uint64) []byte {
	var out []byte

	for {
		b := byte(n & 0x7f)
		if n >>= 7; n != 0 {
			b |= 0x80
		}

		out = append(out, b)

		if n == 0 {
			return out
		}
	}
}

// sleb is signed LEB128: as uleb, until the rest is the sign extension of
// the last byte's bit 6.
func sleb(n int64) []byte {
	var out []byte

	for {
		b := byte(n & 0x7f)
		n >>= 7

		if (n == 0 && b&0x40 == 0) || (n == -1 && b&0x40 != 0) {
			return append(out, b)
		}

		out = append(out, b|0x80)
	}
}
