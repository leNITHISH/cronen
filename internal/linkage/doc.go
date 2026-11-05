// Package linkage finds broken dynamic linkage: ELF files of installed
// packages that list a shared library (DT_NEEDED) the dynamic loader can't
// find, which is what a partial upgrade leaves behind.
//
// Resolution (Searcher) is pure and mirrors the loader's order: the file's
// RUNPATH/RPATH with $ORIGIN expanded, then the directories of
// /etc/ld.so.conf and /etc/ld.so.conf.d/*, then /usr/lib (and /usr/lib32 for
// 32-bit ELF). The ELF files are read with rebuild.ReadELF, the reader M8
// wrote, through system.Opener; results are cached per package version.
package linkage
