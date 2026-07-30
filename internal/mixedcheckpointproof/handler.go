// Package mixedcheckpointproof is a private application-architecture proof.
// It composes concrete generated Ruby and Sprig packages behind one detached
// checkpoint without introducing a shared language runtime or ABI.
package mixedcheckpointproof

import (
	"context"
	"errors"
	"fmt"

	ruby "github.com/besmpl/ember/internal/rubyproof/generated"
	sprig "github.com/besmpl/ember/internal/sprigproof/generated"
)

const CheckpointVersion uint32 = 1

var (
	ErrNilContext = errors.New("mixed checkpoint proof: nil context")
	ErrClosed     = errors.New("mixed checkpoint proof: handler is closed")
	ErrPoisoned   = errors.New("mixed checkpoint proof: handler is poisoned")
	ErrCheckpoint = errors.New("mixed checkpoint proof: invalid checkpoint")
)

// Policy is application-owned resource policy. The languages retain distinct
// limit types and schedules rather than sharing a generic budget.
type Policy struct {
	Ruby       ruby.Limits
	SprigSteps uint64
}

func ProofPolicy() Policy {
	return Policy{Ruby: ruby.ProofLimits(), SprigSteps: 4}
}

// Request is the proof application's concrete transaction input.
type Request struct {
	Reading     sprig.Reading
	AdvanceRuby bool
}

// Checkpoint is the complete detached durable state. Ruby object identity and
// method tables are deliberately absent; a restored generation gets a fresh
// Ruby owner. Sprig remains a pure value computation.
type Checkpoint struct {
	Version    uint32
	Ruby       ruby.State
	SprigTotal int64
}

func InitialCheckpoint() Checkpoint {
	return Checkpoint{Version: CheckpointVersion, Ruby: ruby.InitialState()}
}

// Result is one closed application projection. Domain and rejection are
// concrete Sprig outcomes; zero values mean the mixed transaction succeeded.
type Result struct {
	Domain        sprig.DomainCode
	Rejected      bool
	RejectionCode int64
	Ruby          ruby.State
	SprigTotal    int64
}

// Effect is the single application effect emitted by a successful mixed
// transaction. It is one typed record, not one exchange per language.
type Effect struct {
	RubyValue  int64
	RubyTrace  int64
	SprigTotal int64
}

// Handler owns one generation-local Ruby runtime and the current detached
// checkpoint. It is confined to the application transaction owner:
// preparedworker serializes Apply and Close, while a direct embedded caller
// must do the same. Ruby still independently admits access to its owner. This
// avoids a redundant second atomic gate in every mixed transaction.
type Handler struct {
	closed, poisoned bool
	ruby             *ruby.Engine
	checkpoint       Checkpoint
	policy           Policy
}

// NewHandler restores one complete application generation. All checkpoint
// validation precedes owner construction; a cancellation observed after
// construction closes the new owner before returning the error.
func NewHandler(ctx context.Context, checkpoint Checkpoint, policy Policy) (*Handler, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return newHandler(checkpoint, policy, func(*Handler) error { return ctx.Err() })
}

func newHandler(checkpoint Checkpoint, policy Policy, afterRestore func(*Handler) error) (*Handler, error) {
	if checkpoint.Version != CheckpointVersion {
		return nil, fmt.Errorf("%w: version %d", ErrCheckpoint, checkpoint.Version)
	}
	engine, err := ruby.NewEngineFromState(checkpoint.Ruby)
	if err != nil {
		return nil, fmt.Errorf("%w: Ruby state: %v", ErrCheckpoint, err)
	}
	handler := &Handler{ruby: engine, checkpoint: checkpoint, policy: policy}
	if afterRestore != nil {
		if err := afterRestore(handler); err != nil {
			return nil, errors.Join(err, handler.Close())
		}
	}
	return handler, nil
}

// Apply performs Sprig first. A typed Sprig domain/rejection result commits an
// unchanged checkpoint and cannot reach Ruby. After Ruby begins, every Go
// error poisons the complete handler and no checkpoint or effect is returned.
func (handler *Handler) Apply(ctx context.Context, request Request) (Result, Checkpoint, Effect, bool, error) {
	if err := handler.enter(ctx); err != nil {
		return Result{}, Checkpoint{}, Effect{}, false, err
	}

	readings := [...]sprig.Reading{
		{Value: handler.checkpoint.SprigTotal, Divisor: 1},
		request.Reading,
	}
	sprigResult, err := sprig.Reduce(ctx, readings[:], handler.policy.SprigSteps)
	if err != nil {
		return Result{}, Checkpoint{}, Effect{}, false, handler.poison(err)
	}
	if sprigResult.Tag == sprig.ResultError {
		result := handler.result()
		result.Domain = sprigResult.Code
		return result, handler.checkpoint, Effect{}, false, nil
	}
	if len(sprigResult.Rejected) != 0 {
		if len(sprigResult.Rejected) != 1 {
			return Result{}, Checkpoint{}, Effect{}, false, handler.poison(fmt.Errorf("mixed checkpoint proof: unexpected Sprig rejection count %d", len(sprigResult.Rejected)))
		}
		result := handler.result()
		result.Rejected = true
		result.RejectionCode = sprigResult.Rejected[0]
		return result, handler.checkpoint, Effect{}, false, nil
	}

	rubyState := handler.checkpoint.Ruby
	if request.AdvanceRuby {
		rubyState, err = handler.ruby.ApplyRescuedRaise(ctx, handler.policy.Ruby)
		if err != nil {
			return Result{}, Checkpoint{}, Effect{}, false, handler.poison(err)
		}
	} else {
		value, hotErr := handler.ruby.Hot(ctx)
		if hotErr != nil {
			return Result{}, Checkpoint{}, Effect{}, false, handler.poison(hotErr)
		}
		if value != rubyState.Value {
			return Result{}, Checkpoint{}, Effect{}, false, handler.poison(errors.New("mixed checkpoint proof: Ruby state projection differs from owner"))
		}
	}
	handler.checkpoint = Checkpoint{
		Version:    CheckpointVersion,
		Ruby:       rubyState,
		SprigTotal: sprigResult.Total,
	}
	result := handler.result()
	effect := Effect{RubyValue: rubyState.Value, RubyTrace: rubyState.Trace, SprigTotal: sprigResult.Total}
	return result, handler.checkpoint, effect, true, nil
}

func (handler *Handler) result() Result {
	return Result{Ruby: handler.checkpoint.Ruby, SprigTotal: handler.checkpoint.SprigTotal}
}

func (handler *Handler) enter(ctx context.Context) error {
	if handler == nil {
		return ErrClosed
	}
	if ctx == nil {
		return ErrNilContext
	}
	if handler.closed {
		return ErrClosed
	}
	if handler.poisoned {
		return ErrPoisoned
	}
	return nil
}

func (handler *Handler) poison(err error) error {
	handler.poisoned = true
	return err
}

// Close is idempotent. Cleanup does not stop merely because a caller's close
// context was canceled; the preparedworker adapter handles that policy.
func (handler *Handler) Close() error {
	if handler == nil || handler.closed {
		return nil
	}
	if handler.ruby != nil {
		if err := handler.ruby.Close(); err != nil {
			return err
		}
	}
	handler.closed = true
	handler.ruby = nil
	handler.checkpoint = Checkpoint{}
	return nil
}
