// Copyright (c) 2018 Timo Savola. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package rewrite

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"

	"github.com/tsavola/wag"
	"github.com/tsavola/wag/internal/module"
	"github.com/tsavola/wag/internal/section"
	"github.com/tsavola/wag/types"
	"github.com/tsavola/wag/wasm"
)

var typeEncodings = map[types.T]int8{
	types.I32: 0x7f,
	types.I64: 0x7e,
	types.F32: 0x7d,
	types.F64: 0x7c,
}

type export struct {
	name  string
	kind  module.ExternalKind
	index uint32
}

func findSigIndex(m *wag.Module, needle types.Function) uint32 {
	for i, hay := range m.Sigs {
		if hay.Equal(needle) {
			return uint32(i)
		}
	}
	panic("a function signature is missing from module")
}

func writeUint32(b *bytes.Buffer, x uint32) {
	binary.Write(b, binary.LittleEndian, x)
}

func writeUint64(b *bytes.Buffer, x uint64) {
	binary.Write(b, binary.LittleEndian, x)
}

func writeVarint7(b *bytes.Buffer, x int8) {
	writeVarint64(b, int64(x))
}

func writeVarint32(b *bytes.Buffer, x int32) {
	writeVarint64(b, int64(x))
}

func writeVarint64(b *bytes.Buffer, x int64) {
	for {
		septet := uint8(x) & 0x7f
		x >>= 7
		if (x+1)&^1 == 0 { // only the sign remains
			b.WriteByte(septet)
			break
		}
		b.WriteByte(septet | 0x80)
	}
}

func writeVaruint1(b *bytes.Buffer, x bool) {
	if x {
		b.WriteByte(1)
	} else {
		b.WriteByte(0)
	}
}

func writeVaruint32(b *bytes.Buffer, x uint32) {
	var buf [binary.MaxVarintLen32]byte
	n := binary.PutUvarint(buf[:], uint64(x))
	b.Write(buf[:n])
}

func writeString(b *bytes.Buffer, x string) {
	writeVaruint32(b, uint32(len(x)))
	b.WriteString(x)
}

func writeConstOp(b *bytes.Buffer, t types.T, value uint64) {
	switch t {
	case types.I32:
		b.WriteByte(0x41) // opcode: i32.const
		writeVarint32(b, int32(value))

	case types.I64:
		b.WriteByte(0x42) // opcode: i64.const
		writeVarint64(b, int64(value))

	case types.F32:
		b.WriteByte(0x43) // opcode: f32.const
		writeUint32(b, uint32(value))

	case types.F64:
		b.WriteByte(0x44) // opcode: f64.const
		writeUint64(b, value)

	default:
		panic("invalid scalar type")
	}
}

func writeInitExpr(b *bytes.Buffer, t types.T, value uint64) {
	writeConstOp(b, t, value)
	b.WriteByte(0x0b) // opcode: end
}

func writePreliminarySections(w io.Writer, m *wag.Module, imports *envImports, exports []export) error {
	b := new(bytes.Buffer)

	binary.Write(b, binary.LittleEndian, module.Header{
		MagicNumber: module.MagicNumber,
		Version:     module.Version,
	})

	writeSection := func(sectionType byte, writePayload func()) {
		b.WriteByte(sectionType)

		sizeOffset := b.Len()
		b.Write(make([]byte, binary.MaxVarintLen32)) // placeholder

		payloadOffset := b.Len()
		writePayload()
		payloadSize := b.Len() - payloadOffset

		// fill in size and move payload backwards
		data := b.Bytes()[sizeOffset:]
		n := binary.PutUvarint(data, uint64(payloadSize))
		copy(data[n:], data[binary.MaxVarintLen32:])
		b.Truncate(sizeOffset + n + payloadSize)
	}

	if len(m.Sigs) > 0 {
		writeSection(module.SectionType, func() {
			writeVaruint32(b, uint32(len(m.Sigs)))

			for _, sig := range m.Sigs {
				writeVarint7(b, -0x20) // form
				writeVaruint32(b, uint32(len(sig.Args)))

				for _, t := range sig.Args {
					writeVarint7(b, typeEncodings[t])
				}

				if sig.Result == types.Void {
					writeVaruint1(b, false)
				} else {
					writeVaruint1(b, true)
					writeVarint7(b, typeEncodings[sig.Result])
				}
			}
		})
	}

	if count := len(m.ImportFuncs) + m.NumImportGlobals; count > 0 {
		writeSection(module.SectionImport, func() {
			writeVaruint32(b, uint32(count))

			for _, imp := range imports.funcs {
				writeString(b, imp.module)
				writeString(b, imp.field)
				b.WriteByte(byte(module.ExternalKindFunction))
				writeVaruint32(b, findSigIndex(m, imp.sig))
			}

			for i, imp := range imports.globals {
				writeString(b, imp.module)
				writeString(b, imp.field)
				b.WriteByte(byte(module.ExternalKindGlobal))
				writeVarint7(b, typeEncodings[imp.t])
				writeVaruint1(b, m.Globals[i].Mutable)
			}
		})
	}

	if count := len(m.FuncSigs) - len(imports.funcs); count > 0 {
		writeSection(module.SectionFunction, func() {
			writeVaruint32(b, uint32(count))

			for _, sigIndex := range m.FuncSigs[len(imports.funcs):] {
				writeVaruint32(b, sigIndex)
			}
		})
	}

	if m.TableLimitValues.Defined {
		writeSection(module.SectionTable, func() {
			writeVaruint32(b, 1)    // count
			writeVarint7(b, -0x10)  // element type
			writeVaruint1(b, false) // no maximum
			writeVaruint32(b, uint32(m.TableLimitValues.Initial))
		})
	}

	if m.MemoryLimitValues.Defined {
		writeSection(module.SectionMemory, func() {
			writeVaruint32(b, 1)    // count
			writeVaruint1(b, false) // no maximum
			writeVaruint32(b, uint32(m.MemoryLimitValues.Initial/int(wasm.Page)))
		})
	}

	if count := len(m.Globals) - m.NumImportGlobals; count > 0 {
		writeSection(module.SectionGlobal, func() {
			writeVaruint32(b, uint32(count))

			for _, g := range m.Globals[m.NumImportGlobals:] {
				writeVarint7(b, typeEncodings[g.Type])
				writeVaruint1(b, g.Mutable)
				writeInitExpr(b, g.Type, g.Init)
			}
		})
	}

	if len(exports) > 0 {
		writeSection(module.SectionExport, func() {
			writeVaruint32(b, uint32(len(exports)))

			for _, exp := range exports {
				writeString(b, exp.name)
				b.WriteByte(byte(exp.kind))
				writeVaruint32(b, exp.index)
			}
		})
	}

	if m.StartDefined {
		writeSection(module.SectionStart, func() {
			writeVaruint32(b, m.StartIndex)
		})
	}

	if len(m.TableFuncs) > 0 {
		writeSection(module.SectionElement, func() {
			writeVaruint32(b, 1) // count

			writeVaruint32(b, 0) // table index

			b.WriteByte(0x41)   // opcode: i32.const
			writeVarint32(b, 0) // offset
			b.WriteByte(0x0b)   // opcode: end

			writeVaruint32(b, uint32(len(m.TableFuncs))) // element count

			for _, elem := range m.TableFuncs {
				if elem >= uint32(len(m.FuncSigs)) {
					// TODO: disjoint ranges instead
					elem = 0
				}
				writeVaruint32(b, elem)
			}
		})
	}

	_, err := w.Write(b.Bytes())
	return err
}

func writeModule(w io.Writer, m *wag.Module, imports *envImports, exports []export, code []byte, tail module.Reader,
) (err error) {
	err = writePreliminarySections(w, m, imports, exports)
	if err != nil {
		return
	}

	_, err = w.Write(code)
	if err != nil {
		return
	}

	_, err = section.CopySpecific(w, tail, module.SectionData)
	return
}

// EntryFunction synthesizes a WebAssembly function which doesn't take any
// parameters.  It calls an existing target function with the specified
// arguments.  The entry function will return a value if the target function
// returns a value.
//
// The target function must have been exported.  The entry function will be the
// only exported symbol in the rewritten module.
func EntryFunction(w io.Writer, r module.Reader, entryName, targetName string, targetArgs []uint64,
) (err error) {
	m := &wag.Module{
		EntrySymbol: targetName,
		EntryArgs:   targetArgs,
	}

	env := new(envImports)

	err = m.LoadPreliminarySections(r, env)
	if err != nil {
		return
	}

	if !m.EntryDefined {
		err = errors.New("target function not found in export section")
		return
	}
	targetFuncIndex := m.EntryIndex
	targetSigIndex := m.FuncSigs[targetFuncIndex]
	targetSig := m.Sigs[targetSigIndex]
	result := targetSig.Result

	entrySigIndex := -1
	for i, sig := range m.Sigs {
		if len(sig.Args) == 0 && sig.Result == result {
			entrySigIndex = i
			break
		}
	}
	if entrySigIndex < 0 {
		entrySigIndex = len(m.Sigs)
		m.Sigs = append(m.Sigs, types.Function{Result: result})
	}
	entryFuncIndex := len(m.FuncSigs)
	m.FuncSigs = append(m.FuncSigs, uint32(entrySigIndex))

	oldCode := new(bytes.Buffer)
	ok, err := section.CopySpecific(oldCode, r, module.SectionCode)
	if err != nil {
		return
	}
	if !ok {
		err = errors.New("no code section")
		return
	}
	newCode := appendEntryFunctionCode(oldCode.Bytes(), targetFuncIndex, targetSig.Args, targetArgs)

	exports := []export{
		{entryName, module.ExternalKindFunction, uint32(entryFuncIndex)},
	}

	return writeModule(w, m, env, exports, newCode, r)
}

func appendEntryFunctionCode(oldSection []byte, callIndex uint32, params []types.T, args []uint64,
) []byte {
	newBody := new(bytes.Buffer)
	writeVaruint32(newBody, 0) // param group count
	for i, t := range params {
		writeConstOp(newBody, t, args[i])
	}
	newBody.WriteByte(0x10) // opcode: call
	writeVaruint32(newBody, callIndex)
	newBody.WriteByte(0x0b) // opcode: end

	_, n := binary.Uvarint(oldSection[1:]) // skip section type, decode payload size length
	oldPayload := oldSection[1+n:]

	oldBodyCount, n := binary.Uvarint(oldPayload)
	oldBodies := oldPayload[n:]

	newPayload := new(bytes.Buffer)
	writeVaruint32(newPayload, uint32(oldBodyCount+1))
	newPayload.Write(oldBodies)
	writeVaruint32(newPayload, uint32(newBody.Len()))
	newPayload.Write(newBody.Bytes())

	newSection := new(bytes.Buffer)
	newSection.WriteByte(oldSection[0]) // section type
	writeVaruint32(newSection, uint32(newPayload.Len()))
	newSection.Write(newPayload.Bytes())

	return newSection.Bytes()
}
