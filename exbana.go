package exbana

type EntityStreamer interface {
	Peek() interface{}
	Read() interface{}
	Finished() bool
	Position() int
	SetPosition(int)
}

type PatternMatchRange struct {
	Start int
	End   int
}

type PatternMatch struct {
	Identifier string
	Range      PatternMatchRange
}

func NewPatternMatch(identifier string, start int, end int) *PatternMatch {
	return &PatternMatch{
		Identifier: identifier,
		Range: PatternMatchRange{
			Start: start,
			End:   end,
		},
	}
}

type PatternMismatch struct {
	PatternMatch
	PartialMatches []*PatternMatch
}

func NewPatternMismatch(identifier string, start int, end int, partialMatches []*PatternMatch) *PatternMismatch {
	return &PatternMismatch{
		PatternMatch: PatternMatch{
			Identifier: identifier,
			Range: PatternMatchRange{
				Start: start,
				End:   end,
			},
		},
		PartialMatches: partialMatches,
	}
}

type PatternMismatchLog struct {
	Mismatches []*PatternMismatch
}

func NewPatternMismatchLog() *PatternMismatchLog {
	return &PatternMismatchLog{
		Mismatches: []*PatternMismatch{},
	}
}

func (l *PatternMismatchLog) Log(err *PatternMismatch) {
	l.Mismatches = append(l.Mismatches, err)
}

type PatternMatcher interface {
	Match(EntityStreamer, *PatternMismatchLog) (bool, *PatternMatch)
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

func (m *SingleEntityMatch) Match(s EntityStreamer, l *PatternMismatchLog) (bool, *PatternMatch) {
	pos := s.Position()

	if m.matchFunction(s.Read()) {
		return true, NewPatternMatch(m.identifier, pos, s.Position())
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

func (m *ConcatenationMatch) Match(s EntityStreamer, l *PatternMismatchLog) (bool, *PatternMatch) {
	pos := s.Position()

	partialMatches := []*PatternMatch{}

	for _, pm := range m.patterns {

		match, result := pm.Match(s, l)
		if match {
			partialMatches = append(partialMatches, result)
		} else {
			l.Log(NewPatternMismatch(m.identifier, pos, s.Position(), partialMatches))

			return false, nil
		}
	}

	return true, NewPatternMatch(m.identifier, pos, s.Position())
}
