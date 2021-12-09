package exbana

type EntityStreamer interface {
	Peek() interface{}
	Read() interface{}
	Finished() bool
	Position() int
	SetPosition(int)
}

type PatternMatchResult struct {
	StartPosition int
	EndPosition   int
}

type PatternMatcher interface {
	Match(EntityStreamer) (bool, *PatternMatchResult)
	Identifier() string
}

type SingleEntityMatchFunction func(interface{}) bool

type SingleEntityMatch struct {
	identifier    string
	matchFunction SingleEntityMatchFunction
}

func NewSingleEntityMatch(identifier string, matchFunction SingleEntityMatchFunction) *SingleEntityMatch {
	return &SingleEntityMatch{
		identifier:    identifier,
		matchFunction: matchFunction,
	}
}

func (m *SingleEntityMatch) Identifier() string {
	return m.identifier
}

func (m *SingleEntityMatch) Match(s EntityStreamer) (bool, *PatternMatchResult) {
	pos := s.Position()

	if m.matchFunction(s.Read()) {
		return true, &PatternMatchResult{
			StartPosition: pos,
			EndPosition:   s.Position(),
		}
	}

	return false, nil
}

type ConcatenationMatch struct {
	identifier string
	patterns   []PatternMatcher
}

func NewConcatenationMatch(identifier string, patterns []PatternMatcher) *ConcatenationMatch {
	return &ConcatenationMatch{
		identifier: identifier,
		patterns:   patterns,
	}
}

func (m *ConcatenationMatch) Identifier() string {
	return m.identifier
}

func (m *ConcatenationMatch) Match(s EntityStreamer) (bool, *PatternMatchResult) {
	pos := s.Position()

	for _, pm := range m.patterns {
		match, _ := pm.Match(s)
		if !match {
			return false, nil
		}
	}

	return true, &PatternMatchResult{StartPosition: pos, EndPosition: s.Position()}
}
