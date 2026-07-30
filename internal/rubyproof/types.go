// Package rubyproof is a private, bounded Ruby semantic and prepared-code
// architecture proof. It deliberately owns its object model, method tables,
// unwind rules, and execution limits instead of reusing Ember's Luau runtime.
package rubyproof

import (
	"context"
	"crypto/sha256"
	"errors"

	"github.com/besmpl/ember/preparedsource"
)

// Result is the closed, detached Go projection of ProofSource's result array.
// No Ruby owner-bound value escapes through this record.
type Result struct {
	Same, Other       bool
	Before            int64
	BetaBefore        int64
	GammaBefore       int64
	AlphaWarm         int64
	BetaWarm          int64
	After             int64
	AlphaAfterHit     int64
	BetaAfter         int64
	GammaAfter        int64
	Saved             int64
	PublicBefore      int64
	ProtectedRejected int64
	PrivateRejected   int64
	PrivateSent       int64
	PrivateBeta       int64
	PublicRestored    int64
	BetaRemoved       int64
	Returned          int64
	Value             int64
	Trace             int64
	BetaTrace         int64
	GammaTrace        int64
	Topology          TopologyResult
	Singleton         SingletonResult
}

// TopologyResult is the detached C1 module/include/prepend record. Keeping it
// nested preserves Result's original twenty-four-field record as one exact
// semantic unit.
type TopologyResult struct {
	DeltaBefore    int64
	DeltaIncluded  int64
	BetaRoute      int64
	BetaDefined    int64
	DeltaRedefined int64
	SavedStable    int64
	Trace          int64
}

// SingletonResult is the detached C2 receiver-specific method record. The
// receiver, its singleton lookup row, and every generation-local identity
// remain owned by the Ruby runtime; only these scalar observations escape.
type SingletonResult struct {
	Before     int64
	Warm       int64
	PeerBefore int64
	After      int64
	AfterHit   int64
	PeerAfter  int64
	Trace      int64
}

// Limits are fresh for each Run. Zero is a real zero budget rather than an
// alias for an implicit default.
type Limits struct {
	Steps   uint64
	Frames  uint64
	Objects uint64
}

// ProofLimits returns a sufficient deterministic budget for ProofSource.
func ProofLimits() Limits {
	return Limits{Steps: 1_024, Frames: 32, Objects: 15}
}

var (
	// ErrStepLimit reports exhaustion of the Ruby evaluator's statement and
	// expression budget.
	ErrStepLimit = errors.New("ruby: step limit exceeded")
	// ErrFrameLimit reports exhaustion of the Ruby method/block frame budget.
	ErrFrameLimit = errors.New("ruby: frame limit exceeded")
	// ErrObjectLimit reports exhaustion of the Ruby object allocation budget.
	ErrObjectLimit = errors.New("ruby: object limit exceeded")
	// ErrBusy reports concurrent use of one mutable Ruby owner.
	ErrBusy = errors.New("ruby: runtime is busy")
	// ErrClosed reports use after Close.
	ErrClosed = errors.New("ruby: runtime is closed")
	// ErrPoisoned reports reuse after cancellation or a resource limit aborted
	// execution before Ruby cleanup could be proved complete.
	ErrPoisoned = errors.New("ruby: runtime is poisoned")
)

// Program is one immutable checked Ruby program. Its syntax and semantic facts
// are private and are not a shared language IR.
type Program struct{ checked *checkedProgram }

// IsZero reports whether p is the invalid zero Program.
func (p Program) IsZero() bool { return p.checked == nil }

// Identity returns the source- and semantic-version-bound program identity.
func (p Program) Identity() [sha256.Size]byte {
	if p.checked == nil {
		return [sha256.Size]byte{}
	}
	return p.checked.identity
}

// NewRuntime binds one mutable, single-call-at-a-time Ruby owner to p.
func (p Program) NewRuntime() (*Runtime, error) {
	if p.checked == nil {
		return nil, errors.New("ruby: zero program")
	}
	return newRuntime(p.checked), nil
}

// PreparedSource emits one location-neutral static Go package. Placement,
// module layout, building, and reload remain application/delivery concerns.
func (p Program) PreparedSource(packageName string) (preparedsource.Set, error) {
	if p.checked == nil {
		return preparedsource.Set{}, errors.New("ruby: zero program")
	}
	return emitPreparedSource(p.checked, packageName)
}

// Run is a convenience for a fresh owner. It is intentionally not the hot
// embedding surface; repeated callers should own and close a Runtime.
func (p Program) Run(ctx context.Context, limits Limits) (Result, error) {
	runtime, err := p.NewRuntime()
	if err != nil {
		return Result{}, err
	}
	defer runtime.Close()
	return runtime.Run(ctx, limits)
}
