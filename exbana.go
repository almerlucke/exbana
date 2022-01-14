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
	Val   Value
}

// NewResult creates a new pattern match result
func NewResult(id string, begin Position, end Position, val Value) *Result {
	return &Result{
		ID:    id,
		Begin: begin,
		End:   end,
		Val:   val,
	}
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

	return m.Val
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
			Val:   nil,
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

// Unit represents a single object pattern
type Unit struct {
	id        string
	logging   bool
	matchFunc UnitMatchFunc
}

// NewUnitF creates a new unit pattern
func NewUnitF(id string, logging bool, matchFunc UnitMatchFunc) *Unit {
	return &Unit{
		id:        id,
		logging:   logging,
		matchFunc: matchFunc,
	}
}

// NewUnit creates a new unit pattern
func NewUnit(matchFunction UnitMatchFunc) *Unit {
	return NewUnitF("", false, matchFunction)
}

// ID returns the unit pattern ID
func (p *Unit) ID() string {
	return p.id
}

// Match matches the unit object against a stream
func (p *Unit) Match(s ObjStreamer, l Logger) (bool, *Result, error) {
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

// Series represents a series of objects to match
type Series struct {
	id      string
	logging bool
	eqFunc  ObjectEqualFunc
	series  []Object
}

// NewSeriesF creates a new series pattern
func NewSeriesF(id string, logging bool, eqFunc ObjectEqualFunc, series ...Object) *Series {
	return &Series{
		id:      id,
		logging: logging,
		series:  series,
		eqFunc:  eqFunc,
	}
}

// NewSeries creates a new series pattern
func NewSeries(eqFunc ObjectEqualFunc, series ...Object) *Series {
	return NewSeriesF("", false, eqFunc, series...)
}

// ID return the series pattern ID
func (p *Series) ID() string {
	return p.id
}

// Match matches the series pattern against a stream
func (p *Series) Match(s ObjStreamer, l Logger) (bool, *Result, error) {
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

// And matches a series of patterns AND style
type And struct {
	id       string
	logging  bool
	Patterns Patterns
}

// NewAndF creates a new AND pattern
func NewAndF(id string, logging bool, patterns ...Pattern) *And {
	return &And{
		id:       id,
		logging:  logging,
		Patterns: patterns,
	}
}

// NewAnd creates a new AND pattern
func NewAnd(patterns ...Pattern) *And {
	return NewAndF("", false, patterns...)
}

// ID returns the AND pattern ID
func (p *And) ID() string {
	return p.id
}

// Match matches And against a stream, fails if any of the patterns mismatches
func (p *And) Match(s ObjStreamer, l Logger) (bool, *Result, error) {
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

// Or matches a series of patterns OR style
type Or struct {
	id       string
	logging  bool
	Patterns Patterns
}

// NewOrF creates a new OR pattern
func NewOrF(id string, logging bool, patterns ...Pattern) *Or {
	return &Or{
		id:       id,
		logging:  logging,
		Patterns: patterns,
	}
}

// NewOr creates a new OR pattern
func NewOr(patterns ...Pattern) *Or {
	return NewOrF("", false, patterns...)
}

// ID returns the ID of the OR pattern
func (p *Or) ID() string {
	return p.id
}

// Match matches the OR pattern against a stream, fails if all of the patterns mismatch
func (p *Or) Match(s ObjStreamer, l Logger) (bool, *Result, error) {
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

// Rep matches a pattern repetition
type Rep struct {
	id      string
	logging bool
	Pattern Pattern
	min     int
	max     int
}

// NewRepF creates a new repetition pattern
func NewRepF(id string, logging bool, pattern Pattern, min int, max int) *Rep {
	return &Rep{
		id:      id,
		logging: logging,
		Pattern: pattern,
		min:     min,
		max:     max,
	}
}

// NewRep creates a new repetition pattern
func NewRep(pattern Pattern, min int, max int) *Rep {
	return NewRepF("", false, pattern, min, max)
}

// NewOptF creates a new optional pattern
func NewOptF(id string, logging bool, pattern Pattern) *Rep {
	return NewRepF(id, logging, pattern, 0, 1)
}

// NewOpt creates a new optional pattern
func NewOpt(pattern Pattern) *Rep {
	return NewOptF("", false, pattern)
}

// NewAnyF creates a new any repetition pattern
func NewAnyF(id string, logging bool, pattern Pattern) *Rep {
	return NewRepF(id, logging, pattern, 0, 0)
}

// NewAny creates a new any repetition pattern
func NewAny(pattern Pattern) *Rep {
	return NewAnyF("", false, pattern)
}

// NewNF creates a new repetition pattern for exactly n times
func NewNF(id string, logging bool, pattern Pattern, n int) *Rep {
	return NewRepF(id, logging, pattern, n, n)
}

// NewN creates a new repetition pattern for exactly n times
func NewN(pattern Pattern, n int) *Rep {
	return NewNF("", false, pattern, n)
}

// ID returns the ID of the repetition pattern
func (p *Rep) ID() string {
	return p.id
}

// Match matches the repetition pattern aginst a stream
func (p *Rep) Match(s ObjStreamer, l Logger) (bool, *Result, error) {
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

// Except pattern must not match the Except pattern but must match the MustMatch pattern
type Except struct {
	id        string
	logging   bool
	MustMatch Pattern
	Except    Pattern
}

// NewExceptF creates a new except pattern
func NewExceptF(id string, logging bool, mustMatch Pattern, except Pattern) *Except {
	return &Except{
		id:        id,
		logging:   logging,
		MustMatch: mustMatch,
		Except:    except,
	}
}

// NewExcept creates a new except pattern
func NewExcept(mustMatch Pattern, except Pattern) *Except {
	return NewExceptF("", false, mustMatch, except)
}

// ID returns the except pattern ID
func (p *Except) ID() string {
	return p.id
}

// Match matches the exception against a stream
func (p *Except) Match(s ObjStreamer, l Logger) (bool, *Result, error) {
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

// End pattern matches the end of stream
type End struct {
	id      string
	logging bool
}

// NewEndF creates a new end of stream pattern
func NewEndF(id string, logging bool) *End {
	return &End{
		id:      id,
		logging: logging,
	}
}

// NewEnd creates a new end of stream pattern
func NewEnd() *End {
	return NewEndF("", false)
}

// ID returns end of stream pattern ID
func (p *End) ID() string {
	return p.id
}

// Match matches a end of stream pattern against a stream
func (p *End) Match(s ObjStreamer, l Logger) (bool, *Result, error) {
	if s.Finished() {
		return true, NewResult(p.id, s.Position(), s.Position(), nil), nil
	}

	if p.logging && l != nil {
		l.Log(NewMismatch(p.id, s.Position(), s.Position(), nil, nil, nil))
	}

	return false, nil, nil
}
