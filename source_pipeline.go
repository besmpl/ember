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
}

type sourceArtifactStoreSnapshot struct {
	artifacts map[sourceIdentity]sourceArtifact
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
	return &sourceArtifactStore{
		artifacts: make(map[sourceIdentity]sourceArtifact),
	}
}

func (s *sourceArtifactStore) snapshot() sourceArtifactStoreSnapshot {
	if s == nil {
		return sourceArtifactStoreSnapshot{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return sourceArtifactStoreSnapshot{
		artifacts: copySourceArtifacts(s.artifacts),
	}
}

func (s *sourceArtifactStore) restore(snapshot sourceArtifactStoreSnapshot) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.artifacts = snapshot.artifacts
}

func (s *sourceArtifactStore) parse(source Source, identity sourceIdentity) (sourceArtifact, error) {
	if s == nil {
		return parseSource(source)
	}
	if artifact, ok := s.artifact(identity); ok {
		return artifact, nil
	}
	artifact, err := parseSource(source)
	if err != nil {
		return sourceArtifact{}, err
	}
	artifact.identity = identity
	return s.storeParsed(identity, artifact), nil
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

func copySourceArtifacts(values map[sourceIdentity]sourceArtifact) map[sourceIdentity]sourceArtifact {
	copied := make(map[sourceIdentity]sourceArtifact, len(values))
	for key, value := range values {
		copied[key] = value
	}
	return copied
}

func (s *sourceArtifactStore) artifact(identity sourceIdentity) (sourceArtifact, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	artifact, ok := s.artifacts[identity]
	return artifact, ok
}

func (s *sourceArtifactStore) storeParsed(identity sourceIdentity, artifact sourceArtifact) sourceArtifact {
	s.mu.Lock()
	defer s.mu.Unlock()
	if stored, ok := s.artifacts[identity]; ok {
		return stored
	}
	s.artifacts[identity] = artifact
	return artifact
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
