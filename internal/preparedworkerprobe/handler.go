package preparedworkerprobe

import (
	"context"
	"fmt"
	"math"
	"sort"

	"github.com/besmpl/ember"
	"github.com/besmpl/ember/internal/preparedworkerfixture"
	preparedworkerfixturegenerated "github.com/besmpl/ember/internal/preparedworkerfixture/generated"
)

const (
	maxTurnItems          = 64
	maxExactGuestInteger  = int64(1<<53 - 1)
	maxEffectIDSequence   = ^uint64(0) >> 8
	maxPreparedWorkerSeed = int64(20)
)

type Handler struct {
	runtime   *ember.Runtime
	callbacks map[uint16]ember.Callback
	pending   map[uint64]pendingEffect
	active    *turnBuffers
	sequence  uint64
	revision  uint64
	state     preparedworkerfixture.StateSnapshot
	terminal  error
}

type turnBuffers struct {
	sequence uint64
	commands []preparedworkerfixture.Command
	effects  []preparedworkerfixture.Effect
}

type pendingEffect struct {
	suspension *ember.Suspension
	entity     uint32
	amount     int64
}

func NewHandler() (*Handler, error) {
	return NewHandlerAt(preparedworkerfixture.Checkpoint{})
}

func QuiescentCheckpoint(result preparedworkerfixture.TurnResult) (preparedworkerfixture.Checkpoint, error) {
	if len(result.Pending) != 0 {
		return preparedworkerfixture.Checkpoint{}, fmt.Errorf(
			"prepare worker fixture: generation has %d pending continuations",
			len(result.Pending),
		)
	}
	return preparedworkerfixture.Checkpoint{
		Sequence: result.Sequence,
		Revision: result.Revision,
		State:    result.State,
	}, nil
}

func NewHandlerAt(checkpoint preparedworkerfixture.Checkpoint) (*Handler, error) {
	if checkpoint.Sequence > maxEffectIDSequence {
		return nil, fmt.Errorf("prepare worker fixture: checkpoint sequence %d is out of bounds", checkpoint.Sequence)
	}
	if checkpoint.Revision == ^uint64(0) {
		return nil, fmt.Errorf("prepare worker fixture: checkpoint revision is exhausted")
	}
	if err := validateState(checkpoint.State, "checkpoint state"); err != nil {
		return nil, fmt.Errorf("prepare worker fixture: %w", err)
	}
	loader := sourceLoader{
		preparedworkerfixture.MainModule:    preparedworkerfixture.MainSource,
		preparedworkerfixture.NumericModule: preparedworkerfixture.NumericSource,
		preparedworkerfixture.SharedModule:  preparedworkerfixture.SharedSource,
	}
	main := ember.LogicalModule("prepared-worker/main")
	program, _, err := ember.LoadProgram(context.Background(), loader, ember.ProgramOptions{
		Entrypoints: []ember.Entrypoint{{Name: "main", Module: main}},
		Parallelism: 1,
	})
	if err != nil {
		return nil, fmt.Errorf("prepare worker fixture: load Program: %w", err)
	}
	runtime, err := program.NewRuntime(ember.RuntimeOptions{Prepared: preparedworkerfixturegenerated.Bundle})
	if err != nil {
		return nil, fmt.Errorf("prepare worker fixture: bind bundle: %w", err)
	}
	handler := &Handler{
		runtime:   runtime,
		callbacks: make(map[uint16]ember.Callback),
		pending:   make(map[uint64]pendingEffect),
		sequence:  checkpoint.Sequence,
		revision:  checkpoint.Revision,
		state:     checkpoint.State,
	}
	handler.active = &turnBuffers{}
	_, err = runtime.Invoke(context.Background(), ember.Invocation{
		Module:  main,
		Export:  "startup",
		Globals: handler.globals(),
	})
	handler.active = nil
	if err != nil {
		_ = runtime.Close()
		return nil, fmt.Errorf("prepare worker fixture: startup: %w", err)
	}
	if _, ok := handler.callbacks[preparedworkerfixture.RouteDamage]; !ok {
		_ = runtime.Close()
		return nil, fmt.Errorf("prepare worker fixture: startup did not capture damage route")
	}
	return handler, nil
}

func (handler *Handler) Transact(
	ctx context.Context,
	request preparedworkerfixture.TurnRequest,
) (preparedworkerfixture.TurnResult, error) {
	if handler == nil || handler.runtime == nil {
		return preparedworkerfixture.TurnResult{}, fmt.Errorf("prepared worker fixture: closed")
	}
	if handler.terminal != nil {
		return preparedworkerfixture.TurnResult{}, fmt.Errorf(
			"prepared worker fixture: terminal transaction failure: %w",
			handler.terminal,
		)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return preparedworkerfixture.TurnResult{}, err
	}
	if err := handler.validateRequest(request); err != nil {
		return preparedworkerfixture.TurnResult{}, err
	}
	result, err := handler.transactValidated(ctx, request)
	if err != nil {
		handler.terminal = err
		return preparedworkerfixture.TurnResult{}, err
	}
	return result, nil
}

func (handler *Handler) transactValidated(
	ctx context.Context,
	request preparedworkerfixture.TurnRequest,
) (preparedworkerfixture.TurnResult, error) {
	buffers := &turnBuffers{sequence: request.Sequence}
	handler.active = buffers
	defer func() { handler.active = nil }()
	state := request.State

	for _, completion := range request.Completions {
		pending := handler.pending[completion.EffectID]
		delete(handler.pending, completion.EffectID)
		step, err := pending.suspension.Resume(ctx, ember.NumberValue(float64(completion.Value)))
		if err != nil {
			return preparedworkerfixture.TurnResult{}, fmt.Errorf(
				"prepared worker fixture: complete effect %d: %w",
				completion.EffectID,
				err,
			)
		}
		if len(step.Suspensions) != 0 {
			return preparedworkerfixture.TurnResult{}, fmt.Errorf(
				"prepared worker fixture: completion %d suspended again",
				completion.EffectID,
			)
		}
		if err := applyDamageCompletion(&state, pending, completion.Value, step.Values); err != nil {
			return preparedworkerfixture.TurnResult{}, err
		}
	}
	for _, event := range request.Events {
		callback := handler.callbacks[event.Route]
		step, err := callback.CallResumable(
			ctx,
			ember.NumberValue(float64(event.Entity)),
			ember.NumberValue(float64(event.Amount)),
		)
		if err != nil {
			return preparedworkerfixture.TurnResult{}, fmt.Errorf(
				"prepared worker fixture: route %d: %w",
				event.Route,
				err,
			)
		}
		if len(step.Suspensions) != 1 || len(step.Values) != 0 {
			return preparedworkerfixture.TurnResult{}, fmt.Errorf(
				"prepared worker fixture: route %d produced %d values and %d suspensions, want 0 and 1",
				event.Route,
				len(step.Values),
				len(step.Suspensions),
			)
		}
		if err := handler.retainSuspension(step.Suspensions[0], event); err != nil {
			return preparedworkerfixture.TurnResult{}, err
		}
	}

	values, err := handler.runtime.Invoke(ctx, ember.Invocation{
		Module:  ember.LogicalModule("prepared-worker/main"),
		Export:  "turn",
		Globals: handler.globals(),
	},
		ember.NumberValue(float64(state.Tick)),
		ember.NumberValue(float64(state.Total)),
		ember.NumberValue(float64(state.Ready)),
		ember.NumberValue(float64(state.Entity7)),
		ember.NumberValue(float64(state.ModuleCalls)),
		ember.NumberValue(float64(request.Projection.Step)),
		ember.NumberValue(float64(request.Projection.Seed)),
		ember.NumberValue(float64(request.Projection.Work)),
	)
	if err != nil {
		return preparedworkerfixture.TurnResult{}, fmt.Errorf("prepared worker fixture: turn: %w", err)
	}
	state, err = turnState(values)
	if err != nil {
		return preparedworkerfixture.TurnResult{}, err
	}
	handler.sequence = request.Sequence
	handler.revision++
	handler.state = state
	return preparedworkerfixture.TurnResult{
		Sequence: request.Sequence,
		Revision: handler.revision,
		State:    state,
		Commands: append([]preparedworkerfixture.Command(nil), buffers.commands...),
		Effects:  append([]preparedworkerfixture.Effect(nil), buffers.effects...),
		Pending:  handler.pendingIDs(),
	}, nil
}

func (handler *Handler) Close() error {
	if handler == nil || handler.runtime == nil {
		return nil
	}
	var first error
	for _, pending := range handler.pending {
		if err := pending.suspension.Cancel(); err != nil && first == nil {
			first = err
		}
	}
	for _, callback := range handler.callbacks {
		if err := callback.Close(); err != nil && first == nil {
			first = err
		}
	}
	if err := handler.runtime.Close(); err != nil && first == nil {
		first = err
	}
	handler.runtime = nil
	return first
}

func (handler *Handler) validateRequest(request preparedworkerfixture.TurnRequest) error {
	if handler.sequence == maxEffectIDSequence {
		return fmt.Errorf("prepared worker fixture: sequence space is exhausted")
	}
	if handler.revision == ^uint64(0) {
		return fmt.Errorf("prepared worker fixture: revision space is exhausted")
	}
	if request.Sequence != handler.sequence+1 {
		return fmt.Errorf(
			"prepared worker fixture: sequence %d, want %d",
			request.Sequence,
			handler.sequence+1,
		)
	}
	if request.Revision != handler.revision {
		return fmt.Errorf(
			"prepared worker fixture: revision %d, want %d",
			request.Revision,
			handler.revision,
		)
	}
	if request.State != handler.state {
		return fmt.Errorf("prepared worker fixture: request state does not match revision %d", handler.revision)
	}
	if err := validateState(request.State, "request state"); err != nil {
		return fmt.Errorf("prepared worker fixture: %w", err)
	}
	if err := exactGuestInteger(request.Projection.Step, "projection step"); err != nil {
		return fmt.Errorf("prepared worker fixture: %w", err)
	}
	if err := exactGuestInteger(request.Projection.Seed, "projection seed"); err != nil {
		return fmt.Errorf("prepared worker fixture: %w", err)
	}
	if request.Projection.Seed < 0 || request.Projection.Seed > maxPreparedWorkerSeed {
		return fmt.Errorf("prepared worker fixture: seed %d is out of bounds", request.Projection.Seed)
	}
	if request.Projection.Work <= 0 || request.Projection.Work > 1_000_000 {
		return fmt.Errorf("prepared worker fixture: work %d is out of bounds", request.Projection.Work)
	}
	if len(request.Events) >= maxTurnItems || len(request.Completions) >= maxTurnItems {
		return fmt.Errorf("prepared worker fixture: turn leaves no room for its mandatory output")
	}
	remainingPending := len(handler.pending) - len(request.Completions) + len(request.Events)
	if remainingPending > maxTurnItems {
		return fmt.Errorf("prepared worker fixture: turn would retain %d pending effects", remainingPending)
	}
	for _, event := range request.Events {
		if _, ok := handler.callbacks[event.Route]; !ok {
			return fmt.Errorf("prepared worker fixture: unknown event route %d", event.Route)
		}
		if err := exactGuestInteger(event.Amount, "event amount"); err != nil {
			return fmt.Errorf("prepared worker fixture: %w", err)
		}
	}
	seen := make(map[uint64]bool, len(request.Completions))
	for _, completion := range request.Completions {
		if completion.Status != preparedworkerfixture.CompletionOK {
			return fmt.Errorf("prepared worker fixture: unsupported completion status %d", completion.Status)
		}
		if err := exactGuestInteger(completion.Value, "completion value"); err != nil {
			return fmt.Errorf("prepared worker fixture: %w", err)
		}
		if seen[completion.EffectID] {
			return fmt.Errorf("prepared worker fixture: duplicate completion %d", completion.EffectID)
		}
		seen[completion.EffectID] = true
		if pending, ok := handler.pending[completion.EffectID]; !ok || pending.suspension == nil {
			return fmt.Errorf("prepared worker fixture: stale completion %d", completion.EffectID)
		}
	}
	return nil
}

func (handler *Handler) globals() map[string]ember.Value {
	return map[string]ember.Value{
		"capture": ember.ContextHostFuncValue(handler.capture),
		"command": ember.ContextHostFuncValue(handler.command),
		"effect":  ember.ContextHostFuncValue(handler.effect),
		"wait":    ember.ResumableHostFuncValue(handler.wait),
	}
}

func (handler *Handler) capture(ctx context.Context, args []ember.Value) ([]ember.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("capture: got %d arguments, want 2", len(args))
	}
	route, err := uint16Argument(args[0], "capture route")
	if err != nil {
		return nil, err
	}
	if _, exists := handler.callbacks[route]; exists {
		return nil, fmt.Errorf("capture: duplicate route %d", route)
	}
	callback, err := ember.CaptureCallback(ctx, args[1])
	if err != nil {
		return nil, err
	}
	handler.callbacks[route] = callback
	return nil, nil
}

func (handler *Handler) command(_ context.Context, args []ember.Value) ([]ember.Value, error) {
	if handler.active == nil || len(args) != 4 {
		return nil, fmt.Errorf("command: unavailable or malformed")
	}
	kind, err := uint8Argument(args[0], "command kind")
	if err != nil {
		return nil, err
	}
	entity, err := uint32Argument(args[1], "command entity")
	if err != nil {
		return nil, err
	}
	a, err := int64Argument(args[2], "command a")
	if err != nil {
		return nil, err
	}
	b, err := int64Argument(args[3], "command b")
	if err != nil {
		return nil, err
	}
	handler.active.commands = append(handler.active.commands, preparedworkerfixture.Command{
		Kind:   preparedworkerfixture.CommandKind(kind),
		Entity: entity,
		A:      a,
		B:      b,
	})
	return nil, nil
}

func (handler *Handler) effect(_ context.Context, args []ember.Value) ([]ember.Value, error) {
	if handler.active == nil || len(args) != 3 {
		return nil, fmt.Errorf("effect: unavailable or malformed")
	}
	kind, err := uint8Argument(args[0], "effect kind")
	if err != nil {
		return nil, err
	}
	entity, err := uint32Argument(args[1], "effect entity")
	if err != nil {
		return nil, err
	}
	value, err := int64Argument(args[2], "effect value")
	if err != nil {
		return nil, err
	}
	id, err := handler.takeEffectID()
	if err != nil {
		return nil, err
	}
	handler.active.effects = append(handler.active.effects, preparedworkerfixture.Effect{
		ID:     id,
		Kind:   preparedworkerfixture.EffectKind(kind),
		Entity: entity,
		Value:  value,
	})
	return nil, nil
}

func (handler *Handler) wait(_ context.Context, args []ember.Value) ember.HostResult {
	if handler.active == nil || len(args) != 2 {
		return ember.HostError(fmt.Errorf("wait: unavailable or malformed"))
	}
	kind, err := uint8Argument(args[0], "wait kind")
	if err != nil {
		return ember.HostError(err)
	}
	entity, err := uint32Argument(args[1], "wait entity")
	if err != nil {
		return ember.HostError(err)
	}
	id, err := handler.takeEffectID()
	if err != nil {
		return ember.HostError(err)
	}
	handler.active.effects = append(handler.active.effects, preparedworkerfixture.Effect{
		ID:              id,
		Kind:            preparedworkerfixture.EffectKind(kind),
		Entity:          entity,
		NeedsCompletion: true,
	})
	return ember.HostSuspend(id)
}

func (handler *Handler) takeEffectID() (uint64, error) {
	if handler.active == nil || handler.active.sequence == 0 {
		return 0, fmt.Errorf("prepared worker fixture: effect outside a transaction")
	}
	ordinal := len(handler.active.effects) + 1
	if ordinal > maxTurnItems {
		return 0, fmt.Errorf("prepared worker fixture: turn produced too many effects")
	}
	return handler.active.sequence<<8 | uint64(ordinal), nil
}

func (handler *Handler) retainSuspension(
	suspension *ember.Suspension,
	event preparedworkerfixture.DamageEvent,
) error {
	id, ok := suspension.Token().(uint64)
	if !ok || id == 0 {
		return fmt.Errorf("prepared worker fixture: suspension token %#v is not an effect ID", suspension.Token())
	}
	if _, exists := handler.pending[id]; exists {
		return fmt.Errorf("prepared worker fixture: duplicate pending effect %d", id)
	}
	handler.pending[id] = pendingEffect{suspension: suspension, entity: event.Entity, amount: event.Amount}
	return nil
}

func (handler *Handler) pendingIDs() []uint64 {
	if len(handler.pending) == 0 {
		return nil
	}
	ids := make([]uint64, 0, len(handler.pending))
	for id := range handler.pending {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func turnState(values []ember.Value) (preparedworkerfixture.StateSnapshot, error) {
	if len(values) != 5 {
		return preparedworkerfixture.StateSnapshot{}, fmt.Errorf(
			"prepared worker fixture: turn returned %d values, want 5",
			len(values),
		)
	}
	fields := make([]int64, len(values))
	for index, value := range values {
		field, err := int64Argument(value, fmt.Sprintf("turn result %d", index))
		if err != nil {
			return preparedworkerfixture.StateSnapshot{}, err
		}
		fields[index] = field
	}
	return preparedworkerfixture.StateSnapshot{
		Tick:        fields[0],
		Total:       fields[1],
		Ready:       fields[2],
		Entity7:     fields[3],
		ModuleCalls: fields[4],
	}, nil
}

func applyDamageCompletion(
	state *preparedworkerfixture.StateSnapshot,
	pending pendingEffect,
	loaded int64,
	values []ember.Value,
) error {
	if len(values) != 3 {
		return fmt.Errorf("prepared worker fixture: damage completion returned %d values, want 3", len(values))
	}
	fields := make([]int64, len(values))
	for index, value := range values {
		field, err := int64Argument(value, fmt.Sprintf("damage completion result %d", index))
		if err != nil {
			return err
		}
		fields[index] = field
	}
	wantTotalDelta, err := sumExactGuestIntegers("damage total delta", pending.amount, loaded, 3)
	if err != nil {
		return err
	}
	wantEntityDelta := int64(0)
	if pending.entity == 7 {
		wantEntityDelta = pending.amount
	}
	if fields[0] != loaded || fields[1] != wantTotalDelta || fields[2] != wantEntityDelta {
		return fmt.Errorf(
			"prepared worker fixture: damage completion values %v, want [%d %d %d]",
			fields,
			loaded,
			wantTotalDelta,
			wantEntityDelta,
		)
	}
	ready, err := sumExactGuestIntegers("ready state", state.Ready, loaded)
	if err != nil {
		return err
	}
	total, err := sumExactGuestIntegers("total state", state.Total, wantTotalDelta)
	if err != nil {
		return err
	}
	entity7, err := sumExactGuestIntegers("entity state", state.Entity7, wantEntityDelta)
	if err != nil {
		return err
	}
	state.Ready = ready
	state.Total = total
	state.Entity7 = entity7
	return nil
}

func validateState(state preparedworkerfixture.StateSnapshot, name string) error {
	fields := []struct {
		name  string
		value int64
	}{
		{name: "tick", value: state.Tick},
		{name: "total", value: state.Total},
		{name: "ready", value: state.Ready},
		{name: "entity7", value: state.Entity7},
		{name: "module calls", value: state.ModuleCalls},
	}
	for _, field := range fields {
		if err := exactGuestInteger(field.value, name+" "+field.name); err != nil {
			return err
		}
	}
	return nil
}

func sumExactGuestIntegers(name string, values ...int64) (int64, error) {
	var sum int64
	for _, value := range values {
		sum += value
	}
	if err := exactGuestInteger(sum, name); err != nil {
		return 0, err
	}
	return sum, nil
}

func exactGuestInteger(value int64, name string) error {
	if value < -maxExactGuestInteger || value > maxExactGuestInteger {
		return fmt.Errorf("%s %d is outside the exact guest integer domain", name, value)
	}
	return nil
}

func int64Argument(value ember.Value, name string) (int64, error) {
	number, ok := value.Number()
	if !ok || math.Trunc(number) != number ||
		number < -float64(maxExactGuestInteger) || number > float64(maxExactGuestInteger) {
		return 0, fmt.Errorf("%s is not an exact guest integer", name)
	}
	return int64(number), nil
}

func uint8Argument(value ember.Value, name string) (uint8, error) {
	number, err := int64Argument(value, name)
	if err != nil || number < 0 || number > math.MaxUint8 {
		return 0, fmt.Errorf("%s is not a uint8", name)
	}
	return uint8(number), nil
}

func uint16Argument(value ember.Value, name string) (uint16, error) {
	number, err := int64Argument(value, name)
	if err != nil || number < 0 || number > math.MaxUint16 {
		return 0, fmt.Errorf("%s is not a uint16", name)
	}
	return uint16(number), nil
}

func uint32Argument(value ember.Value, name string) (uint32, error) {
	number, err := int64Argument(value, name)
	if err != nil || number < 0 || number > math.MaxUint32 {
		return 0, fmt.Errorf("%s is not a uint32", name)
	}
	return uint32(number), nil
}

type sourceLoader map[string]string

func (loader sourceLoader) LoadModule(_ context.Context, id ember.ModuleID) (ember.Source, error) {
	text, ok := loader[id.String()]
	if !ok {
		return ember.Source{}, fmt.Errorf("prepared worker fixture: missing module %s", id)
	}
	return ember.Source{Name: id.String(), Text: text}, nil
}
