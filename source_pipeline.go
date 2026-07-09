package ember

import "sync"

type sourceArtifact struct {
	identity sourceIdentity
	source   Source
	program  program
	bind     bindResult
	proto    *Proto
	check    *checkArtifact
}

type sourceArtifactStore struct {
	mu        sync.Mutex
	artifacts map[sourceIdentity]sourceArtifact
	preparing map[sourceIdentity]*sourceArtifactPreparation
	prepare   func(Source) (sourceArtifact, error)
}

type sourceArtifactPreparation struct {
	done     chan struct{}
	artifact sourceArtifact
	err      error
}

func parseSource(source Source) (sourceArtifact, error) {
	identity := identifyModuleSource(source)
	p := parser{source: source.Text}
	prog, err := p.parse()
	if err != nil {
		return sourceArtifact{}, err
	}
	return sourceArtifact{
		identity: identity,
		source:   source,
		program:  prog,
		bind:     bindProgram(prog),
	}, nil
}

func newSourceArtifactStore() *sourceArtifactStore {
	return newSourceArtifactStoreWithPrepare(parseSource)
}

func newSourceArtifactStoreWithPrepare(prepare func(Source) (sourceArtifact, error)) *sourceArtifactStore {
	return &sourceArtifactStore{
		artifacts: make(map[sourceIdentity]sourceArtifact),
		preparing: make(map[sourceIdentity]*sourceArtifactPreparation),
		prepare:   prepare,
	}
}

func (s *sourceArtifactStore) parse(source Source, identity sourceIdentity) (sourceArtifact, error) {
	if s == nil {
		return parseSource(source)
	}

	s.mu.Lock()
	if artifact, ok := s.artifacts[identity]; ok {
		s.mu.Unlock()
		return artifact, nil
	}
	if preparation, ok := s.preparing[identity]; ok {
		s.mu.Unlock()
		<-preparation.done
		return preparation.artifact, preparation.err
	}
	preparation := &sourceArtifactPreparation{done: make(chan struct{})}
	s.preparing[identity] = preparation
	s.mu.Unlock()

	artifact, err := s.prepare(source)
	if err == nil {
		artifact.identity = identity
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
	if s == nil {
		artifact, err := parseSource(source)
		if err != nil {
			return nil, err
		}
		return compileProgram(artifact)
	}
	artifact, err := s.parse(source, identity)
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
	if s == nil {
		return checkSource(source)
	}
	artifact, err := s.parse(source, identity)
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
	artifact.identity = identity
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
	artifact.identity = identity
	artifact.check = &check
	s.artifacts[identity] = artifact
	return check
}
