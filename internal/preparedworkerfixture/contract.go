package preparedworkerfixture

const (
	MainModule    = "logical:prepared-worker/main"
	NumericModule = "logical:prepared-worker/numeric"
	SharedModule  = "logical:prepared-worker/shared"
)

const MainSource = `
return {
    startup = function()
        capture(1, function(entity, amount)
            local loaded = wait(1, entity)
            local dependency = require("./shared")
            local entityDelta = 0
            if entity == 7 then
                entityDelta = amount
            end
            local totalDelta = amount + loaded + dependency.bonus
            command(2, entity, entityDelta, totalDelta)
            return loaded, totalDelta, entityDelta
        end)
    end,

    turn = function(tick, total, ready, entity7, moduleCalls, step, seed, work)
        local dependency = require("./shared")
        local compute = require("./numeric")
        moduleCalls = dependency.next(moduleCalls)
        tick = tick + step
        local numeric = compute(seed, work)
        total = total + step + numeric

        command(1, 0, tick, total)
        effect(2, 0, numeric)

        return tick, total, ready, entity7, moduleCalls
    end,
}
`

const NumericSource = `
return function(seed, work)
    local numeric = 0
    for index = 1, work do
        local n = seed + ((index - 1) % 3)
        local previous = 0
        local current = 1
        for fibIndex = 1, n do
            local nextValue = previous + current
            previous = current
            current = nextValue
        end
        numeric = numeric + previous
    end
    return numeric
end
`

const SharedSource = `
return {
    bonus = 3,
    next = function(calls)
        return calls + 1
    end,
}
`

const (
	RouteDamage uint16 = 1
)

type CommandKind uint8

const (
	CommandDraw  CommandKind = 1
	CommandFlash CommandKind = 2
)

type EffectKind uint8

const (
	EffectLoad  EffectKind = 1
	EffectAudio EffectKind = 2
)

type CompletionStatus uint8

const (
	CompletionOK CompletionStatus = 1
)

type Projection struct {
	Step int64
	Seed int64
	Work int64
}

type DamageEvent struct {
	Route  uint16
	Entity uint32
	Amount int64
}

type Completion struct {
	EffectID uint64
	Status   CompletionStatus
	Value    int64
}

type TurnRequest struct {
	Sequence    uint64
	Revision    uint64
	State       StateSnapshot
	Projection  Projection
	Events      []DamageEvent
	Completions []Completion
}

type StateSnapshot struct {
	Tick        int64
	Total       int64
	Ready       int64
	Entity7     int64
	ModuleCalls int64
}

type Command struct {
	Kind   CommandKind
	Entity uint32
	A      int64
	B      int64
}

type Effect struct {
	ID              uint64
	Kind            EffectKind
	Entity          uint32
	Value           int64
	NeedsCompletion bool
}

type TurnResult struct {
	Sequence uint64
	Revision uint64
	State    StateSnapshot
	Commands []Command
	Effects  []Effect
	Pending  []uint64
}

// Checkpoint is the complete application-owned state needed to create a new
// handler at a quiescent transaction boundary. Generation-owned callbacks and
// suspensions are deliberately absent and therefore must be drained first.
type Checkpoint struct {
	Sequence uint64
	Revision uint64
	State    StateSnapshot
}

// BatchRequest is a closed performance-observer record. The production-shaped
// deployment seam is TurnRequest; this second shape only exercises static-AOT
// guest-call throughput without exposing Runtime or Invocation remotely.
type BatchRequest struct {
	Iterations uint32
	Seed       int64
}

type BatchResult struct {
	Checksum int64
}
