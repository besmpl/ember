package ember_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/besmpl/ember"
)

type top10LuauCase struct {
	name   string
	source string
	want   string
}

var top10LuauCases = []top10LuauCase{
	{
		name: "arithmetic_for",
		source: `
local total = 0
for i = 1, 200 do
    total = total + ((i * 3 - i // 2) % 17)
end
return total
`,
		want: "1595",
	},
	{
		name: "while_branching",
		source: `
local i = 0
local total = 0
while i < 250 do
    i = i + 1
    if i % 5 == 0 then
        total = total + i // 5
    elseif i % 2 == 0 then
        total = total + 2
    else
        total = total + 1
    end
end
return total
`,
		want: "1575",
	},
	{
		name: "table_fields",
		source: `
local player = {stats = {hp = 100, shield = 25}, inventory = {coins = 3}}
local i = 0
while i < 80 do
    i = i + 1
    player.stats.hp = player.stats.hp + player.stats.shield - player.inventory.coins
end
return player.stats.hp
`,
		want: "1860",
	},
	{
		name: "array_ops",
		source: `
local values = {}
for i = 1, 80 do
    table.insert(values, i % 9)
end
local removed = 0
for i = 1, 20 do
    removed = removed + table.remove(values, 1)
end
return removed + rawlen(values)
`,
		want: "135",
	},
	{
		name: "generic_iteration",
		source: `
local values = {1, 2, 3, 4, 5, 6, 7, 8}
local total = 0
for _, value in values do
    total = total + value * value
end
return total
`,
		want: "204",
	},
	{
		name: "closures_upvalues",
		source: `
local function makeCounter(seed)
    local value = seed
    return function(step)
        value = value + step
        return value
    end
end
local counter = makeCounter(10)
local total = 0
for i = 1, 60 do
    total = total + counter(i % 4)
end
return total
`,
		want: "3360",
	},
	{
		name: "method_calls",
		source: `
local counter = {value = 0}
function counter:add(amount)
    self.value = self.value + amount
    return self.value
end
local total = 0
for i = 1, 70 do
    total = total + counter:add(i % 5)
end
return total
`,
		want: "4970",
	},
	{
		name: "metatable_index",
		source: `
local fallback = {hp = 7, shield = 3}
local player = setmetatable({shield = 5}, {__index = fallback})
local total = 0
for i = 1, 90 do
    total = total + player.hp + player.shield
end
return total
`,
		want: "1080",
	},
	{
		name: "varargs_select",
		source: `
local function score(...)
    local count = select("#", ...)
    local a, b, c, d, e = ...
    return count + a * 2 + b * 3 + c * 5 + d * 7 + e * 11
end
local total = 0
for i = 1, 50 do
    total = total + score(i, i + 1, i + 2, i + 3, i + 4)
end
return total
`,
		want: "39850",
	},
	{
		name: "coroutine_yield",
		source: `
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
local ok, value = coroutine.resume(co, 45)
while coroutine.status(co) ~= "dead" do
    total = total + value
    ok, value = coroutine.resume(co)
end
return total + value
`,
		want: "2585",
	},
}

var classicLuauCases = []top10LuauCase{
	{
		name: "recursive_fibonacci",
		source: `
local function fib(n)
    if n < 2 then
        return n
    end
    return fib(n - 1) + fib(n - 2)
end
return fib(20)
`,
		want: "6765",
	},
	{
		name: "iterative_fibonacci",
		source: `
local a = 0
local b = 1
for i = 1, 30 do
    local next = a + b
    a = b
    b = next
end
return a
`,
		want: "832040",
	},
}

var scenarioLuauCases = []top10LuauCase{
	{
		name: "combat_tick",
		source: `
local entities = {
    {hp = 120, shield = 12, regen = 2, damage = 13, alive = true},
    {hp = 95, shield = 24, regen = 1, damage = 8, alive = true},
    {hp = 160, shield = 5, regen = 3, damage = 17, alive = true},
    {hp = 80, shield = 30, regen = 0, damage = 11, alive = true},
}
local score = 0
for tick = 1, 30 do
    for _, entity in entities do
        if entity.alive then
            local incoming = entity.damage + tick % 5
            if entity.shield > 0 then
                local absorbed = math.min(entity.shield, incoming)
                entity.shield = entity.shield - absorbed
                incoming = incoming - absorbed
            end
            entity.hp = entity.hp - incoming + entity.regen
            if entity.hp <= 0 then
                entity.alive = false
            else
                score = score + entity.hp + entity.shield
            end
        end
    end
end
return score
`,
		want: "2519",
	},
	{
		name: "inventory_value",
		source: `
local inventory = {
    {kind = "ore", count = 12, value = 5, rarity = 1},
    {kind = "gem", count = 3, value = 40, rarity = 4},
    {kind = "potion", count = 7, value = 13, rarity = 2},
    {kind = "key", count = 1, value = 100, rarity = 5},
    {kind = "cloth", count = 20, value = 2, rarity = 1},
}
local score = 0
for day = 1, 40 do
    for _, item in inventory do
        local bonus = item.rarity * (day % 4 + 1)
        if item.kind == "gem" or item.kind == "key" then
            score = score + item.count * (item.value + bonus)
        else
            score = score + item.count * item.value + bonus
        end
    end
end
return score
`,
		want: "18540",
	},
	{
		name: "event_dispatch",
		source: `
local state = {hp = 100, shield = 20, score = 0}
local handlers = {}
function handlers.damage(s, amount)
    if s.shield > 0 then
        local absorbed = math.min(s.shield, amount)
        s.shield = s.shield - absorbed
        amount = amount - absorbed
    end
    s.hp = s.hp - amount
    return s.hp
end
function handlers.heal(s, amount)
    s.hp = s.hp + amount
    return s.hp
end
function handlers.score(s, amount)
    s.score = s.score + amount
    return s.score
end
local events = {
    {kind = "damage", amount = 7},
    {kind = "score", amount = 5},
    {kind = "heal", amount = 3},
    {kind = "damage", amount = 11},
    {kind = "score", amount = 13},
}
local total = 0
for round = 1, 50 do
    for _, event in events do
        total = total + handlers[event.kind](state, event.amount + round % 3)
    end
end
return total + state.hp + state.shield + state.score
`,
		want: "8414",
	},
	{
		name: "buff_stack_tick",
		source: `
local entities = {
    {hp = 100, speed = 10, armor = 4, buffs = {
        {kind = "poison", power = 3, turns = 5},
        {kind = "shield", power = 2, turns = 3},
    }},
    {hp = 140, speed = 8, armor = 6, buffs = {
        {kind = "regen", power = 4, turns = 4},
        {kind = "haste", power = 1, turns = 6},
        {kind = "poison", power = 2, turns = 2},
    }},
    {hp = 90, speed = 14, armor = 2, buffs = {
        {kind = "shield", power = 5, turns = 2},
        {kind = "regen", power = 1, turns = 8},
    }},
}
local score = 0
for tick = 1, 24 do
    for _, entity in entities do
        local i = 1
        while i <= rawlen(entity.buffs) do
            local buff = entity.buffs[i]
            if buff.kind == "poison" then
                entity.hp = entity.hp - buff.power
            elseif buff.kind == "regen" then
                entity.hp = entity.hp + buff.power
            elseif buff.kind == "shield" then
                entity.armor = entity.armor + buff.power
            elseif buff.kind == "haste" then
                entity.speed = entity.speed + buff.power
            end
            buff.turns = buff.turns - 1
            if buff.turns <= 0 then
                table.remove(entity.buffs, i)
            else
                i = i + 1
            end
        end
        score = score + entity.hp + entity.speed + entity.armor + rawlen(entity.buffs)
    end
end
return score
`,
		want: "9601",
	},
	{
		name: "ability_resolution",
		source: `
local caster = {mana = 120, heat = 0, power = 11, combo = 0}
local target = {hp = 340, armor = 8, ward = 12}
local abilities = {
    {cost = 7, cooldown = 0, base = 18, tag = "strike"},
    {cost = 13, cooldown = 1, base = 31, tag = "burn"},
    {cost = 5, cooldown = 0, base = 12, tag = "pierce"},
    {cost = 17, cooldown = 2, base = 45, tag = "burst"},
}
local total = 0
for turn = 1, 36 do
    for _, ability in abilities do
        if ability.cooldown <= 0 and caster.mana >= ability.cost then
            caster.mana = caster.mana - ability.cost
            local damage = ability.base + caster.power + caster.combo
            if ability.tag == "burn" then
                damage = damage + caster.heat
                caster.heat = caster.heat + 3
            elseif ability.tag == "pierce" then
                damage = damage - target.armor // 2
            elseif ability.tag == "burst" then
                damage = damage + caster.combo * 2
                caster.combo = 0
            else
                damage = damage - target.armor
                caster.combo = caster.combo + 1
            end
            if target.ward > 0 then
                local absorbed = math.min(target.ward, damage)
                target.ward = target.ward - absorbed
                damage = damage - absorbed
            end
            target.hp = target.hp - damage
            ability.cooldown = turn % 3 + 1
            total = total + target.hp + damage + caster.mana
        else
            ability.cooldown = ability.cooldown - 1
            caster.mana = caster.mana + 2
            total = total + caster.mana + ability.cooldown
        end
    end
end
return total + target.hp + caster.heat + caster.combo
`,
		want: "-6048",
	},
	{
		name: "ai_utility_scoring",
		source: `
local self = {hp = 72, energy = 40, threat = 9}
local targets = {
    {hp = 30, distance = 4, threat = 7, armor = 2},
    {hp = 80, distance = 9, threat = 12, armor = 6},
    {hp = 55, distance = 2, threat = 4, armor = 1},
    {hp = 110, distance = 12, threat = 15, armor = 9},
}
local actions = {
    {kind = "attack", cost = 8, base = 20, range = 5},
    {kind = "kite", cost = 4, base = 12, range = 10},
    {kind = "guard", cost = 6, base = 16, range = 3},
    {kind = "burst", cost = 18, base = 45, range = 4},
}
local total = 0
for tick = 1, 45 do
    local best = -9999
    for _, action in actions do
        for _, target in targets do
            local score = action.base + self.threat - target.armor
            if target.distance <= action.range then
                score = score + 25
            else
                score = score - (target.distance - action.range) * 3
            end
            if action.kind == "attack" then
                score = score + (100 - target.hp) // 4
            elseif action.kind == "kite" then
                score = score + target.threat - self.hp // 10
            elseif action.kind == "guard" then
                score = score + (100 - self.hp) // 3
            else
                score = score + self.energy - action.cost
            end
            if self.energy < action.cost then
                score = score - 50
            end
            if score > best then
                best = score
            end
        end
    end
    total = total + best
    self.energy = self.energy + tick % 5 - 2
    self.hp = self.hp + tick % 3 - 1
end
return total + self.hp + self.energy
`,
		want: "4612",
	},
	{
		name: "cooldown_scheduler",
		source: `
local actors = {
    {energy = 30, haste = 1, abilities = {
        {cost = 6, cooldown = 0, reset = 3, uses = 0},
        {cost = 11, cooldown = 2, reset = 5, uses = 0},
        {cost = 4, cooldown = 1, reset = 2, uses = 0},
    }},
    {energy = 22, haste = 2, abilities = {
        {cost = 8, cooldown = 0, reset = 4, uses = 0},
        {cost = 5, cooldown = 3, reset = 3, uses = 0},
    }},
    {energy = 45, haste = 0, abilities = {
        {cost = 13, cooldown = 1, reset = 6, uses = 0},
        {cost = 7, cooldown = 0, reset = 2, uses = 0},
        {cost = 9, cooldown = 4, reset = 4, uses = 0},
    }},
}
local score = 0
for tick = 1, 72 do
    for _, actor in actors do
        actor.energy = actor.energy + 2 + actor.haste
        for _, ability in actor.abilities do
            if ability.cooldown > 0 then
                ability.cooldown = ability.cooldown - 1 - actor.haste
                if ability.cooldown < 0 then
                    ability.cooldown = 0
                end
            end
            if ability.cooldown == 0 and actor.energy >= ability.cost then
                actor.energy = actor.energy - ability.cost
                ability.uses = ability.uses + 1
                ability.cooldown = ability.reset
                score = score + actor.energy + ability.uses * ability.cost
            else
                score = score + ability.cooldown + actor.energy
            end
        end
    end
end
return score
`,
		want: "13075",
	},
	{
		name: "projectile_sweep",
		source: `
local projectiles = {
    {x = 0, y = 0, vx = 3, vy = 1, damage = 12, live = true},
    {x = 5, y = -2, vx = 2, vy = 2, damage = 9, live = true},
    {x = -4, y = 3, vx = 4, vy = -1, damage = 15, live = true},
    {x = 8, y = 1, vx = 1, vy = 3, damage = 7, live = true},
}
local targets = {
    {x = 24, y = 8, radius = 5, hp = 80},
    {x = 38, y = 16, radius = 4, hp = 70},
    {x = 52, y = 18, radius = 6, hp = 110},
    {x = 64, y = 28, radius = 5, hp = 90},
}
local score = 0
for step = 1, 30 do
    for _, projectile in projectiles do
        if projectile.live then
            projectile.x = projectile.x + projectile.vx
            projectile.y = projectile.y + projectile.vy
            for _, target in targets do
                if target.hp > 0 then
                    local dx = projectile.x - target.x
                    local dy = projectile.y - target.y
                    if dx * dx + dy * dy <= target.radius * target.radius then
                        target.hp = target.hp - projectile.damage
                        projectile.live = false
                        score = score + target.hp + step
                        break
                    end
                end
            end
            if projectile.x > 80 or projectile.y > 40 then
                projectile.live = false
            end
        end
    end
end
for _, target in targets do
    score = score + target.hp
end
return score
`,
		want: "413",
	},
	{
		name: "quest_progress_update",
		source: `
local quests = {
    {id = "hunt", done = false, objectives = {
        {kind = "kill", target = "wolf", need = 5, have = 0},
        {kind = "collect", target = "pelt", need = 3, have = 0},
    }},
    {id = "craft", done = false, objectives = {
        {kind = "collect", target = "ore", need = 8, have = 0},
        {kind = "visit", target = "forge", need = 1, have = 0},
    }},
    {id = "scout", done = false, objectives = {
        {kind = "visit", target = "tower", need = 2, have = 0},
        {kind = "kill", target = "spider", need = 4, have = 0},
    }},
}
local events = {
    {kind = "kill", target = "wolf", amount = 1},
    {kind = "collect", target = "pelt", amount = 1},
    {kind = "collect", target = "ore", amount = 2},
    {kind = "visit", target = "tower", amount = 1},
    {kind = "kill", target = "spider", amount = 1},
    {kind = "visit", target = "forge", amount = 1},
}
local score = 0
for round = 1, 24 do
    for _, event in events do
        for _, quest in quests do
            if not quest.done then
                local complete = true
                for _, objective in quest.objectives do
                    if objective.kind == event.kind and objective.target == event.target then
                        objective.have = math.min(objective.need, objective.have + event.amount)
                    end
                    if objective.have < objective.need then
                        complete = false
                    end
                    score = score + objective.have
                end
                if complete then
                    quest.done = true
                    score = score + round * 10
                end
            end
        end
    end
end
return score
`,
		want: "419",
	},
	{
		name: "behavior_tree_tick",
		source: `
local blackboard = {hp = 65, ammo = 6, enemyDistance = 12, cover = 3, alert = 0}
local nodes = {
    {kind = "condition", key = "hp", threshold = 35, pass = 2, fail = 3},
    {kind = "action", name = "attack", weight = 15},
    {kind = "condition", key = "ammo", threshold = 1, pass = 4, fail = 5},
    {kind = "action", name = "reload", weight = 9},
    {kind = "action", name = "retreat", weight = 12},
}
local total = 0
for tick = 1, 80 do
    local index = 1
    local depth = 0
    while index > 0 and depth < 4 do
        local node = nodes[index]
        if node.kind == "condition" then
            local value = blackboard[node.key]
            if value > node.threshold then
                index = node.pass
            else
                index = node.fail
            end
        else
            if node.name == "attack" then
                blackboard.ammo = blackboard.ammo - 1
                blackboard.alert = blackboard.alert + 2
            elseif node.name == "reload" then
                blackboard.ammo = blackboard.ammo + 3
                blackboard.alert = blackboard.alert + 1
            else
                blackboard.hp = blackboard.hp + blackboard.cover
                blackboard.enemyDistance = blackboard.enemyDistance + 2
            end
            total = total + node.weight + blackboard.hp + blackboard.ammo + blackboard.alert
            index = 0
        end
        depth = depth + 1
    end
    blackboard.hp = blackboard.hp - tick % 4
    blackboard.enemyDistance = blackboard.enemyDistance - tick % 3
    if blackboard.enemyDistance < 3 then
        blackboard.enemyDistance = 12
    end
end
return total + blackboard.hp + blackboard.ammo + blackboard.alert
`,
		want: "7252",
	},
	{
		name: "threat_aggro_table",
		source: `
local actors = {
    {id = "tank", role = "front", alive = true},
    {id = "mage", role = "burst", alive = true},
    {id = "healer", role = "support", alive = true},
    {id = "rogue", role = "burst", alive = true},
}
local enemies = {
    {hp = 180, enraged = false, threat = {tank = 20, mage = 0, healer = 4, rogue = 8}},
    {hp = 140, enraged = false, threat = {tank = 10, mage = 12, healer = 0, rogue = 5}},
    {hp = 220, enraged = false, threat = {tank = 30, mage = 8, healer = 6, rogue = 2}},
}
local events = {
    {actor = "tank", kind = "taunt", amount = 9},
    {actor = "mage", kind = "damage", amount = 17},
    {actor = "healer", kind = "heal", amount = 12},
    {actor = "rogue", kind = "damage", amount = 11},
    {actor = "tank", kind = "damage", amount = 7},
}
local total = 0
for tick = 1, 48 do
    for _, enemy in enemies do
        for _, event in events do
            local gain = event.amount + tick % 4
            if event.kind == "taunt" then
                gain = gain * 2
            elseif event.kind == "heal" then
                gain = gain // 2 + 3
            end
            if enemy.enraged then
                gain = gain + 2
            end
            enemy.threat[event.actor] = enemy.threat[event.actor] + gain
        end
        local top = -1
        local focusRole = ""
        for _, actor in actors do
            local value = enemy.threat[actor.id]
            if actor.alive and value > top then
                top = value
                focusRole = actor.role
            end
        end
        if focusRole == "front" then
            total = total + top + enemy.hp
        elseif focusRole == "support" then
            total = total + top * 2
        else
            total = total + top + enemy.hp // 2
        end
        enemy.hp = enemy.hp - tick % 5
        if enemy.hp < 120 then
            enemy.enraged = true
        end
    end
end
return total
`,
		want: "129646",
	},
	{
		name: "economy_market_tick",
		source: `
local markets = {
    {stock = {wood = 40, ore = 18, food = 30}, demand = {wood = 8, ore = 14, food = 6}, price = {wood = 3, ore = 9, food = 4}},
    {stock = {wood = 12, ore = 35, food = 20}, demand = {wood = 15, ore = 5, food = 10}, price = {wood = 5, ore = 7, food = 6}},
    {stock = {wood = 25, ore = 11, food = 50}, demand = {wood = 10, ore = 18, food = 4}, price = {wood = 4, ore = 10, food = 3}},
}
local orders = {
    {good = "wood", amount = 7, kind = "buy"},
    {good = "ore", amount = 4, kind = "buy"},
    {good = "food", amount = 9, kind = "sell"},
    {good = "ore", amount = 3, kind = "sell"},
}
local cash = 0
for day = 1, 54 do
    for _, market in markets do
        for _, order in orders do
            local good = order.good
            local pressure = market.demand[good] - market.stock[good] // 5
            local price = market.price[good] + pressure
            if price < 1 then
                price = 1
            end
            if order.kind == "buy" then
                local amount = math.min(order.amount + day % 3, market.stock[good])
                market.stock[good] = market.stock[good] - amount
                cash = cash - amount * price
            else
                local amount = order.amount + day % 2
                market.stock[good] = market.stock[good] + amount
                cash = cash + amount * price
            end
            market.price[good] = price + day % 2
        end
    end
end
local inventoryScore = 0
for _, market in markets do
    inventoryScore = inventoryScore + market.stock.wood + market.stock.ore * 2 + market.stock.food
end
return cash + inventoryScore
`,
		want: "4537",
	},
	{
		name: "formation_layout_score",
		source: `
local units = {
    {role = "tank", x = 1, y = 1, hp = 120, speed = 4},
    {role = "healer", x = 3, y = 2, hp = 80, speed = 5},
    {role = "ranged", x = 2, y = 5, hp = 70, speed = 6},
    {role = "melee", x = 5, y = 3, hp = 95, speed = 7},
    {role = "ranged", x = 6, y = 6, hp = 65, speed = 5},
}
local slots = {
    {role = "tank", x = 2, y = 1, weight = 20},
    {role = "melee", x = 3, y = 3, weight = 16},
    {role = "healer", x = 2, y = 4, weight = 18},
    {role = "ranged", x = 5, y = 5, weight = 14},
    {role = "ranged", x = 6, y = 4, weight = 12},
}
local total = 0
for tick = 1, 64 do
    for _, unit in units do
        local best = -999
        for _, slot in slots do
            local dx = unit.x - slot.x
            if dx < 0 then dx = -dx end
            local dy = unit.y - slot.y
            if dy < 0 then dy = -dy end
            local score = slot.weight + unit.hp // 10 + unit.speed - (dx + dy) * 4
            if unit.role == slot.role then
                score = score + 30
            end
            if score > best then
                best = score
            end
        end
        total = total + best
        if tick % 2 == 0 then
            unit.x = unit.x + 1
        else
            unit.y = unit.y + 1
        end
        if unit.x > 7 then unit.x = 1 end
        if unit.y > 7 then unit.y = 1 end
    end
end
return total
`,
		want: "14194",
	},
	{
		name: "dialogue_condition_eval",
		source: `
local state = {reputation = 8, gold = 25, insight = 3, flags = {met_guard = true, has_badge = false, helped_mage = false}}
local rules = {
    {speaker = "guard", checks = {{key = "met_guard", want = true}, {stat = "reputation", atLeast = 5}}, reward = 4, flag = "has_badge"},
    {speaker = "mage", checks = {{key = "has_badge", want = true}, {stat = "gold", atLeast = 12}}, reward = 7, flag = "helped_mage"},
    {speaker = "merchant", checks = {{key = "helped_mage", want = true}, {stat = "insight", atLeast = 4}}, reward = 11, flag = "trade_route"},
    {speaker = "scout", checks = {{key = "trade_route", want = true}, {stat = "reputation", atLeast = 12}}, reward = 13, flag = "map_known"},
}
local total = 0
for pass = 1, 36 do
    for _, rule in rules do
        local ok = true
        for _, check in rule.checks do
            if check.key ~= nil then
                if state.flags[check.key] ~= check.want then
                    ok = false
                end
            else
                if state[check.stat] < check.atLeast then
                    ok = false
                end
            end
        end
        if ok then
            state.flags[rule.flag] = true
            state.reputation = state.reputation + rule.reward % 5
            state.insight = state.insight + 1
            state.gold = state.gold - rule.reward // 3
            total = total + rule.reward + state.reputation + state.insight
        else
            state.gold = state.gold + 1
            total = total + state.gold % 7
        end
    end
end
local flagScore = 0
if state.flags.has_badge then flagScore = flagScore + 10 end
if state.flags.helped_mage then flagScore = flagScore + 20 end
if state.flags.trade_route then flagScore = flagScore + 30 end
if state.flags.map_known then flagScore = flagScore + 40 end
return total + flagScore + state.gold + state.reputation + state.insight
`,
		want: "24963",
	},
	{
		name: "procgen_room_scoring",
		source: `
local rooms = {
    {kind = "start", exits = 2, loot = 1, danger = 0, size = 4},
    {kind = "combat", exits = 3, loot = 3, danger = 7, size = 6},
    {kind = "treasure", exits = 1, loot = 9, danger = 3, size = 3},
    {kind = "puzzle", exits = 2, loot = 5, danger = 2, size = 5},
    {kind = "boss", exits = 1, loot = 12, danger = 12, size = 8},
}
local total = 0
local depth = 0
for step = 1, 70 do
    local bestScore = -999
    local bestIndex = 1
    for i, room in rooms do
        local score = room.loot * 5 + room.exits * 8 + room.size * 2 - room.danger * 3 - depth
        if room.kind == "combat" and step % 3 == 0 then
            score = score + 12
        elseif room.kind == "treasure" and depth > 8 then
            score = score + 18
        elseif room.kind == "boss" and depth < 10 then
            score = score - 30
        end
        if score > bestScore then
            bestScore = score
            bestIndex = i
        end
    end
    local chosen = rooms[bestIndex]
    total = total + bestScore + chosen.loot
    depth = depth + chosen.exits
    chosen.danger = chosen.danger + step % 4
    chosen.loot = chosen.loot - 1
    if chosen.loot < 0 then chosen.loot = step % 3 end
    if depth > 20 then depth = depth - 11 end
end
return total + depth
`,
		want: "-725",
	},
	{
		name: "save_state_diff",
		source: `
local before = {
    {id = "p1", hp = 100, zone = "town", inv = {coins = 20, herbs = 3, ore = 0}},
    {id = "p2", hp = 85, zone = "mine", inv = {coins = 5, herbs = 0, ore = 8}},
    {id = "npc1", hp = 40, zone = "road", inv = {coins = 2, herbs = 1, ore = 0}},
}
local after = {
    {id = "p1", hp = 92, zone = "road", inv = {coins = 17, herbs = 5, ore = 0}},
    {id = "p2", hp = 85, zone = "mine", inv = {coins = 12, herbs = 0, ore = 4}},
    {id = "npc1", hp = 0, zone = "road", inv = {coins = 2, herbs = 1, ore = 0}},
}
local fields = {"coins", "herbs", "ore"}
local total = 0
for pass = 1, 90 do
    for i, left in before do
        local right = after[i]
        if left.hp ~= right.hp then
            local delta = left.hp - right.hp
            if delta < 0 then delta = -delta end
            total = total + delta + pass % 5
        end
        if left.zone ~= right.zone then
            total = total + 17
        end
        for _, field in fields do
            local delta = left.inv[field] - right.inv[field]
            if delta < 0 then delta = -delta end
            total = total + delta * (pass % 3 + 1)
        end
    end
end
return total
`,
		want: "9090",
	},
	{
		name: "path_relaxation",
		source: `
local nodes = {
    {cost = 1, dist = 0, blocked = false, edges = {{to = 2, weight = 3}, {to = 3, weight = 7}}},
    {cost = 2, dist = 999, blocked = false, edges = {{to = 4, weight = 2}, {to = 5, weight = 5}}},
    {cost = 4, dist = 999, blocked = false, edges = {{to = 5, weight = 1}, {to = 6, weight = 9}}},
    {cost = 1, dist = 999, blocked = false, edges = {{to = 7, weight = 4}}},
    {cost = 3, dist = 999, blocked = false, edges = {{to = 7, weight = 2}, {to = 8, weight = 8}}},
    {cost = 2, dist = 999, blocked = true, edges = {{to = 8, weight = 1}}},
    {cost = 5, dist = 999, blocked = false, edges = {{to = 9, weight = 3}}},
    {cost = 1, dist = 999, blocked = false, edges = {{to = 9, weight = 2}}},
    {cost = 2, dist = 999, blocked = false, edges = {}},
}
local total = 0
for pass = 1, 40 do
    for i, node in nodes do
        if not node.blocked then
            for _, edge in node.edges do
                local nextNode = nodes[edge.to]
                if not nextNode.blocked then
                    local candidate = node.dist + edge.weight + nextNode.cost + pass % 3
                    if candidate < nextNode.dist then
                        nextNode.dist = candidate
                    end
                    total = total + nextNode.dist % 17 + i
                end
            end
        end
    end
    if pass % 10 == 0 then
        nodes[3].dist = nodes[3].dist + 4
        nodes[5].dist = nodes[5].dist + 2
    end
end
local sum = 0
for _, node in nodes do
    if node.dist < 999 then
        sum = sum + node.dist
    end
end
return total + sum
`,
		want: "4286",
	},
	{
		name: "component_churn",
		source: `
local entities = {
    {id = 1, components = {hp = 100, mana = 20, poison = 0}, dirty = false},
    {id = 2, components = {hp = 85, shield = 12, speed = 4}, dirty = false},
    {id = 3, components = {hp = 130, mana = 5, poison = 2}, dirty = false},
    {id = 4, components = {hp = 60, shield = 30, speed = 7}, dirty = false},
}
local keys = {"hp", "mana", "poison", "shield", "speed"}
local score = 0
for tick = 1, 60 do
    for _, entity in entities do
        local key = keys[(tick + entity.id) % rawlen(keys) + 1]
        local value = entity.components[key]
        if value == nil then
            entity.components[key] = tick % 7 + entity.id
            entity.dirty = true
            score = score + entity.components[key]
        else
            entity.components[key] = value + tick % 5 - 1
            score = score + entity.components[key]
            if key ~= "hp" and entity.components[key] % 11 == 0 then
                entity.components[key] = nil
                entity.dirty = true
            end
        end
        if entity.components.hp ~= nil and entity.components.poison ~= nil and entity.components.poison > 0 then
            entity.components.hp = entity.components.hp - entity.components.poison
        end
        if entity.dirty then
            score = score + entity.id
            entity.dirty = false
        end
    end
end
return score + (entities[1].components.hp or 0) + (entities[2].components.hp or 0) + (entities[3].components.hp or 0) + (entities[4].components.hp or 0)
`,
		want: "-2325",
	},
	{
		name: "prototype_fallback",
		source: `
local prototype = {hp = 20, mana = 5, armor = 2}
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
		want: "32379",
	},
	{
		name: "signal_bus_callbacks",
		source: `
local state = {hp = 120, score = 0, armor = 3}
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
		want: "76620",
	},
	{
		name: "state_machine_transitions",
		source: `
local transitions = {
    idle = {see = "chase", hit = "evade", rest = "idle"},
    chase = {near = "attack", lost = "search", hit = "evade"},
    attack = {cooldown = "chase", hit = "evade", lost = "search"},
    evade = {safe = "search", hit = "evade", rest = "idle"},
    search = {see = "chase", rest = "idle", lost = "search"},
}
local weights = {idle = 2, chase = 8, attack = 15, evade = 9, search = 5}
local events = {"see", "near", "cooldown", "lost", "hit", "safe", "rest"}
local state = "idle"
local energy = 20
local total = 0
for tick = 1, 120 do
    local event = events[tick % rawlen(events) + 1]
    local nextState = transitions[state][event]
    if nextState == nil then
        nextState = "idle"
    end
    local weight = weights[nextState] or 0
    if nextState == "attack" then
        energy = energy - 3
    elseif nextState == "evade" then
        energy = energy - 1
    else
        energy = energy + 1
    end
    if energy < 0 then
        energy = 4
    elseif energy > 35 then
        energy = 20
    end
    state = nextState
    total = total + weight + energy + tick % 7
end
return total + weights[state] + energy
`,
		want: "4278",
	},
	{
		name: "sparse_grid_neighbors",
		source: `
local cells = {
    ["0:0"] = {terrain = 1, heat = 5},
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
		want: "-236651",
	},
	{
		name: "dirty_metatable_writes",
		source: `
local dirty = {}
local backing = {hp = 100, mana = 30, xp = 0, gold = 5, flags = 1}
local tracked = setmetatable({}, {
    __index = function(_, key)
        return backing[key] or 0
    end,
    __newindex = function(_, key, value)
        dirty[key] = (dirty[key] or 0) + 1
        backing[key] = value
    end,
})
local keys = {"hp", "mana", "xp", "gold", "flags"}
local total = 0
for tick = 1, 100 do
    local key = keys[tick % rawlen(keys) + 1]
    tracked[key] = tracked[key] + tick % 9
    if tick % 7 == 0 then
        tracked[key] = tracked[key] - tracked.hp % 3
    end
    total = total + tracked[key] + dirty[key]
end
return total + tracked.hp + tracked.mana + tracked.xp + tracked.gold + tracked.flags
`,
		want: "8487",
	},
	{
		name: "array_hole_compaction",
		source: `
local values = {}
for i = 1, 30 do
    values[i] = {score = i * 3, live = true}
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
		want: "31652",
	},
	{
		name: "command_vararg_router",
		source: `
local state = {x = 0, y = 0, score = 0, gold = 10}
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
		want: "824780",
	},
}

func TestTop10LuauBenchmarksMatchExpectedResults(t *testing.T) {
	testLuauCasesMatchExpectedResults(t, top10LuauCases)
}

func TestClassicLuauBenchmarksMatchExpectedResults(t *testing.T) {
	testLuauCasesMatchExpectedResults(t, classicLuauCases)
}

func TestScenarioLuauBenchmarksMatchExpectedResults(t *testing.T) {
	testLuauCasesMatchExpectedResults(t, scenarioLuauCases)
}

func TestTop10EmberRunAllocationBudgets(t *testing.T) {
	budgets := map[string]struct {
		maxBytesPerOp  uint64
		maxAllocsPerOp uint64
	}{
		"array_ops":         {maxBytesPerOp: 10000, maxAllocsPerOp: 28},
		"generic_iteration": {maxBytesPerOp: 1800, maxAllocsPerOp: 10},
	}

	for _, tc := range top10LuauCases {
		budget, ok := budgets[tc.name]
		if !ok {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			proto := benchmarkCompile(t, tc.source)
			result := testing.Benchmark(func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					results, err := ember.Run(proto)
					if err != nil {
						b.Fatal(err)
					}
					if got := top10EmberResultString(b, results); got != tc.want {
						b.Fatalf("Ember result is %q, want %q", got, tc.want)
					}
				}
			})
			bytesPerOp := uint64(result.AllocedBytesPerOp())
			allocsPerOp := uint64(result.AllocsPerOp())
			if bytesPerOp > budget.maxBytesPerOp {
				t.Fatalf("Ember run used %d B/op, want at most %d", bytesPerOp, budget.maxBytesPerOp)
			}
			if allocsPerOp > budget.maxAllocsPerOp {
				t.Fatalf("Ember run used %d allocs/op, want at most %d", allocsPerOp, budget.maxAllocsPerOp)
			}
		})
	}
}

func TestClassicEmberRunAllocationBudgets(t *testing.T) {
	budgets := map[string]struct {
		maxBytesPerOp  uint64
		maxAllocsPerOp uint64
	}{
		"recursive_fibonacci": {maxBytesPerOp: 2300, maxAllocsPerOp: 28},
		"iterative_fibonacci": {maxBytesPerOp: 328, maxAllocsPerOp: 6},
	}

	for _, tc := range classicLuauCases {
		budget, ok := budgets[tc.name]
		if !ok {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			proto := benchmarkCompile(t, tc.source)
			result := testing.Benchmark(func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					results, err := ember.Run(proto)
					if err != nil {
						b.Fatal(err)
					}
					if got := top10EmberResultString(b, results); got != tc.want {
						b.Fatalf("Ember result is %q, want %q", got, tc.want)
					}
				}
			})
			bytesPerOp := uint64(result.AllocedBytesPerOp())
			allocsPerOp := uint64(result.AllocsPerOp())
			if bytesPerOp > budget.maxBytesPerOp {
				t.Fatalf("Ember run used %d B/op, want at most %d", bytesPerOp, budget.maxBytesPerOp)
			}
			if allocsPerOp > budget.maxAllocsPerOp {
				t.Fatalf("Ember run used %d allocs/op, want at most %d", allocsPerOp, budget.maxAllocsPerOp)
			}
		})
	}
}

func testLuauCasesMatchExpectedResults(t *testing.T, cases []top10LuauCase) {
	luauBin, haveLuau := lookupLuauBinary()

	for _, tc := range cases {
		t.Run(tc.name+"/ember", func(t *testing.T) {
			got := runTop10EmberCase(t, tc)
			if got != tc.want {
				t.Fatalf("Ember result is %q, want %q", got, tc.want)
			}
		})

		t.Run(tc.name+"/luau", func(t *testing.T) {
			if !haveLuau {
				t.Skip("set LUAU_BIN or install luau to verify upstream Luau")
			}
			got := runTop10LuauCase(t, luauBin, tc)
			if got != tc.want {
				t.Fatalf("Luau result is %q, want %q", got, tc.want)
			}
		})
	}
}

func TestScenarioEmberRunAllocationBudgets(t *testing.T) {
	budgets := map[string]struct {
		maxBytesPerOp  uint64
		maxAllocsPerOp uint64
	}{
		"combat_tick":               {maxBytesPerOp: 3300, maxAllocsPerOp: 22},
		"inventory_value":           {maxBytesPerOp: 3700, maxAllocsPerOp: 18},
		"event_dispatch":            {maxBytesPerOp: 4000, maxAllocsPerOp: 24},
		"buff_stack_tick":           {maxBytesPerOp: 9000, maxAllocsPerOp: 230},
		"ability_resolution":        {maxBytesPerOp: 3800, maxAllocsPerOp: 20},
		"ai_utility_scoring":        {maxBytesPerOp: 6200, maxAllocsPerOp: 28},
		"cooldown_scheduler":        {maxBytesPerOp: 7700, maxAllocsPerOp: 36},
		"projectile_sweep":          {maxBytesPerOp: 5600, maxAllocsPerOp: 26},
		"quest_progress_update":     {maxBytesPerOp: 9100, maxAllocsPerOp: 70},
		"behavior_tree_tick":        {maxBytesPerOp: 4500, maxAllocsPerOp: 20},
		"threat_aggro_table":        {maxBytesPerOp: 9000, maxAllocsPerOp: 42},
		"economy_market_tick":       {maxBytesPerOp: 14000, maxAllocsPerOp: 390},
		"formation_layout_score":    {maxBytesPerOp: 8000, maxAllocsPerOp: 30},
		"dialogue_condition_eval":   {maxBytesPerOp: 7500, maxAllocsPerOp: 37},
		"procgen_room_scoring":      {maxBytesPerOp: 4600, maxAllocsPerOp: 18},
		"save_state_diff":           {maxBytesPerOp: 7700, maxAllocsPerOp: 36},
		"path_relaxation":           {maxBytesPerOp: 11350, maxAllocsPerOp: 55},
		"component_churn":           {maxBytesPerOp: 14000, maxAllocsPerOp: 310},
		"prototype_fallback":        {maxBytesPerOp: 56000, maxAllocsPerOp: 700},
		"signal_bus_callbacks":      {maxBytesPerOp: 80000, maxAllocsPerOp: 750},
		"state_machine_transitions": {maxBytesPerOp: 6000, maxAllocsPerOp: 150},
		"sparse_grid_neighbors":     {maxBytesPerOp: 1700000, maxAllocsPerOp: 36000},
		"dirty_metatable_writes":    {maxBytesPerOp: 56000, maxAllocsPerOp: 650},
		"array_hole_compaction":     {maxBytesPerOp: 26000, maxAllocsPerOp: 700},
		"command_vararg_router":     {maxBytesPerOp: 95000, maxAllocsPerOp: 700},
	}

	for _, tc := range scenarioLuauCases {
		budget, ok := budgets[tc.name]
		if !ok {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			proto := benchmarkCompile(t, tc.source)
			result := testing.Benchmark(func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					results, err := ember.Run(proto)
					if err != nil {
						b.Fatal(err)
					}
					if got := top10EmberResultString(b, results); got != tc.want {
						b.Fatalf("Ember result is %q, want %q", got, tc.want)
					}
				}
			})
			bytesPerOp := uint64(result.AllocedBytesPerOp())
			allocsPerOp := uint64(result.AllocsPerOp())
			if bytesPerOp > budget.maxBytesPerOp {
				t.Fatalf("Ember run used %d B/op, want at most %d", bytesPerOp, budget.maxBytesPerOp)
			}
			if allocsPerOp > budget.maxAllocsPerOp {
				t.Fatalf("Ember run used %d allocs/op, want at most %d", allocsPerOp, budget.maxAllocsPerOp)
			}
		})
	}
}

func BenchmarkTop10Luau(b *testing.B) {
	benchmarkLuauCases(b, top10LuauCases)
}

func BenchmarkClassicLuau(b *testing.B) {
	benchmarkLuauCases(b, classicLuauCases)
}

func BenchmarkScenarioLuau(b *testing.B) {
	benchmarkLuauCases(b, scenarioLuauCases)
}

func benchmarkLuauCases(b *testing.B, cases []top10LuauCase) {
	luauBin, haveLuau := lookupLuauBinary()

	for _, tc := range cases {
		b.Run(tc.name+"/ember_run", func(b *testing.B) {
			proto := benchmarkCompile(b, tc.source)
			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				results, err := ember.Run(proto)
				if err != nil {
					b.Fatal(err)
				}
				if got := top10EmberResultString(b, results); got != tc.want {
					b.Fatalf("Ember result is %q, want %q", got, tc.want)
				}
			}
		})

		b.Run(tc.name+"/ember_compile_run", func(b *testing.B) {
			b.ReportAllocs()

			for b.Loop() {
				got := runTop10EmberCase(b, tc)
				if got != tc.want {
					b.Fatalf("Ember result is %q, want %q", got, tc.want)
				}
			}
		})

		b.Run(tc.name+"/luau_cli_process", func(b *testing.B) {
			if !haveLuau {
				b.Skip("set LUAU_BIN or install luau to benchmark upstream Luau")
			}
			path := writeTop10LuauScript(b, tc)
			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				got := runTop10LuauScript(b, luauBin, path)
				if got != tc.want {
					b.Fatalf("Luau result is %q, want %q", got, tc.want)
				}
			}
		})

		b.Run(tc.name+"/luau_cli_batch", func(b *testing.B) {
			if !haveLuau {
				b.Skip("set LUAU_BIN or install luau to benchmark upstream Luau")
			}
			const scriptRunsPerProcess = 1000
			path := writeTop10LuauBatchScript(b, tc, scriptRunsPerProcess)
			var total time.Duration
			processRuns := 0
			b.ReportAllocs()
			b.ResetTimer()

			for b.Loop() {
				start := time.Now()
				got := runTop10LuauScript(b, luauBin, path)
				total += time.Since(start)
				processRuns++
				if got != tc.want {
					b.Fatalf("Luau result is %q, want %q", got, tc.want)
				}
			}
			b.ReportMetric(float64(total.Nanoseconds())/float64(processRuns*scriptRunsPerProcess), "ns/luau_run")
		})
	}
}

func lookupLuauBinary() (string, bool) {
	if path := os.Getenv("LUAU_BIN"); path != "" {
		return path, true
	}
	path, err := exec.LookPath("luau")
	return path, err == nil
}

func runTop10EmberCase(tb testing.TB, tc top10LuauCase) string {
	tb.Helper()
	proto := benchmarkCompile(tb, tc.source)
	results, err := ember.Run(proto)
	if err != nil {
		tb.Fatal(err)
	}
	return top10EmberResultString(tb, results)
}

func top10EmberResultString(tb testing.TB, results []ember.Value) string {
	tb.Helper()
	if len(results) != 1 {
		tb.Fatalf("Ember returned %d results, want 1", len(results))
	}
	result := results[0]
	if number, ok := result.Number(); ok {
		return strconv.FormatFloat(number, 'g', -1, 64)
	}
	if str, ok := result.String(); ok {
		return str
	}
	if value, ok := result.Bool(); ok {
		return strconv.FormatBool(value)
	}
	if result.IsNil() {
		return "nil"
	}
	tb.Fatalf("Ember returned %s, want scalar benchmark result", result.Kind())
	return ""
}

func runTop10LuauCase(tb testing.TB, luauBin string, tc top10LuauCase) string {
	tb.Helper()
	return runTop10LuauScript(tb, luauBin, writeTop10LuauScript(tb, tc))
}

func writeTop10LuauScript(tb testing.TB, tc top10LuauCase) string {
	tb.Helper()
	path := filepath.Join(tb.TempDir(), tc.name+".luau")
	source := fmt.Sprintf("local __result = (function()\n%s\nend)()\nprint(__result)\n", tc.source)
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		tb.Fatal(err)
	}
	return path
}

func writeTop10LuauBatchScript(tb testing.TB, tc top10LuauCase, runs int) string {
	tb.Helper()
	path := filepath.Join(tb.TempDir(), tc.name+".luau")
	source := fmt.Sprintf(`
local __case = function()
%s
end
local __result = nil
for __i = 1, %d do
    __result = __case()
end
print(__result)
`, tc.source, runs)
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		tb.Fatal(err)
	}
	return path
}

func runTop10LuauScript(tb testing.TB, luauBin string, path string) string {
	tb.Helper()
	var stderr bytes.Buffer
	cmd := exec.Command(luauBin, path)
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		tb.Fatalf("luau %s failed: %v\n%s", path, err, stderr.String())
	}
	return strings.TrimSpace(string(output))
}
