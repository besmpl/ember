package main

import (
	"debug/elf"
	"debug/macho"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
)

const (
	leafSymbol = "github.com/besmpl/ember.runMachineBurstArm64.abi0"
	backendMax = uint64(256 << 10)
)

type binaryReport struct {
	format   string
	leafSize uint64
}

func main() {
	binary := flag.String("binary", "", "linked Go binary to inspect")
	flag.Parse()
	if *binary == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: ember-machocheck --binary FILE")
		os.Exit(2)
	}
	report, err := inspectBinary(*binary)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ember-machocheck: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("machine backend: format=%s leaf_size=%d status=PASS\n", report.format, report.leafSize)
}

func inspectBinary(path string) (binaryReport, error) {
	mach, machErr := macho.Open(path)
	if machErr == nil {
		defer mach.Close()
		return inspectMachO(mach)
	}
	executable, elfErr := elf.Open(path)
	if elfErr == nil {
		defer executable.Close()
		return inspectELF(executable)
	}
	return binaryReport{}, fmt.Errorf("unsupported binary format (Mach-O: %v; ELF: %v)", machErr, elfErr)
}

func inspectMachO(file *macho.File) (binaryReport, error) {
	if file == nil || file.Cpu != macho.CpuArm64 {
		return binaryReport{}, errors.New("Mach-O target is not arm64")
	}
	for _, load := range file.Loads {
		segment, ok := load.(*macho.Segment)
		if ok && segment.Prot&0x2 != 0 && segment.Prot&0x4 != 0 {
			return binaryReport{}, fmt.Errorf("Mach-O segment %q is writable and executable", segment.Name)
		}
	}
	if file.Symtab == nil {
		return binaryReport{}, errors.New("Mach-O symbol table is missing")
	}
	index, symbol, err := selectMachOSymbol(file.Symtab.Syms, leafSymbol)
	if err != nil {
		return binaryReport{}, err
	}
	if symbol.Sect == 0 || int(symbol.Sect) > len(file.Sections) {
		return binaryReport{}, errors.New("leaf symbol has no valid section")
	}
	section := file.Sections[symbol.Sect-1]
	size := machOSymbolSize(file.Symtab.Syms, index, symbol, section)
	if size == 0 || size > backendMax {
		return binaryReport{}, fmt.Errorf("leaf text size %d is outside 1..%d bytes", size, backendMax)
	}
	start := symbol.Value - section.Addr
	for _, relocation := range section.Relocs {
		address := uint64(relocation.Addr)
		if address >= start && address < start+size {
			return binaryReport{}, fmt.Errorf("leaf contains relocation at section offset %#x", address)
		}
	}
	return binaryReport{format: "macho-arm64", leafSize: size}, nil
}

func selectMachOSymbol(symbols []macho.Symbol, name string) (int, macho.Symbol, error) {
	index := -1
	var selected macho.Symbol
	for candidateIndex, symbol := range symbols {
		if strings.TrimPrefix(symbol.Name, "_") != name {
			continue
		}
		if index >= 0 {
			return 0, macho.Symbol{}, fmt.Errorf("leaf symbol %q is duplicated", name)
		}
		index, selected = candidateIndex, symbol
	}
	if index < 0 {
		return 0, macho.Symbol{}, fmt.Errorf("leaf symbol %q is missing", name)
	}
	return index, selected, nil
}

func machOSymbolSize(symbols []macho.Symbol, index int, symbol macho.Symbol, section *macho.Section) uint64 {
	end := section.Addr + section.Size
	for candidateIndex, candidate := range symbols {
		if candidateIndex == index || candidate.Sect != symbol.Sect || candidate.Value <= symbol.Value || candidate.Value >= end {
			continue
		}
		end = candidate.Value
	}
	return end - symbol.Value
}

func inspectELF(file *elf.File) (binaryReport, error) {
	if file == nil || file.Machine != elf.EM_AARCH64 {
		return binaryReport{}, errors.New("ELF target is not arm64")
	}
	for _, program := range file.Progs {
		if program.Flags&elf.PF_W != 0 && program.Flags&elf.PF_X != 0 {
			return binaryReport{}, errors.New("ELF contains a writable-executable segment")
		}
	}
	symbols, err := file.Symbols()
	if err != nil {
		return binaryReport{}, fmt.Errorf("read ELF symbols: %w", err)
	}
	for _, symbol := range symbols {
		if symbol.Name == leafSymbol {
			return binaryReport{}, errors.New("Darwin/arm64 leaf leaked into portable ELF build")
		}
	}
	return binaryReport{format: "elf-arm64"}, nil
}
