package main

import (
	"debug/macho"
	"strings"
	"testing"
)

func TestInspectMachOAcceptsExactNoRelocationLeaf(t *testing.T) {
	file := machOFixture()
	report, err := inspectMachO(file)
	if err != nil {
		t.Fatal(err)
	}
	if report.format != "macho-arm64" || report.leafSize != 64 {
		t.Fatalf("report = %#v, want 64-byte Mach-O leaf", report)
	}
}

func TestInspectMachORejectsForbiddenClasses(t *testing.T) {
	tests := []struct {
		name   string
		change func(*macho.File)
		want   string
	}{
		{name: "missing leaf", change: func(file *macho.File) { file.Symtab.Syms[0].Name = "other" }, want: "missing"},
		{name: "duplicate leaf", change: func(file *macho.File) { file.Symtab.Syms = append(file.Symtab.Syms, file.Symtab.Syms[0]) }, want: "duplicated"},
		{name: "writable executable", change: func(file *macho.File) { file.Loads[0].(*macho.Segment).Prot = 0x7 }, want: "writable and executable"},
		{name: "relocation", change: func(file *macho.File) { file.Sections[0].Relocs = []macho.Reloc{{Addr: 4}} }, want: "relocation"},
		{name: "oversized", change: func(file *macho.File) { file.Sections[0].Size = backendMax + 1 }, want: "outside"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := machOFixture()
			test.change(file)
			_, err := inspectMachO(file)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("inspectMachO error = %v, want %q", err, test.want)
			}
		})
	}
}

func machOFixture() *macho.File {
	section := &macho.Section{SectionHeader: macho.SectionHeader{Name: "__text", Seg: "__TEXT", Addr: 0x1000, Size: 64}}
	return &macho.File{
		FileHeader: macho.FileHeader{Cpu: macho.CpuArm64},
		Loads:      []macho.Load{&macho.Segment{SegmentHeader: macho.SegmentHeader{Name: "__TEXT", Prot: 0x5}}},
		Sections:   []*macho.Section{section},
		Symtab:     &macho.Symtab{Syms: []macho.Symbol{{Name: "_" + leafSymbol, Sect: 1, Value: 0x1000}}},
	}
}
