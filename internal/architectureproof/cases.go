package architectureproof

import (
	"strconv"
)

type proofFunc func(seed int64) int64

type proofCase struct {
	id          string
	family      string
	luauBody    string
	wantFrozen  int64
	run         proofFunc
	sensitivity proofFunc
}

var proofCases = []proofCase{
	{
		id:         "top10/arithmetic_for",
		family:     "arithmetic_branches",
		wantFrozen: 1595,
		run:        arithmeticFor,
		luauBody: `
local total = seed % 7
for i = 1, 200 do
    total = total + ((i * 3 - i // 2) % 17)
end
return total
`,
	},
	{
		id:         "top10/table_fields",
		family:     "stable_fields_scalar_replacement",
		wantFrozen: 1860,
		run:        tableFields,
		luauBody: `
local player = {stats = {hp = 100 + seed % 7, shield = 25}, inventory = {coins = 3}}
local i = 0
while i < 80 do
    i = i + 1
    player.stats.hp = player.stats.hp + player.stats.shield - player.inventory.coins
end
return player.stats.hp
`,
	},
	{
		id:         "top10/generic_iteration",
		family:     "array_iteration",
		wantFrozen: 204,
		run:        genericIteration,
		luauBody: `
local values = {1 + seed % 5, 2, 3, 4, 5, 6, 7, 8}
local total = 0
for _, value in values do
    total = total + value * value
end
return total
`,
	},
	{
		id:         "top10/array_ops",
		family:     "array_growth_front_removal",
		wantFrozen: 135,
		run:        arrayOps,
		luauBody: `
local values = {}
for i = 1, 80 do
    table.insert(values, i % 9 + seed % 3)
end
local removed = 0
for i = 1, 20 do
    removed = removed + table.remove(values, 1)
end
return removed + rawlen(values)
`,
	},
	{
		id:          "scenario/signal_bus_callbacks",
		family:      "dynamic_calls_closures_upvalues",
		wantFrozen:  76620,
		run:         signalBusSpecialized,
		sensitivity: signalBusGoClosures,
		luauBody: `
local state = {hp = 120 + seed % 11, score = 0, armor = 3}
local function makeHandler(mult)
    local seen = 0
    return function(s, event)
        seen = seen + 1
        if event.kind == "damage" then
            s.hp = s.hp - event.amount * mult + s.armor
        elseif event.kind == "heal" then
            s.hp = s.hp + event.amount + mult
        else
            s.score = s.score + event.amount * mult
        end
        return seen + s.hp + s.score
    end
end
local handlers = {
    damage = {makeHandler(1), makeHandler(2)},
    heal = {makeHandler(1)},
    score = {makeHandler(1), makeHandler(3)},
}
local events = {
    {kind = "damage", amount = 7},
    {kind = "score", amount = 4},
    {kind = "heal", amount = 5},
    {kind = "damage", amount = 3},
}
local total = 0
for tick = 1, 45 do
    for _, event in events do
        local bucket = handlers[event.kind]
        for _, handler in bucket do
            total = total + handler(state, event)
        end
    end
end
return total + state.hp + state.score
`,
	},
	{
		id:         "classic/recursive_fibonacci",
		family:     "recursive_direct_calls",
		wantFrozen: 6765,
		run:        recursiveFibonacci,
		luauBody: `
local function fib(n)
    if n < 2 then
        return n
    end
    return fib(n - 1) + fib(n - 2)
end
return fib(20 + seed % 2)
`,
	},
	{
		id:         "top10/coroutine_yield",
		family:     "coroutine_state_machine",
		wantFrozen: 2585,
		run:        coroutineYield,
		luauBody: `
local co = coroutine.create(function(limit)
    local total = 0
    for i = 1, limit do
        total = total + i
        if i % 10 == 0 then
            coroutine.yield(total)
        end
    end
    return total
end)
local total = 0
local ok, value = coroutine.resume(co, 45 + seed % 3)
while coroutine.status(co) ~= "dead" do
    total = total + value
    ok, value = coroutine.resume(co)
end
return total + value
`,
	},
	{
		id:          "scenario/sparse_grid_neighbors",
		family:      "tables_iteration_generated_strings",
		wantFrozen:  -236651,
		run:         sparseGridPacked,
		sensitivity: sparseGridStrings,
		luauBody: `
local cells = {
    ["0:0"] = {terrain = 1, heat = 5 + seed % 5},
    ["1:0"] = {terrain = 2, heat = 3},
    ["2:1"] = {terrain = 1, heat = 7},
    ["3:2"] = {terrain = 3, heat = 2},
    ["4:4"] = {terrain = 2, heat = 9},
}
local offsets = {
    {dx = 1, dy = 0},
    {dx = -1, dy = 0},
    {dx = 0, dy = 1},
    {dx = 0, dy = -1},
}
local function cellKey(x, y)
    return tostring(x) .. ":" .. tostring(y)
end
local total = 0
for tick = 1, 36 do
    for x = 0, 4 do
        for y = 0, 4 do
            local key = cellKey(x, y)
            local center = cells[key]
            if center ~= nil then
                for _, offset in offsets do
                    local neighborKey = cellKey(x + offset.dx, y + offset.dy)
                    local neighbor = cells[neighborKey]
                    if neighbor ~= nil then
                        local flow = center.heat - neighbor.heat
                        if flow < 0 then flow = -flow end
                        center.heat = center.heat + tick % 3 - neighbor.terrain
                        total = total + flow + center.heat
                    elseif tick % 5 == 0 then
                        cells[neighborKey] = {terrain = tick % 3 + 1, heat = x + y + tick % 4}
                        total = total + cells[neighborKey].heat
                    end
                end
            end
        end
    end
end
return total + (cells["2:2"] and cells["2:2"].heat or 0) + (cells["4:4"] and cells["4:4"].heat or 0)
`,
	},
	{
		id:         "scenario/prototype_fallback",
		family:     "metatable_guarded_fallback",
		wantFrozen: 32379,
		run:        prototypeFallback,
		luauBody: `
local prototype = {hp = 20 + seed % 3, mana = 5, armor = 2}
local misses = 0
local mt = {
    __index = function(_, key)
        misses = misses + 1
        if key == "power" then
            return prototype.hp + prototype.mana
        end
        return prototype[key] or 0
    end,
}
local actors = {
    setmetatable({hp = 80}, mt),
    setmetatable({mana = 15, armor = 4}, mt),
    setmetatable({hp = 45, power = 9}, mt),
}
local total = 0
for tick = 1, 80 do
    for _, actor in actors do
        local value = actor.hp + actor.mana + actor.power
        if actor.armor > 3 then
            actor.hp = actor.hp + tick % 3 - actor.armor
        else
            actor.mana = actor.mana + tick % 4
        end
        total = total + value + actor.hp + actor.mana
    end
end
return total + misses
`,
	},
	{
		id:         "scenario/array_hole_compaction",
		family:     "mutating_array_iteration",
		wantFrozen: 31652,
		run:        arrayHoleCompaction,
		luauBody: `
local values = {}
for i = 1, 30 do
    values[i] = {score = i * 3 + seed % 2, live = true}
end
local total = 0
for tick = 1, 70 do
    local i = 1
    while i <= rawlen(values) do
        local row = values[i]
        row.score = row.score + tick % 6
        total = total + row.score
        if row.score % 13 == 0 then
            table.remove(values, i)
        else
            i = i + 1
        end
    end
    if tick % 5 == 0 then
        table.insert(values, {score = tick, live = true})
    end
end
return total + rawlen(values)
`,
	},
	{
		id:          "scenario/command_vararg_router",
		family:      "varargs_multi_return_dynamic_branch",
		wantFrozen:  824780,
		run:         commandVarargSpecialized,
		sensitivity: commandVarargSlices,
		luauBody: `
local state = {x = 0, y = 0, score = 0, gold = 10 + seed % 7}
local function apply(name, ...)
    if name == "move" then
        local dx, dy = ...
        state.x = state.x + dx
        state.y = state.y + dy
        return state.x, state.y, state.score
    elseif name == "loot" then
        local a, b, c = ...
        state.gold = state.gold + a + b + c
        state.score = state.score + state.gold
        return state.gold, state.score, select("#", ...)
    elseif name == "spend" then
        local amount = ...
        state.gold = state.gold - amount
        return state.gold, state.x, state.y
    else
        return state.score, state.gold, 0
    end
end
local commands = {
    {"move", 1, 2, 0},
    {"loot", 3, 4, 5},
    {"spend", 6, 0, 0},
    {"wait", 0, 0, 0},
}
local total = 0
for tick = 1, 60 do
    for _, command in commands do
        local a, b, c = apply(command[1], command[2] + tick % 3, command[3], command[4])
        total = total + a + b + c
    end
end
return total + state.x + state.y + state.score + state.gold
`,
	},
}

func arithmeticFor(seed int64) int64 {
	total := seed % 7
	for i := int64(1); i <= 200; i++ {
		total += (i*3 - i/2) % 17
	}
	return total
}

func tableFields(seed int64) int64 {
	hp := int64(100) + seed%7
	const shield = int64(25)
	const coins = int64(3)
	for range 80 {
		hp = hp + shield - coins
	}
	return hp
}

func genericIteration(seed int64) int64 {
	values := [...]int64{1 + seed%5, 2, 3, 4, 5, 6, 7, 8}
	var total int64
	for _, value := range values {
		total += value * value
	}
	return total
}

func arrayOps(seed int64) int64 {
	values := make([]int64, 0, 80)
	for i := int64(1); i <= 80; i++ {
		values = append(values, i%9+seed%3)
	}
	var removed int64
	for range 20 {
		removed += values[0]
		values = values[1:]
	}
	return removed + int64(len(values))
}

type signalKind uint8

const (
	signalDamage signalKind = iota
	signalHeal
	signalScore
)

type signalState struct {
	hp    int64
	score int64
	armor int64
}

type signalEvent struct {
	kind   signalKind
	amount int64
}

type signalHandler struct {
	mult int64
	seen int64
}

func (handler *signalHandler) call(state *signalState, event signalEvent) int64 {
	handler.seen++
	switch event.kind {
	case signalDamage:
		state.hp = state.hp - event.amount*handler.mult + state.armor
	case signalHeal:
		state.hp = state.hp + event.amount + handler.mult
	default:
		state.score += event.amount * handler.mult
	}
	return handler.seen + state.hp + state.score
}

func signalBusSpecialized(seed int64) int64 {
	state := signalState{hp: 120 + seed%11, armor: 3}
	damage := [...]signalHandler{{mult: 1}, {mult: 2}}
	heal := [...]signalHandler{{mult: 1}}
	score := [...]signalHandler{{mult: 1}, {mult: 3}}
	events := [...]signalEvent{
		{kind: signalDamage, amount: 7},
		{kind: signalScore, amount: 4},
		{kind: signalHeal, amount: 5},
		{kind: signalDamage, amount: 3},
	}
	var total int64
	for range 45 {
		for _, event := range events {
			switch event.kind {
			case signalDamage:
				for i := range damage {
					total += damage[i].call(&state, event)
				}
			case signalHeal:
				for i := range heal {
					total += heal[i].call(&state, event)
				}
			case signalScore:
				for i := range score {
					total += score[i].call(&state, event)
				}
			}
		}
	}
	return total + state.hp + state.score
}

func signalBusGoClosures(seed int64) int64 {
	type event struct {
		kind   string
		amount int64
	}
	type state struct {
		hp    int64
		score int64
		armor int64
	}
	current := state{hp: 120 + seed%11, armor: 3}
	makeHandler := func(mult int64) func(*state, event) int64 {
		var seen int64
		return func(current *state, event event) int64 {
			seen++
			switch event.kind {
			case "damage":
				current.hp = current.hp - event.amount*mult + current.armor
			case "heal":
				current.hp = current.hp + event.amount + mult
			default:
				current.score += event.amount * mult
			}
			return seen + current.hp + current.score
		}
	}
	handlers := map[string][]func(*state, event) int64{
		"damage": {makeHandler(1), makeHandler(2)},
		"heal":   {makeHandler(1)},
		"score":  {makeHandler(1), makeHandler(3)},
	}
	events := [...]event{
		{kind: "damage", amount: 7},
		{kind: "score", amount: 4},
		{kind: "heal", amount: 5},
		{kind: "damage", amount: 3},
	}
	var total int64
	for range 45 {
		for _, event := range events {
			for _, handler := range handlers[event.kind] {
				total += handler(&current, event)
			}
		}
	}
	return total + current.hp + current.score
}

func recursiveFibonacci(seed int64) int64 {
	return fib(20 + seed%2)
}

func fib(n int64) int64 {
	if n < 2 {
		return n
	}
	return fib(n-1) + fib(n-2)
}

type coroutineState struct {
	limit int64
	i     int64
	total int64
	dead  bool
}

func (state *coroutineState) resume() (int64, bool) {
	if state.dead {
		return 0, true
	}
	for state.i <= state.limit {
		state.total += state.i
		yielded := state.i%10 == 0
		state.i++
		if yielded {
			return state.total, false
		}
	}
	state.dead = true
	return state.total, true
}

func coroutineYield(seed int64) int64 {
	state := coroutineState{limit: 45 + seed%3, i: 1}
	value, dead := state.resume()
	var total int64
	for !dead {
		total += value
		value, dead = state.resume()
	}
	return total + value
}

type gridCell struct {
	terrain int64
	heat    int64
}

type gridOffset struct {
	dx int64
	dy int64
}

var gridOffsets = [...]gridOffset{
	{dx: 1},
	{dx: -1},
	{dy: 1},
	{dy: -1},
}

func gridPackedKey(x, y int64) uint64 {
	return uint64(uint32(int32(x)))<<32 | uint64(uint32(int32(y)))
}

func sparseGridPacked(seed int64) int64 {
	cells := map[uint64]*gridCell{
		gridPackedKey(0, 0): {terrain: 1, heat: 5 + seed%5},
		gridPackedKey(1, 0): {terrain: 2, heat: 3},
		gridPackedKey(2, 1): {terrain: 1, heat: 7},
		gridPackedKey(3, 2): {terrain: 3, heat: 2},
		gridPackedKey(4, 4): {terrain: 2, heat: 9},
	}
	var total int64
	for tick := int64(1); tick <= 36; tick++ {
		for x := int64(0); x <= 4; x++ {
			for y := int64(0); y <= 4; y++ {
				center := cells[gridPackedKey(x, y)]
				if center == nil {
					continue
				}
				for _, offset := range gridOffsets {
					neighborKey := gridPackedKey(x+offset.dx, y+offset.dy)
					neighbor := cells[neighborKey]
					if neighbor != nil {
						flow := center.heat - neighbor.heat
						if flow < 0 {
							flow = -flow
						}
						center.heat = center.heat + tick%3 - neighbor.terrain
						total += flow + center.heat
					} else if tick%5 == 0 {
						cells[neighborKey] = &gridCell{
							terrain: tick%3 + 1,
							heat:    x + y + tick%4,
						}
						total += cells[neighborKey].heat
					}
				}
			}
		}
	}
	if cell := cells[gridPackedKey(2, 2)]; cell != nil {
		total += cell.heat
	}
	if cell := cells[gridPackedKey(4, 4)]; cell != nil {
		total += cell.heat
	}
	return total
}

func gridStringKey(x, y int64) string {
	return strconv.FormatInt(x, 10) + ":" + strconv.FormatInt(y, 10)
}

func sparseGridStrings(seed int64) int64 {
	cells := map[string]*gridCell{
		"0:0": {terrain: 1, heat: 5 + seed%5},
		"1:0": {terrain: 2, heat: 3},
		"2:1": {terrain: 1, heat: 7},
		"3:2": {terrain: 3, heat: 2},
		"4:4": {terrain: 2, heat: 9},
	}
	var total int64
	for tick := int64(1); tick <= 36; tick++ {
		for x := int64(0); x <= 4; x++ {
			for y := int64(0); y <= 4; y++ {
				center := cells[gridStringKey(x, y)]
				if center == nil {
					continue
				}
				for _, offset := range gridOffsets {
					neighborKey := gridStringKey(x+offset.dx, y+offset.dy)
					neighbor := cells[neighborKey]
					if neighbor != nil {
						flow := center.heat - neighbor.heat
						if flow < 0 {
							flow = -flow
						}
						center.heat = center.heat + tick%3 - neighbor.terrain
						total += flow + center.heat
					} else if tick%5 == 0 {
						cells[neighborKey] = &gridCell{
							terrain: tick%3 + 1,
							heat:    x + y + tick%4,
						}
						total += cells[neighborKey].heat
					}
				}
			}
		}
	}
	if cell := cells["2:2"]; cell != nil {
		total += cell.heat
	}
	if cell := cells["4:4"]; cell != nil {
		total += cell.heat
	}
	return total
}

type prototype struct {
	hp    int64
	mana  int64
	armor int64
}

type actor struct {
	hp       int64
	mana     int64
	armor    int64
	power    int64
	hasHP    bool
	hasMana  bool
	hasArmor bool
	hasPower bool
}

func (actor *actor) get(key string, prototype prototype, misses *int64) int64 {
	switch key {
	case "hp":
		if actor.hasHP {
			return actor.hp
		}
	case "mana":
		if actor.hasMana {
			return actor.mana
		}
	case "armor":
		if actor.hasArmor {
			return actor.armor
		}
	case "power":
		if actor.hasPower {
			return actor.power
		}
	}
	(*misses)++
	if key == "power" {
		return prototype.hp + prototype.mana
	}
	switch key {
	case "hp":
		return prototype.hp
	case "mana":
		return prototype.mana
	case "armor":
		return prototype.armor
	default:
		return 0
	}
}

func prototypeFallback(seed int64) int64 {
	prototype := prototype{hp: 20 + seed%3, mana: 5, armor: 2}
	actors := [...]actor{
		{hp: 80, hasHP: true},
		{mana: 15, armor: 4, hasMana: true, hasArmor: true},
		{hp: 45, power: 9, hasHP: true, hasPower: true},
	}
	var total int64
	var misses int64
	for tick := int64(1); tick <= 80; tick++ {
		for i := range actors {
			actor := &actors[i]
			value := actor.get("hp", prototype, &misses) +
				actor.get("mana", prototype, &misses) +
				actor.get("power", prototype, &misses)
			if actor.get("armor", prototype, &misses) > 3 {
				actor.hp = actor.get("hp", prototype, &misses) + tick%3 - actor.get("armor", prototype, &misses)
				actor.hasHP = true
			} else {
				actor.mana = actor.get("mana", prototype, &misses) + tick%4
				actor.hasMana = true
			}
			total += value + actor.get("hp", prototype, &misses) + actor.get("mana", prototype, &misses)
		}
	}
	return total + misses
}

type compactRow struct {
	score int64
	live  bool
}

func arrayHoleCompaction(seed int64) int64 {
	values := make([]compactRow, 30, 44)
	for i := range values {
		values[i] = compactRow{score: int64(i+1)*3 + seed%2, live: true}
	}
	var total int64
	for tick := int64(1); tick <= 70; tick++ {
		for i := 0; i < len(values); {
			values[i].score += tick % 6
			total += values[i].score
			if values[i].score%13 == 0 {
				copy(values[i:], values[i+1:])
				values = values[:len(values)-1]
			} else {
				i++
			}
		}
		if tick%5 == 0 {
			values = append(values, compactRow{score: tick, live: true})
		}
	}
	return total + int64(len(values))
}

type commandName uint8

const (
	commandMove commandName = iota
	commandLoot
	commandSpend
	commandWait
)

type command struct {
	name    commandName
	a, b, c int64
}

type commandState struct {
	x, y, score, gold int64
}

func applyCommand(state *commandState, name commandName, a, b, c int64) (int64, int64, int64) {
	switch name {
	case commandMove:
		state.x += a
		state.y += b
		return state.x, state.y, state.score
	case commandLoot:
		state.gold += a + b + c
		state.score += state.gold
		return state.gold, state.score, 3
	case commandSpend:
		state.gold -= a
		return state.gold, state.x, state.y
	default:
		return state.score, state.gold, 0
	}
}

func commandVarargSpecialized(seed int64) int64 {
	state := commandState{gold: 10 + seed%7}
	commands := [...]command{
		{name: commandMove, a: 1, b: 2},
		{name: commandLoot, a: 3, b: 4, c: 5},
		{name: commandSpend, a: 6},
		{name: commandWait},
	}
	var total int64
	for tick := int64(1); tick <= 60; tick++ {
		for _, command := range commands {
			a, b, c := applyCommand(&state, command.name, command.a+tick%3, command.b, command.c)
			total += a + b + c
		}
	}
	return total + state.x + state.y + state.score + state.gold
}

func commandVarargSlices(seed int64) int64 {
	type genericCommand struct {
		name string
		args []int64
	}
	state := commandState{gold: 10 + seed%7}
	apply := func(name string, args ...int64) []int64 {
		switch name {
		case "move":
			state.x += args[0]
			state.y += args[1]
			return []int64{state.x, state.y, state.score}
		case "loot":
			state.gold += args[0] + args[1] + args[2]
			state.score += state.gold
			return []int64{state.gold, state.score, int64(len(args))}
		case "spend":
			state.gold -= args[0]
			return []int64{state.gold, state.x, state.y}
		default:
			return []int64{state.score, state.gold, 0}
		}
	}
	commands := [...]genericCommand{
		{name: "move", args: []int64{1, 2, 0}},
		{name: "loot", args: []int64{3, 4, 5}},
		{name: "spend", args: []int64{6, 0, 0}},
		{name: "wait", args: []int64{0, 0, 0}},
	}
	var total int64
	for tick := int64(1); tick <= 60; tick++ {
		for _, command := range commands {
			values := apply(command.name, command.args[0]+tick%3, command.args[1], command.args[2])
			total += values[0] + values[1] + values[2]
		}
	}
	return total + state.x + state.y + state.score + state.gold
}
