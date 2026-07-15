package ember

import (
	"fmt"
	"sort"
)

// programModuleID is the stable dense identifier assigned to one module in a
// prepared Program image. IDs are assigned from sorted module keys and are
// therefore independent of map iteration order.
type programModuleID uint32

// programImage is the immutable, owner-neutral inventory for one Program.
// Code is lowered once into pointer-neutral codeImage values; this image does
// not retain compiler Proto pointers or any runtime-owner state.
type programImage struct {
	modules     []programImageModule
	entrypoints []programImageEntrypoint
	moduleIDs   map[moduleKey]programModuleID
	globalNames []string
}

type programImageModule struct {
	moduleID   programModuleID
	key        moduleKey
	sourceName string
	code       *codeImage
}

type programImageEntrypoint struct {
	name     string
	moduleID programModuleID
}

func (p *Program) preparedProgramImage() (*programImage, error) {
	if p == nil {
		return nil, fmt.Errorf("prepare program image: nil program")
	}
	p.programImageOnce.Do(func() {
		p.programImage, p.programImageErr = prepareProgramImage(p)
	})
	return p.programImage, p.programImageErr
}

func prepareProgramImage(p *Program) (*programImage, error) {
	if p == nil {
		return nil, fmt.Errorf("prepare program image: nil program")
	}

	keys := programImageModuleKeys(p)
	moduleIDs := make(map[moduleKey]programModuleID, len(keys))
	modules := make([]programImageModule, 0, len(keys))
	for index, key := range keys {
		if uint64(index) > uint64(^programModuleID(0)) {
			return nil, fmt.Errorf("prepare program image: module count %d exceeds dense ID range", len(keys))
		}
		proto, ok := p.protos[key]
		if !ok || proto == nil {
			return nil, fmt.Errorf("prepare program image: missing prototype for module %s", key.String())
		}
		code, err := proto.preparedCodeImage()
		if err != nil {
			return nil, fmt.Errorf("prepare program image: module %s: %w", key.String(), err)
		}
		moduleID := programModuleID(index)
		moduleIDs[key] = moduleID
		node := p.graph.Nodes[key]
		modules = append(modules, programImageModule{
			moduleID:   moduleID,
			key:        key,
			sourceName: node.Source.Name,
			code:       code,
		})
	}

	// A graph is authoritative when present. Reject orphan prototypes rather
	// than silently leaving part of the Program outside its image inventory.
	if p.graph.Nodes != nil {
		for key := range p.protos {
			if _, ok := moduleIDs[key]; !ok {
				return nil, fmt.Errorf("prepare program image: prototype for unknown module %s", key.String())
			}
		}
	}

	entrypoints := make([]programImageEntrypoint, 0, len(p.entrypoints))
	for _, entrypoint := range p.entrypoints {
		moduleID, ok := moduleIDs[entrypoint.key]
		if !ok {
			return nil, fmt.Errorf("prepare program image: entrypoint %q references missing module %s", entrypoint.name, entrypoint.key.String())
		}
		entrypoints = append(entrypoints, programImageEntrypoint{
			name:     entrypoint.name,
			moduleID: moduleID,
		})
	}

	globalNames, err := programImageGlobalNames(modules)
	if err != nil {
		return nil, err
	}
	return &programImage{
		modules:     modules,
		entrypoints: entrypoints,
		moduleIDs:   moduleIDs,
		globalNames: globalNames,
	}, nil
}

func programImageGlobalNames(modules []programImageModule) ([]string, error) {
	unique := make(map[string]struct{})
	for moduleIndex, module := range modules {
		for _, id := range module.code.globalNames {
			name, ok := machineImageString(module.code, id)
			if !ok {
				return nil, fmt.Errorf("prepare program image: module %d has invalid global string ID %d", moduleIndex, id)
			}
			unique[name] = struct{}{}
		}
	}
	names := make([]string, 0, len(unique))
	for name := range unique {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func machineImageString(code *codeImage, id machineStringID) (string, bool) {
	if code == nil || id == invalidMachineStringID || uint64(id) > uint64(len(code.stringRecords)) {
		return "", false
	}
	record := code.stringRecords[id-1]
	end := uint64(record.offset) + uint64(record.length)
	if end > uint64(len(code.stringData)) {
		return "", false
	}
	return string(code.stringData[record.offset:uint32(end)]), true
}

func programImageModuleKeys(p *Program) []moduleKey {
	if p.graph.Nodes != nil {
		return sortedModuleKeys(p.graph.Nodes)
	}
	keys := make([]moduleKey, 0, len(p.protos))
	for key := range p.protos {
		keys = append(keys, key)
	}
	return sortModuleKeys(keys)
}

func sortModuleKeys(keys []moduleKey) []moduleKey {
	// Keep the caller's map-independent input untouched so the returned image
	// owns its own deterministic ordering.
	sorted := append([]moduleKey(nil), keys...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].String() < sorted[j].String()
	})
	return sorted
}
