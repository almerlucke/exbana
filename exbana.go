package exbana

import (
	"fmt"
)

// Object returned from streamer, a real implementation could have rune as object type
type Object interface{}

// ObjectEqualFunc test if two objects are equal
type ObjectEqualFunc func(Object, Object) bool

// Position is an abstract position from a stream implementation
type Position interface{}

// Value is an abstract return value from result
type Value interface{}

// ObjStreamer interface for a stream that can emit objects to a pattern matcher
type ObjStreamer interface {
	Peek() (Object, error)
	Read() (Object, error)
	Finished() bool
	Position() Position
	SetPosition(Position) error
	ValueForRange(Position, Position) Value
}

// Result contains matched pattern position, identifier and value
type Result struct {
	ID    string
	Begin Position
	End   Position
	Value Value
}

// NewResult creates a new pattern match result
func NewResult(id string, begin Position, end Position, value Value) *Result {
	return &Result{
		ID:    id,
		Begin: begin,
		End:   end,
		Value: value,
	}
}

func (r *Result) Components() []*Result {
	return r.Value.([]*Result)
}

func (r *Result) Values() []Value {
	components := r.Components()
	values := make([]Value, len(components))
	for index, component := range components {
		values[index] = component.Value
	}
	return values
}

func (r *Result) NestedValue() Value {
	return r.Value.(*Result).Value
}

// TransformFunc can transform match result to final value
type TransformFunc func(*Result, TransformTable) Value

// TransformTable is used to map matcher identifiers to a transform function
type TransformTable map[string]TransformFunc

// Transform a match result to a value
func (t TransformTable) Transform(m *Result) Value {
	f, ok := t[m.ID]
	if ok {
		return f(m, t)
	}

	return m.Value
}

// Mismatch can hold information about a pattern mismatch and possibly the sub pattern that caused the mismatch
// and the sub patterns that matched so far, an optional error can be passed to give more specific information
type Mismatch struct {
	Result
	SubMismatch *Result
	SubMatches  []*Result
	Error       error
}

// NewMismatch creates a new pattern mismatch
func NewMismatch(id string, begin Position, end Position, subMisMatch *Result, subMatches []*Result, err error) *Mismatch {
	return &Mismatch{
		Result: Result{
			ID:    id,
			Begin: begin,
			End:   end,
			Value: nil,
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

// Pattern can match objects from a stream, has an identifier
type Pattern interface {
	Match(ObjStreamer, Logger) (bool, *Result, error)
	ID() string
}

// Patterns is a convenience type for a slice of pattern interfaces
type Patterns []Pattern

// UnitFunc can match a single object
type UnitMatchFunc func(Object) bool

// UnitPattern represents a single object pattern
type UnitPattern struct {
	id        string
	logging   bool
	matchFunc UnitMatchFunc
}

// Unitx creates a new unit pattern with identifier and logging
func Unitx(id string, logging bool, matchFunc UnitMatchFunc) *UnitPattern {
	return &UnitPattern{
		id:        id,
		logging:   logging,
		matchFunc: matchFunc,
	}
}

// Unit creates a new unit pattern
func Unit(matchFunction UnitMatchFunc) *UnitPattern {
	return Unitx("", false, matchFunction)
}

// ID returns the unit pattern ID
func (p *UnitPattern) ID() string {
	return p.id
}

// Match matches the unit object against a stream
func (p *UnitPattern) Match(s ObjStreamer, l Logger) (bool, *Result, error) {
	pos := s.Position()
	entity, err := s.Read()

	if err != nil {
		return false, nil, err
	}

	if p.matchFunc(entity) {
		return true, NewResult(p.id, pos, s.Position(), s.ValueForRange(pos, s.Position())), nil
	} else if p.logging && l != nil {
		l.Log(NewMismatch(p.id, pos, s.Position(), nil, nil, nil))
	}

	return false, nil, nil
}

// SeriesPattern represents a series of objects to match
type SeriesPattern struct {
	id      string
	logging bool
	eqFunc  ObjectEqualFunc
	series  []Object
}

// Seriesx creates a new series pattern with identifier and logging
func Seriesx(id string, logging bool, eqFunc ObjectEqualFunc, series ...Object) *SeriesPattern {
	return &SeriesPattern{
		id:      id,
		logging: logging,
		series:  series,
		eqFunc:  eqFunc,
	}
}

// Series creates a new series pattern
func Series(eqFunc ObjectEqualFunc, series ...Object) *SeriesPattern {
	return Seriesx("", false, eqFunc, series...)
}

// ID return the series pattern ID
func (p *SeriesPattern) ID() string {
	return p.id
}

// Match matches the series pattern against a stream
func (p *SeriesPattern) Match(s ObjStreamer, l Logger) (bool, *Result, error) {
	beginPos := s.Position()

	for _, e1 := range p.series {
		e2, err := s.Read()
		if err != nil {
			return false, nil, err
		}

		if !p.eqFunc(e1, e2) {
			if p.logging && l != nil {
				l.Log(NewMismatch(p.id, beginPos, s.Position(), nil, nil, nil))
			}

			return false, nil, nil
		}
	}

	endPos := s.Position()

	return true, NewResult(p.id, beginPos, endPos, s.ValueForRange(beginPos, endPos)), nil
}

// Concat matches a series of patterns AND style in order (concatenation)
type ConcatPattern struct {
	id       string
	logging  bool
	Patterns Patterns
}

// Concatx creates a new concat pattern with identifier and logging
func Concatx(id string, logging bool, patterns ...Pattern) *ConcatPattern {
	return &ConcatPattern{
		id:       id,
		logging:  logging,
		Patterns: patterns,
	}
}

// Concat creates a new AND pattern
func Concat(patterns ...Pattern) *ConcatPattern {
	return Concatx("", false, patterns...)
}

// ID returns the AND pattern ID
func (p *ConcatPattern) ID() string {
	return p.id
}

// Match matches And against a stream, fails if any of the patterns mismatches
func (p *ConcatPattern) Match(s ObjStreamer, l Logger) (bool, *Result, error) {
	beginPos := s.Position()

	matches := []*Result{}

	for _, pm := range p.Patterns {
		subBeginPos := s.Position()

		matched, result, err := pm.Match(s, l)
		if err != nil {
			return false, nil, err
		}

		if matched {
			matches = append(matches, result)
		} else {
			subEndPos := s.Position()

			if p.logging && l != nil {
				l.Log(NewMismatch(
					p.id, beginPos, subEndPos, NewResult(pm.ID(), subBeginPos, subEndPos, nil), matches, nil),
				)
			}

			return false, nil, nil
		}
	}

	return true, NewResult(p.id, beginPos, s.Position(), matches), nil
}

// AltPattern matches a series of patterns OR style in order (alternation)
type AltPattern struct {
	id       string
	logging  bool
	Patterns Patterns
}

// Altx creates a new Alt pattern with identifier and logging
func Altx(id string, logging bool, patterns ...Pattern) *AltPattern {
	return &AltPattern{
		id:       id,
		logging:  logging,
		Patterns: patterns,
	}
}

// Alt creates a new OR pattern
func Alt(patterns ...Pattern) *AltPattern {
	return Altx("", false, patterns...)
}

// ID returns the ID of the OR pattern
func (p *AltPattern) ID() string {
	return p.id
}

// Match matches the OR pattern against a stream, fails if all of the patterns mismatch
func (p *AltPattern) Match(s ObjStreamer, l Logger) (bool, *Result, error) {
	beginPos := s.Position()

	for _, pm := range p.Patterns {
		s.SetPosition(beginPos)

		matched, result, err := pm.Match(s, l)
		if err != nil {
			return false, nil, err
		}

		if matched {
			return true, NewResult(p.id, beginPos, s.Position(), result), nil
		}
	}

	if p.logging && l != nil {
		l.Log(NewMismatch(p.id, beginPos, s.Position(), nil, nil, nil))
	}

	return false, nil, nil
}

// RepPattern matches a pattern repetition
type RepPattern struct {
	id      string
	logging bool
	Pattern Pattern
	min     int
	max     int
}

// Repx creates a new repetition pattern
func Repx(id string, logging bool, pattern Pattern, min int, max int) *RepPattern {
	return &RepPattern{
		id:      id,
		logging: logging,
		Pattern: pattern,
		min:     min,
		max:     max,
	}
}

// Rep creates a new repetition pattern
func Rep(pattern Pattern, min int, max int) *RepPattern {
	return Repx("", false, pattern, min, max)
}

// Optx creates a new optional pattern
func Optx(id string, logging bool, pattern Pattern) *RepPattern {
	return Repx(id, logging, pattern, 0, 1)
}

// Opt creates a new optional pattern
func Opt(pattern Pattern) *RepPattern {
	return Optx("", false, pattern)
}

// Anyx creates a new any repetition pattern
func Anyx(id string, logging bool, pattern Pattern) *RepPattern {
	return Repx(id, logging, pattern, 0, 0)
}

// Any creates a new any repetition pattern
func Any(pattern Pattern) *RepPattern {
	return Anyx("", false, pattern)
}

// Nx creates a new repetition pattern for exactly n times
func Nx(id string, logging bool, pattern Pattern, n int) *RepPattern {
	return Repx(id, logging, pattern, n, n)
}

// N creates a new repetition pattern for exactly n times
func N(pattern Pattern, n int) *RepPattern {
	return Nx("", false, pattern, n)
}

// ID returns the ID of the repetition pattern
func (p *RepPattern) ID() string {
	return p.id
}

// Match matches the repetition pattern aginst a stream
func (p *RepPattern) Match(s ObjStreamer, l Logger) (bool, *Result, error) {
	beginPos := s.Position()
	matches := []*Result{}

	for {
		if s.Finished() {
			break
		}

		resetPos := s.Position()

		matched, result, err := p.Pattern.Match(s, l)
		if err != nil {
			return false, nil, err
		}

		if !matched {
			s.SetPosition(resetPos)
			break
		}

		matches = append(matches, result)
		if p.max != 0 && len(matches) == p.max {
			break
		}
	}

	if len(matches) < p.min {
		if p.logging && l != nil {
			l.Log(NewMismatch(p.id, beginPos, s.Position(), nil, nil, fmt.Errorf("expected minimum of %d repetitions", p.min)))
		}

		return false, nil, nil
	}

	return true, NewResult(p.id, beginPos, s.Position(), matches), nil
}

// ExceptPattern must not match the Except pattern but must match the MustMatch pattern
type ExceptPattern struct {
	id        string
	logging   bool
	MustMatch Pattern
	Except    Pattern
}

// Exceptx creates a new except pattern
func Exceptx(id string, logging bool, mustMatch Pattern, except Pattern) *ExceptPattern {
	return &ExceptPattern{
		id:        id,
		logging:   logging,
		MustMatch: mustMatch,
		Except:    except,
	}
}

// Except creates a new except pattern
func Except(mustMatch Pattern, except Pattern) *ExceptPattern {
	return Exceptx("", false, mustMatch, except)
}

// ID returns the except pattern ID
func (p *ExceptPattern) ID() string {
	return p.id
}

// Match matches the exception against a stream
func (p *ExceptPattern) Match(s ObjStreamer, l Logger) (bool, *Result, error) {
	beginPos := s.Position()

	// First check for the exception match, we do not want to match the exception
	matched, result, err := p.Except.Match(s, l)
	if err != nil {
		return false, nil, err
	}

	if matched {
		if p.logging && l != nil {
			l.Log(NewMismatch(p.id, beginPos, s.Position(), result, nil, nil))
		}

		return false, nil, nil
	}

	// Reset the position and return the mustMatch result
	s.SetPosition(beginPos)

	return p.MustMatch.Match(s, l)
}

// EndPattern matches the end of stream
type EndPattern struct {
	id      string
	logging bool
}

// EndF creates a new end of stream pattern
func Endx(id string, logging bool) *EndPattern {
	return &EndPattern{
		id:      id,
		logging: logging,
	}
}

// End creates a new end of stream pattern
func End() *EndPattern {
	return Endx("", false)
}

// ID returns end of stream pattern ID
func (p *EndPattern) ID() string {
	return p.id
}

// Match matches a end of stream pattern against a stream
func (p *EndPattern) Match(s ObjStreamer, l Logger) (bool, *Result, error) {
	if s.Finished() {
		return true, NewResult(p.id, s.Position(), s.Position(), nil), nil
	}

	if p.logging && l != nil {
		l.Log(NewMismatch(p.id, s.Position(), s.Position(), nil, nil, nil))
	}

	return false, nil, nil
}
