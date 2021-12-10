package exbana

// Entity returned from streamer, real implementations could have rune or char as entity types
type Entity interface{}

// Position real type is left to the entity stream
type Position interface{}

// Value real type is left to the entity stream
type Value interface{}

// EntityStreamer interface for a stream that can emit entities to pattern matcher
type EntityStreamer interface {
	Peek() (Entity, error)
	Read() (Entity, error)
	Finished() bool
	Position() Position
	SetPosition(Position) error
	ValueForRange(Position, Position) Value
}

// MatchResult contains matched pattern position and identifier
type MatchResult struct {
	Identifier string
	Begin      Position
	End        Position
	Value      Value
}

// NewMatchResult creates a new match result
func NewMatchResult(identifier string, begin Position, end Position, value Value) *MatchResult {
	return &MatchResult{
		Identifier: identifier,
		Begin:      begin,
		End:        end,
		Value:      value,
	}
}

// Mismatch can hold information about a pattern mismatch and possibly the sub pattern that caused the mismatch
// and the sub patterns that matched so far, this can be used to debug and backtrace pattern mismatches
type Mismatch struct {
	MatchResult
	SubMismatch *MatchResult
	SubMatches  []*MatchResult
}

// NewMismatch creates a new pattern mismatch
func NewMismatch(identifier string, begin Position, end Position, subMisMatch *MatchResult, subMatches []*MatchResult) *Mismatch {
	return &Mismatch{
		MatchResult: MatchResult{
			Identifier: identifier,
			Begin:      begin,
			End:        end,
			Value:      nil,
		},
		SubMismatch: subMisMatch,
		SubMatches:  subMatches,
	}
}

// Logger can be used to log pattern mismatches during pattern matching
type Logger interface {
	Log(mismatch *Mismatch)
}

// Matcher can match a pattern from a stream, has an identifier and indicates if we need to log
// mismatches
type Matcher interface {
	Match(EntityStreamer, Logger) (bool, *MatchResult, error)
	Identifier() string
	LogMismatches() bool
}

// EntityMatchFunction can match a single entity against a pattern
type EntityMatchFunction func(Entity) bool

// EntityMatch
type EntityMatch struct {
	identifier    string
	logMismatches bool
	matchFunction EntityMatchFunction
}

// NewEntityMatch creates a new entity match
func NewEntityMatch(identifier string, logMismatches bool, matchFunction EntityMatchFunction) *EntityMatch {
	return &EntityMatch{
		identifier:    identifier,
		logMismatches: logMismatches,
		matchFunction: matchFunction,
	}
}

// Identifier of this match
func (m *EntityMatch) Identifier() string {
	return m.identifier
}

// LogMismatches indicates if this match needs to log mismatches
func (m *EntityMatch) LogMismatches() bool {
	return m.logMismatches
}

// Match entity
func (m *EntityMatch) Match(s EntityStreamer, l Logger) (bool, *MatchResult, error) {
	pos := s.Position()
	entity, err := s.Read()

	if err != nil {
		return false, nil, err
	}

	if m.matchFunction(entity) {
		return true, NewMatchResult(m.identifier, pos, s.Position(), s.ValueForRange(pos, s.Position())), nil
	} else if m.LogMismatches() && l != nil {
		l.Log(NewMismatch(m.identifier, pos, s.Position(), nil, nil))
	}

	return false, nil, nil
}

// ConcatenationMatch matches a slice of patterns
type ConcatenationMatch struct {
	identifier    string
	logMismatches bool
	patterns      []Matcher
}

// NewConcatenationMatch creates a new concatenation match
func NewConcatenationMatch(identifier string, logMismatches bool, patterns []Matcher) *ConcatenationMatch {
	return &ConcatenationMatch{
		identifier:    identifier,
		logMismatches: logMismatches,
		patterns:      patterns,
	}
}

func (m *ConcatenationMatch) LogMismatches() bool {
	return m.logMismatches
}

func (m *ConcatenationMatch) Identifier() string {
	return m.identifier
}

func (m *ConcatenationMatch) Match(s EntityStreamer, l Logger) (bool, *MatchResult, error) {
	logMismatches := m.logMismatches && l != nil
	beginPos := s.Position()

	matches := []*MatchResult{}

	for _, pm := range m.patterns {
		subBeginPos := s.Position()

		matched, result, err := pm.Match(s, l)
		if err != nil {
			return false, nil, err
		}

		if matched {
			matches = append(matches, result)
		} else {
			subEndPos := s.Position()

			if logMismatches {
				l.Log(NewMismatch(
					m.identifier, beginPos, subEndPos, NewMatchResult(pm.Identifier(), subBeginPos, subEndPos, nil), matches),
				)
			}

			return false, nil, nil
		}
	}

	return true, NewMatchResult(m.identifier, beginPos, s.Position(), matches), nil
}
