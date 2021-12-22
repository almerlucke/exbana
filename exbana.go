package exbana

import (
	"fmt"
)

// Entity returned from streamer, real implementations could have rune or char as entity types
type Entity interface{}

// EntityEqualFunction test if two entities are equal
type EntityEqualFunction func(Entity, Entity) bool

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

// TransformFunction transforms match result to final value
type TransformFunction func(*MatchResult, TransformTable) Value

// TransformTable is used to map matcher identifiers to a transform function
type TransformTable map[string]TransformFunction

// Transform a match result to a value
func (t TransformTable) Transform(m *MatchResult) Value {
	f, ok := t[m.Identifier]
	if ok {
		return f(m, t)
	}

	return m.Value
}

// Mismatch can hold information about a pattern mismatch and possibly the sub pattern that caused the mismatch
// and the sub patterns that matched so far, an optional error can be passed to give more specific information
type Mismatch struct {
	MatchResult
	SubMismatch *MatchResult
	SubMatches  []*MatchResult
	Error       error
}

// NewMismatch creates a new pattern mismatch
func NewMismatch(identifier string, begin Position, end Position, subMisMatch *MatchResult, subMatches []*MatchResult, err error) *Mismatch {
	return &Mismatch{
		MatchResult: MatchResult{
			Identifier: identifier,
			Begin:      begin,
			End:        end,
			Value:      nil,
		},
		SubMismatch: subMisMatch,
		SubMatches:  subMatches,
		Error:       err,
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
		l.Log(NewMismatch(m.identifier, pos, s.Position(), nil, nil, nil))
	}

	return false, nil, nil
}

type EntitySeriesMatch struct {
	identifier    string
	logMismatches bool
	equalFunction EntityEqualFunction
	series        []Entity
}

func NewEntitySeriesMatch(identifier string, logMismatches bool, series []Entity, equalFunction EntityEqualFunction) *EntitySeriesMatch {
	return &EntitySeriesMatch{
		identifier:    identifier,
		logMismatches: logMismatches,
		series:        series,
		equalFunction: equalFunction,
	}
}

func (m *EntitySeriesMatch) LogMismatches() bool {
	return m.logMismatches
}

func (m *EntitySeriesMatch) Identifier() string {
	return m.identifier
}

func (m *EntitySeriesMatch) Match(s EntityStreamer, l Logger) (bool, *MatchResult, error) {
	beginPos := s.Position()

	for _, e1 := range m.series {
		e2, err := s.Read()
		if err != nil {
			return false, nil, err
		}

		if !m.equalFunction(e1, e2) {
			if m.logMismatches && l != nil {
				l.Log(NewMismatch(m.identifier, beginPos, s.Position(), nil, nil, nil))
			}

			return false, nil, nil
		}
	}

	endPos := s.Position()

	return true, NewMatchResult(m.identifier, beginPos, endPos, s.ValueForRange(beginPos, endPos)), nil
}

// ConcatenationMatch matches a slice of patterns AND style
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

			if m.logMismatches && l != nil {
				l.Log(NewMismatch(
					m.identifier, beginPos, subEndPos, NewMatchResult(pm.Identifier(), subBeginPos, subEndPos, nil), matches, nil),
				)
			}

			return false, nil, nil
		}
	}

	return true, NewMatchResult(m.identifier, beginPos, s.Position(), matches), nil
}

// AlternationMatch matches a slice of patterns OR style
type AlternationMatch struct {
	identifier    string
	logMismatches bool
	patterns      []Matcher
}

func NewAlternationMatch(identifier string, logMismatches bool, patterns []Matcher) *AlternationMatch {
	return &AlternationMatch{
		identifier:    identifier,
		logMismatches: logMismatches,
		patterns:      patterns,
	}
}

func (m *AlternationMatch) LogMismatches() bool {
	return m.logMismatches
}

func (m *AlternationMatch) Identifier() string {
	return m.identifier
}

func (m *AlternationMatch) Match(s EntityStreamer, l Logger) (bool, *MatchResult, error) {
	beginPos := s.Position()

	for _, pm := range m.patterns {
		s.SetPosition(beginPos)

		matched, result, err := pm.Match(s, l)
		if err != nil {
			return false, nil, err
		}

		if matched {
			return true, NewMatchResult(m.identifier, beginPos, s.Position(), result), nil
		}
	}

	if m.logMismatches && l != nil {
		l.Log(NewMismatch(m.identifier, beginPos, s.Position(), nil, nil, nil))
	}

	return false, nil, nil
}

type RepetitionMatch struct {
	identifier    string
	logMismatches bool
	pattern       Matcher
	min           int
	max           int
}

func NewRepetitionMatch(identifier string, logMismatches bool, pattern Matcher, min int, max int) *RepetitionMatch {
	return &RepetitionMatch{
		identifier:    identifier,
		logMismatches: logMismatches,
		pattern:       pattern,
		min:           min,
		max:           max,
	}
}

func (m *RepetitionMatch) LogMismatches() bool {
	return m.logMismatches
}

func (m *RepetitionMatch) Identifier() string {
	return m.identifier
}

func (m *RepetitionMatch) Match(s EntityStreamer, l Logger) (bool, *MatchResult, error) {
	beginPos := s.Position()
	matches := []*MatchResult{}

	for {
		if s.Finished() {
			break
		}

		resetPos := s.Position()

		matched, result, err := m.pattern.Match(s, l)
		if err != nil {
			return false, nil, err
		}

		if !matched {
			s.SetPosition(resetPos)
			break
		}

		matches = append(matches, result)
		if m.max != 0 && len(matches) == m.max {
			break
		}
	}

	if len(matches) < m.min {
		if m.logMismatches && l != nil {
			l.Log(NewMismatch(m.identifier, beginPos, s.Position(), nil, nil, fmt.Errorf("expected minimum of %d repetitions", m.min)))
		}

		return false, nil, nil
	}

	return true, NewMatchResult(m.identifier, beginPos, s.Position(), matches), nil
}
