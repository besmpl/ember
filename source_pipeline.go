package ember

import (
	"errors"
	"sync"
)

type sourceArtifact struct {
	source  Source
	metrics sourceMetrics
	program program
	bind    bindResult
	proto   *Proto
	check   *checkArtifact
}

type sourceMetrics struct {
	sourceBytes uint64
	tokens      uint64
	nesting     uint32
	syntaxNodes uint64
}

func validateSourceByteLimit(used, limit uint64) error {
	if limit != 0 && used > limit {
		return &LimitError{Kind: LimitSourceBytes, Limit: limit, Used: used}
	}
	return nil
}

func (metrics sourceMetrics) validate(limits CompileLimits) error {
	checks := []struct {
		kind  LimitKind
		limit uint64
		used  uint64
	}{
		{LimitSourceBytes, limits.MaxSourceBytes, metrics.sourceBytes},
		{LimitTokens, limits.MaxTokens, metrics.tokens},
		{LimitNesting, uint64(limits.MaxNesting), uint64(metrics.nesting)},
		{LimitSyntaxNodes, limits.MaxSyntaxNodes, metrics.syntaxNodes},
	}
	for _, check := range checks {
		if check.limit != 0 && check.used > check.limit {
			return &LimitError{Kind: check.kind, Limit: check.limit, Used: check.used}
		}
	}
	return nil
}

type sourceArtifactStore struct {
	mu                sync.Mutex
	artifacts         map[sourceIdentity]sourceArtifact
	preparing         map[sourceIdentity]*sourceArtifactPreparation
	prepare           func(Source) (sourceArtifact, error)
	prepareWithLimits func(Source, CompileLimits) (sourceArtifact, error)
}

type sourceArtifactPreparation struct {
	done     chan struct{}
	limits   CompileLimits
	artifact sourceArtifact
	err      error
}

func (p *sourceArtifactPreparation) retryFor(limits CompileLimits) bool {
	return p != nil && p.limits != limits && errors.Is(p.err, ErrLimitExceeded)
}

func parseSource(source Source) (sourceArtifact, error) {
	return parseSourceWithLimits(source, CompileLimits{})
}

func parseSourceWithLimits(source Source, limits CompileLimits) (sourceArtifact, error) {
	if limits.MaxSourceBytes != 0 && uint64(len(source.Text)) > limits.MaxSourceBytes {
		return sourceArtifact{}, &LimitError{Kind: LimitSourceBytes, Limit: limits.MaxSourceBytes, Used: uint64(len(source.Text))}
	}
	p := parser{source: source.Text, limits: limits}
	prog, err := p.parse()
	if err != nil {
		return sourceArtifact{}, err
	}
	return sourceArtifact{
		source: source,
		metrics: sourceMetrics{
			sourceBytes: uint64(len(source.Text)),
			tokens:      uint64(len(p.tokens)),
			nesting:     p.maxNesting,
			syntaxNodes: uint64(prog.nodeCount),
		},
		program: prog,
		bind:    bindProgram(prog),
	}, nil
}

func newSourceArtifactStore() *sourceArtifactStore {
	store := newSourceArtifactStoreWithPrepare(parseSource)
	store.prepareWithLimits = parseSourceWithLimits
	return store
}

func newSourceArtifactStoreWithPrepare(prepare func(Source) (sourceArtifact, error)) *sourceArtifactStore {
	return &sourceArtifactStore{
		artifacts: make(map[sourceIdentity]sourceArtifact),
		preparing: make(map[sourceIdentity]*sourceArtifactPreparation),
		prepare:   prepare,
		prepareWithLimits: func(source Source, limits CompileLimits) (sourceArtifact, error) {
			artifact, err := prepare(source)
			if err != nil {
				return sourceArtifact{}, err
			}
			if err := artifact.metrics.validate(limits); err != nil {
				return sourceArtifact{}, err
			}
			return artifact, nil
		},
	}
}

func (s *sourceArtifactStore) parse(source Source, identity sourceIdentity) (sourceArtifact, error) {
	return s.parseWithLimits(source, identity, CompileLimits{})
}

func (s *sourceArtifactStore) parseWithLimits(source Source, identity sourceIdentity, limits CompileLimits) (sourceArtifact, error) {
	if err := validateSourceByteLimit(uint64(len(source.Text)), limits.MaxSourceBytes); err != nil {
		return sourceArtifact{}, err
	}
	if s == nil {
		return parseSourceWithLimits(source, limits)
	}

	s.mu.Lock()
	if artifact, ok := s.artifacts[identity]; ok {
		s.mu.Unlock()
		if err := artifact.metrics.validate(limits); err != nil {
			return sourceArtifact{}, err
		}
		return artifact, nil
	}
	if preparation, ok := s.preparing[identity]; ok {
		s.mu.Unlock()
		<-preparation.done
		if preparation.err != nil {
			if preparation.retryFor(limits) {
				return s.parseWithLimits(source, identity, limits)
			}
			return sourceArtifact{}, preparation.err
		}
		if err := preparation.artifact.metrics.validate(limits); err != nil {
			return sourceArtifact{}, err
		}
		return preparation.artifact, preparation.err
	}
	preparation := &sourceArtifactPreparation{done: make(chan struct{}), limits: limits}
	s.preparing[identity] = preparation
	s.mu.Unlock()

	prepare := s.prepareWithLimits
	if prepare == nil {
		prepare = func(source Source, _ CompileLimits) (sourceArtifact, error) { return s.prepare(source) }
	}
	artifact, err := prepare(source, limits)
	if err == nil {
		err = artifact.metrics.validate(limits)
	}

	s.mu.Lock()
	if err == nil {
		if stored, ok := s.artifacts[identity]; ok {
			artifact = stored
		} else {
			s.artifacts[identity] = artifact
		}
	}
	preparation.artifact = artifact
	preparation.err = err
	delete(s.preparing, identity)
	close(preparation.done)
	s.mu.Unlock()

	if err != nil {
		return sourceArtifact{}, err
	}
	return artifact, nil
}

func (s *sourceArtifactStore) compile(source Source, identity sourceIdentity) (*Proto, error) {
	return s.compileWithLimits(source, identity, CompileLimits{})
}

func (s *sourceArtifactStore) compileWithLimits(source Source, identity sourceIdentity, limits CompileLimits) (*Proto, error) {
	if s == nil {
		artifact, err := parseSourceWithLimits(source, limits)
		if err != nil {
			return nil, err
		}
		return compileProgram(artifact)
	}
	artifact, err := s.parseWithLimits(source, identity, limits)
	if err != nil {
		return nil, err
	}
	if artifact.proto != nil {
		return artifact.proto, nil
	}
	proto, err := compileProgram(artifact)
	if err != nil {
		return nil, err
	}
	return s.storeCompiled(identity, artifact, proto), nil
}

func (s *sourceArtifactStore) check(source Source, identity sourceIdentity) (checkArtifact, error) {
	return s.checkWithLimits(source, identity, CompileLimits{})
}

func (s *sourceArtifactStore) checkWithLimits(source Source, identity sourceIdentity, limits CompileLimits) (checkArtifact, error) {
	if s == nil {
		artifact, err := parseSourceWithLimits(source, limits)
		if err != nil {
			return checkArtifact{}, err
		}
		return buildCheckArtifact(artifact)
	}
	artifact, err := s.parseWithLimits(source, identity, limits)
	if err != nil {
		return checkArtifact{}, err
	}
	if artifact.check != nil {
		return *artifact.check, nil
	}
	check, err := buildCheckArtifact(artifact)
	if err != nil {
		return checkArtifact{}, err
	}
	return s.storeChecked(identity, artifact, check), nil
}

func (s *sourceArtifactStore) storeCompiled(identity sourceIdentity, artifact sourceArtifact, proto *Proto) *Proto {
	s.mu.Lock()
	defer s.mu.Unlock()
	if stored, ok := s.artifacts[identity]; ok {
		if stored.proto != nil {
			return stored.proto
		}
		artifact = stored
	}
	artifact.proto = proto
	s.artifacts[identity] = artifact
	return proto
}

func (s *sourceArtifactStore) storeChecked(identity sourceIdentity, artifact sourceArtifact, check checkArtifact) checkArtifact {
	s.mu.Lock()
	defer s.mu.Unlock()
	if stored, ok := s.artifacts[identity]; ok {
		if stored.check != nil {
			return *stored.check
		}
		artifact = stored
	}
	artifact.check = &check
	s.artifacts[identity] = artifact
	return check
}
