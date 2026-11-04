package fixtures

import (
	"bytes"
	"encoding/binary"
)

// ELF builds a minimal ELF shared object whose dynamic section lists needed
// as DT_NEEDED entries, in order. It is little-endian x86-64 (class64) or
// i386 (otherwise) and holds no code: just enough for debug/elf to read the
// imported libraries. A hand-built fixture, a few hundred bytes, in place of
// a checked-in binary.
func ELF(class64 bool, needed ...string) []byte {
	le := binary.LittleEndian

	// Section data.
	dynstr := []byte{0}
	offsets := make([]uint32, len(needed))
	for i, n := range needed {
		offsets[i] = uint32(len(dynstr))
		dynstr = append(append(dynstr, n...), 0)
	}
	shstrtab := []byte("\x00.dynstr\x00.dynamic\x00.shstrtab\x00")
	const nameDynstr, nameDynamic, nameShstrtab = 1, 9, 18

	word := 4
	ehdrSize, shdrSize := 52, 40
	if class64 {
		word, ehdrSize, shdrSize = 8, 64, 64
	}
	entry := func(tag, val uint64) []byte {
		b := make([]byte, 2*word)
		if class64 {
			le.PutUint64(b, tag)
			le.PutUint64(b[8:], val)
		} else {
			le.PutUint32(b, uint32(tag))
			le.PutUint32(b[4:], uint32(val))
		}
		return b
	}
	var dynamic []byte
	for _, o := range offsets {
		dynamic = append(dynamic, entry(1, uint64(o))...) // DT_NEEDED
	}
	dynamic = append(dynamic, entry(5, 0)...)                    // DT_STRTAB
	dynamic = append(dynamic, entry(10, uint64(len(dynstr)))...) // DT_STRSZ
	dynamic = append(dynamic, entry(0, 0)...)                    // DT_NULL

	dynstrOff := ehdrSize
	dynamicOff := dynstrOff + len(dynstr)
	dynamicOff += -dynamicOff & 7 // align
	shstrOff := dynamicOff + len(dynamic)
	shOff := shstrOff + len(shstrtab)
	shOff += -shOff & 7

	var b bytes.Buffer
	// e_ident, then the rest of the header.
	ident := []byte{0x7f, 'E', 'L', 'F', 2, 1, 1, 0}
	machine := uint16(62) // EM_X86_64
	if !class64 {
		ident[4], machine = 1, 3 // ELFCLASS32, EM_386
	}
	b.Write(ident)
	b.Write(make([]byte, 8))
	put := func(v any) { _ = binary.Write(&b, le, v) }
	put(uint16(3)) // ET_DYN
	put(machine)
	put(uint32(1)) // EV_CURRENT
	if class64 {
		put(uint64(0)) // e_entry
		put(uint64(0)) // e_phoff
		put(uint64(shOff))
	} else {
		put(uint32(0))
		put(uint32(0))
		put(uint32(shOff))
	}
	put(uint32(0))        // e_flags
	put(uint16(ehdrSize)) // e_ehsize
	put(uint16(0))        // e_phentsize
	put(uint16(0))        // e_phnum
	put(uint16(shdrSize)) // e_shentsize
	put(uint16(4))        // e_shnum
	put(uint16(3))        // e_shstrndx

	pad := func(to int) { b.Write(make([]byte, to-b.Len())) }
	pad(dynstrOff)
	b.Write(dynstr)
	pad(dynamicOff)
	b.Write(dynamic)
	b.Write(shstrtab)
	pad(shOff)

	section := func(name, typ uint32, flags uint64, off, size int, link uint32, entsize int) {
		put(name)
		put(typ)
		if class64 {
			put(flags)
			put(uint64(0)) // sh_addr
			put(uint64(off))
			put(uint64(size))
		} else {
			put(uint32(flags))
			put(uint32(0))
			put(uint32(off))
			put(uint32(size))
		}
		put(link)
		put(uint32(0)) // sh_info
		if class64 {
			put(uint64(1)) // sh_addralign
			put(uint64(entsize))
		} else {
			put(uint32(1))
			put(uint32(entsize))
		}
	}
	section(0, 0, 0, 0, 0, 0, 0)                                    // null
	section(nameDynstr, 3, 2, dynstrOff, len(dynstr), 0, 0)         // SHT_STRTAB
	section(nameDynamic, 6, 3, dynamicOff, len(dynamic), 1, 2*word) // SHT_DYNAMIC, link .dynstr
	section(nameShstrtab, 3, 0, shstrOff, len(shstrtab), 0, 0)      // SHT_STRTAB
	return b.Bytes()
}
