package ember

import "testing"

func TestMachineTableLayoutsShareStablePropertyOffsets(t *testing.T) {
	var arena machineTableArena
	first, err := arena.newTableStopped(0, 2)
	if err != nil {
		t.Fatal(err)
	}
	second, err := arena.newTableStopped(0, 2)
	if err != nil {
		t.Fatal(err)
	}
	alpha := machineTableStringKey(1)
	beta := machineTableStringKey(2)
	for _, id := range []machineTableID{first, second} {
		if err := arena.rawSetStopped(id, alpha, slotBool(true), 0); err != nil {
			t.Fatal(err)
		}
		if err := arena.rawSetStopped(id, beta, slotBool(true), 0); err != nil {
			t.Fatal(err)
		}
	}
	firstLayout, firstRaw, firstMeta, err := arena.tableLayout(first)
	if err != nil {
		t.Fatal(err)
	}
	secondLayout, _, _, err := arena.tableLayout(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstLayout == machineTableDynamicLayout || firstLayout != secondLayout {
		t.Fatalf("equal insertion layouts = %d/%d, want one stable shared ID", firstLayout, secondLayout)
	}
	offset, ok := arena.layoutPropertyOffset(firstLayout, beta)
	if !ok {
		t.Fatal("stable layout has no beta offset")
	}
	value, ok := arena.getAtLayoutOffset(first, firstLayout, beta, offset)
	if !ok || value != slotBool(true) {
		t.Fatalf("guarded property read = %v/%t, want true/true", value, ok)
	}
	if err := arena.rawSetStopped(first, beta, slotBool(false), 0); err != nil {
		t.Fatal(err)
	}
	updatedLayout, updatedRaw, updatedMeta, err := arena.tableLayout(first)
	if err != nil {
		t.Fatal(err)
	}
	if updatedLayout != firstLayout || updatedRaw == firstRaw || updatedMeta != firstMeta {
		t.Fatalf("value update layout/raw/meta = %d/%d/%d, want %d/changed/%d", updatedLayout, updatedRaw, updatedMeta, firstLayout, firstMeta)
	}
	value, ok = arena.getAtLayoutOffset(first, firstLayout, beta, offset)
	if !ok || value != slotBool(false) {
		t.Fatalf("guarded property update read = %v/%t, want false/true", value, ok)
	}
}

func TestMachineTableLayoutInvalidatesOnDeleteAndMetatableGuardsSeparately(t *testing.T) {
	var arena machineTableArena
	table, err := arena.newTableStopped(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	metatable, err := arena.newTableStopped(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	key := machineTableStringKey(1)
	if err := arena.rawSetStopped(table, key, slotBool(true), 0); err != nil {
		t.Fatal(err)
	}
	layout, rawVersion, metaVersion, err := arena.tableLayout(table)
	if err != nil {
		t.Fatal(err)
	}
	if err := arena.setMetatableStopped(table, metatable); err != nil {
		t.Fatal(err)
	}
	afterMetaLayout, afterMetaRaw, afterMetaVersion, err := arena.tableLayout(table)
	if err != nil {
		t.Fatal(err)
	}
	if afterMetaLayout != layout || afterMetaRaw != rawVersion || afterMetaVersion == metaVersion {
		t.Fatalf("metatable change layout/raw/meta = %d/%d/%d, want %d/%d/changed", afterMetaLayout, afterMetaRaw, afterMetaVersion, layout, rawVersion)
	}
	if err := arena.rawSetStopped(table, key, slotNil, 0); err != nil {
		t.Fatal(err)
	}
	deletedLayout, _, _, err := arena.tableLayout(table)
	if err != nil {
		t.Fatal(err)
	}
	if deletedLayout != machineTableDynamicLayout {
		t.Fatalf("deleted layout = %d, want dynamic", deletedLayout)
	}
	if _, ok := arena.getAtLayoutOffset(table, layout, key, 0); ok {
		t.Fatal("stale layout guard read a deleted property")
	}
}
