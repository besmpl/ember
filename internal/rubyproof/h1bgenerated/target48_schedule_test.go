package h1bgenerated

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"testing"
)

type target48Schedule struct {
	capture, seed                               string
	warmupHit, warmupCollector, warmupFamily    string
	primaryHit, primaryCollector, primaryFamily string
	reserveHit, reserveCollector, reserveFamily string
	wantSHA256                                  string
}

type target48PairCoordinate struct {
	slot   int
	family target48Family
	order  target48Order
}

var target48Schedules = [...]target48Schedule{
	{
		capture: "A", seed: "8e9323e9bcf267050598ec1250b516b1fa991184ca54d2e53d7013207389e60b",
		warmupHit: "CCEE", warmupCollector: "EECC", warmupFamily: "HGGH",
		primaryHit:       "CECEECCEECCEEECCEECCCEECEECCEC",
		primaryCollector: "ECECCEECCCEEECECECECCEECCEECCE",
		primaryFamily:    "HGHGGHHGHGHGHGGHHGGHHHGGGHHGGH",
		reserveHit:       "CCEE", reserveCollector: "CCEE", reserveFamily: "HGGH",
		wantSHA256: "196380b419f5de65c0187e777ecb60dca4b9ca26d249a0b685fc57b2dbc28c42",
	},
	{
		capture: "B", seed: "839f5583ba1d5c1faf15cce961e7bc43e06508f262a19eafa0b37f684072e377",
		warmupHit: "CEEC", warmupCollector: "CECE", warmupFamily: "HGHG",
		primaryHit:       "ECECCECEEECCECECECCEECECCECECE",
		primaryCollector: "CEECECCECECECCEEECECCEECCCEECE",
		primaryFamily:    "HHGGGHGHHGGHHGGHHGGHHGHGGHHGHG",
		reserveHit:       "CECE", reserveCollector: "ECCE", reserveFamily: "GGHH",
		wantSHA256: "5ae5093a80df1a0f563143f0922aecdbffea731ec92f334c920e93613bf38898",
	},
}

func (schedule target48Schedule) canonical() []byte {
	return []byte(fmt.Sprintf("target48-schedule-v1\n"+
		"capture=%s\nseed=%s\n"+
		"warmup.hit=%s\nwarmup.collector=%s\nwarmup.family=%s\n"+
		"primary.hit=%s\nprimary.collector=%s\nprimary.family=%s\n"+
		"reserve.hit=%s\nreserve.collector=%s\nreserve.family=%s\n",
		schedule.capture, schedule.seed,
		schedule.warmupHit, schedule.warmupCollector, schedule.warmupFamily,
		schedule.primaryHit, schedule.primaryCollector, schedule.primaryFamily,
		schedule.reserveHit, schedule.reserveCollector, schedule.reserveFamily))
}

func target48UniquePermutations(symbols string) []string {
	runes := []byte(symbols)
	sort.Slice(runes, func(i, j int) bool { return runes[i] < runes[j] })
	var permutations []string
	var visit func(int)
	visit = func(index int) {
		if index == len(runes) {
			candidate := string(runes)
			if len(permutations) == 0 || permutations[len(permutations)-1] != candidate {
				permutations = append(permutations, candidate)
			}
			return
		}
		seen := [256]bool{}
		for swap := index; swap < len(runes); swap++ {
			if seen[runes[swap]] {
				continue
			}
			seen[runes[swap]] = true
			runes[index], runes[swap] = runes[swap], runes[index]
			visit(index + 1)
			runes[index], runes[swap] = runes[swap], runes[index]
		}
	}
	visit(0)
	sort.Strings(permutations)
	return permutations
}

func target48ScheduleChoice(seed []byte, domain, symbols string) string {
	candidates := target48UniquePermutations(symbols)
	best := candidates[0]
	bestKey := [sha256.Size]byte{}
	for index, candidate := range candidates {
		input := make([]byte, 0, len(seed)+len(domain)+len(candidate)+2)
		input = append(input, seed...)
		input = append(input, 0)
		input = append(input, domain...)
		input = append(input, 0)
		input = append(input, candidate...)
		key := sha256.Sum256(input)
		if index == 0 || bytes.Compare(key[:], bestKey[:]) < 0 {
			best, bestKey = candidate, key
		}
	}
	return best
}

func target48RegenerateSchedule(schedule target48Schedule) (target48Schedule, error) {
	seed, err := hex.DecodeString(schedule.seed)
	if err != nil || len(seed) != sha256.Size {
		return target48Schedule{}, fmt.Errorf("target48: invalid schedule seed")
	}
	choice := func(domain, symbols string) string { return target48ScheduleChoice(seed, domain, symbols) }
	regenerated := target48Schedule{capture: schedule.capture, seed: schedule.seed, wantSHA256: schedule.wantSHA256}
	regenerated.warmupHit = choice("warmup/hit-order", "CCEE")
	regenerated.warmupCollector = choice("warmup/collector-order", "CCEE")
	regenerated.warmupFamily = choice("warmup/family-order", "HHGG")
	for block := 1; block <= 7; block++ {
		prefix := fmt.Sprintf("primary/block-%02d/", block)
		regenerated.primaryHit += choice(prefix+"hit-order", "CCEE")
		regenerated.primaryCollector += choice(prefix+"collector-order", "CCEE")
		regenerated.primaryFamily += choice(prefix+"family-order", "HHGG")
	}
	regenerated.primaryHit += choice("primary/terminal/hit-order", "CE")
	regenerated.primaryCollector += choice("primary/terminal/collector-order", "CE")
	regenerated.primaryFamily += choice("primary/terminal/family-order", "HG")
	regenerated.reserveHit = choice("reserve/hit-order", "CCEE")
	regenerated.reserveCollector = choice("reserve/collector-order", "CCEE")
	regenerated.reserveFamily = choice("reserve/family-order", "HHGG")
	return regenerated, nil
}

func target48ValidateSchedule(schedule target48Schedule) error {
	if schedule.capture != "A" && schedule.capture != "B" {
		return fmt.Errorf("target48: invalid capture %q", schedule.capture)
	}
	sections := []struct {
		name, value, alphabet string
		length                int
	}{
		{"warmup.hit", schedule.warmupHit, "CE", 4},
		{"warmup.collector", schedule.warmupCollector, "CE", 4},
		{"warmup.family", schedule.warmupFamily, "HG", 4},
		{"primary.hit", schedule.primaryHit, "CE", 30},
		{"primary.collector", schedule.primaryCollector, "CE", 30},
		{"primary.family", schedule.primaryFamily, "HG", 30},
		{"reserve.hit", schedule.reserveHit, "CE", 4},
		{"reserve.collector", schedule.reserveCollector, "CE", 4},
		{"reserve.family", schedule.reserveFamily, "HG", 4},
	}
	for _, section := range sections {
		if len(section.value) != section.length || strings.Count(section.value, string(section.alphabet[0])) != section.length/2 ||
			strings.Count(section.value, string(section.alphabet[1])) != section.length/2 {
			return fmt.Errorf("target48: %s is not balanced length %d", section.name, section.length)
		}
		for _, symbol := range section.value {
			if !strings.ContainsRune(section.alphabet, symbol) {
				return fmt.Errorf("target48: %s has invalid symbol %q", section.name, symbol)
			}
		}
	}
	for _, section := range sections[3:6] {
		for block := 0; block < 7; block++ {
			value := section.value[block*4 : block*4+4]
			if strings.Count(value, string(section.alphabet[0])) != 2 || strings.Count(value, string(section.alphabet[1])) != 2 {
				return fmt.Errorf("target48: %s block %d is not 2/2", section.name, block+1)
			}
		}
		terminal := section.value[28:]
		if terminal[0] == terminal[1] {
			return fmt.Errorf("target48: %s terminal block is not 1/1", section.name)
		}
	}
	return nil
}

func target48OrderFromSymbol(symbol byte) target48Order {
	if symbol == 'C' {
		return target48CurrentEquivalent
	}
	return target48EquivalentCurrent
}

func target48PrimaryPairs(schedule target48Schedule) []target48PairCoordinate {
	pairs := make([]target48PairCoordinate, 0, 2*target48PairCount)
	for slot := 0; slot < target48PairCount; slot++ {
		hit := target48PairCoordinate{slot: slot, family: target48HitFamily, order: target48OrderFromSymbol(schedule.primaryHit[slot])}
		collector := target48PairCoordinate{slot: slot, family: target48CollectorFamily, order: target48OrderFromSymbol(schedule.primaryCollector[slot])}
		if schedule.primaryFamily[slot] == 'H' {
			pairs = append(pairs, hit, collector)
		} else {
			pairs = append(pairs, collector, hit)
		}
	}
	return pairs
}

func target48ReservePairs(schedule target48Schedule) []target48PairCoordinate {
	pairs := make([]target48PairCoordinate, 0, 8)
	for slot := 0; slot < 4; slot++ {
		hit := target48PairCoordinate{slot: slot, family: target48HitFamily, order: target48OrderFromSymbol(schedule.reserveHit[slot])}
		collector := target48PairCoordinate{slot: slot, family: target48CollectorFamily, order: target48OrderFromSymbol(schedule.reserveCollector[slot])}
		if schedule.reserveFamily[slot] == 'H' {
			pairs = append(pairs, hit, collector)
		} else {
			pairs = append(pairs, collector, hit)
		}
	}
	return pairs
}

func target48SelectReserves(schedule target48Schedule, invalid []target48PairCoordinate) ([]target48PairCoordinate, error) {
	if err := target48ValidateSchedule(schedule); err != nil {
		return nil, err
	}
	if len(invalid) > 2 {
		return nil, fmt.Errorf("target48: %d invalid primary pairs exceeds replacement cap 2", len(invalid))
	}
	primary := target48PrimaryPairs(schedule)
	seenInvalid := make(map[target48PairCoordinate]bool, len(invalid))
	for _, coordinate := range invalid {
		if seenInvalid[coordinate] {
			return nil, fmt.Errorf("target48: duplicate invalid primary coordinate")
		}
		seenInvalid[coordinate] = true
		found := false
		for _, expected := range primary {
			found = found || coordinate == expected
		}
		if !found {
			return nil, fmt.Errorf("target48: invalid pair is not a scheduled primary")
		}
	}
	reserves := target48ReservePairs(schedule)
	used := make([]bool, len(reserves))
	selected := make([]target48PairCoordinate, 0, len(invalid))
	for _, rejected := range invalid {
		matched := false
		for index, reserve := range reserves {
			if !used[index] && reserve.family == rejected.family && reserve.order == rejected.order {
				used[index], matched = true, true
				selected = append(selected, reserve)
				break
			}
		}
		if !matched {
			return nil, fmt.Errorf("target48: no comparator/order-matched reserve")
		}
	}
	return selected, nil
}

func TestTarget48SchedulesAreExactBalancedAndRegenerable(t *testing.T) {
	if target48Schedules[0].seed == target48Schedules[1].seed || bytes.Equal(target48Schedules[0].canonical(), target48Schedules[1].canonical()) {
		t.Fatal("capture A and B schedules are not distinct")
	}
	for _, schedule := range target48Schedules {
		if err := target48ValidateSchedule(schedule); err != nil {
			t.Fatalf("capture %s: %v", schedule.capture, err)
		}
		if len(schedule.canonical()) != 356 {
			t.Fatalf("capture %s canonical schedule size = %d, want 356", schedule.capture, len(schedule.canonical()))
		}
		if digest := fmt.Sprintf("%x", sha256.Sum256(schedule.canonical())); digest != schedule.wantSHA256 {
			t.Fatalf("capture %s schedule SHA-256 = %s, want %s", schedule.capture, digest, schedule.wantSHA256)
		}
		regenerated, err := target48RegenerateSchedule(schedule)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(regenerated.canonical(), schedule.canonical()) {
			t.Fatalf("capture %s schedule does not reproduce from its seed:\ngot\n%s\nwant\n%s", schedule.capture, regenerated.canonical(), schedule.canonical())
		}
	}
}

func TestTarget48ScheduleMutationFailsClosed(t *testing.T) {
	mutated := target48Schedules[0]
	mutated.primaryHit = "E" + mutated.primaryHit[1:]
	if err := target48ValidateSchedule(mutated); err == nil {
		t.Fatal("imbalanced first primary block accepted")
	}
	mutated = target48Schedules[0]
	mutated.primaryFamily = mutated.primaryFamily[:29]
	if err := target48ValidateSchedule(mutated); err == nil {
		t.Fatal("short primary family schedule accepted")
	}
}

func TestTarget48ReserveSelectionIsMatchedAndBounded(t *testing.T) {
	for _, schedule := range target48Schedules {
		primaries := target48PrimaryPairs(schedule)
		for left := range primaries {
			for right := left; right < len(primaries); right++ {
				invalid := []target48PairCoordinate{primaries[left]}
				if right != left {
					invalid = append(invalid, primaries[right])
				}
				selected, err := target48SelectReserves(schedule, invalid)
				if err != nil {
					t.Fatalf("capture %s invalid %v: %v", schedule.capture, invalid, err)
				}
				for index := range invalid {
					if selected[index].family != invalid[index].family || selected[index].order != invalid[index].order {
						t.Fatalf("capture %s replacement %d = %#v, want family/order %#v", schedule.capture, index, selected[index], invalid[index])
					}
				}
			}
		}
		if _, err := target48SelectReserves(schedule, []target48PairCoordinate{primaries[0], primaries[1], primaries[2]}); err == nil {
			t.Fatalf("capture %s accepted a third invalid primary pair", schedule.capture)
		}
		if _, err := target48SelectReserves(schedule, []target48PairCoordinate{primaries[0], primaries[0]}); err == nil {
			t.Fatalf("capture %s accepted duplicate invalid pair", schedule.capture)
		}
		short := schedule
		short.primaryHit = short.primaryHit[:1]
		if _, err := target48SelectReserves(short, nil); err == nil {
			t.Fatalf("capture %s accepted a short schedule", schedule.capture)
		}
		invalidSymbol := schedule
		invalidSymbol.reserveHit = "X" + invalidSymbol.reserveHit[1:]
		if _, err := target48SelectReserves(invalidSymbol, nil); err == nil {
			t.Fatalf("capture %s accepted an invalid reserve symbol", schedule.capture)
		}
	}
}
