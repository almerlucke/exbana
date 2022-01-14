package exbana

import (
	"fmt"
)

// Obj returned from streamer, real implementations could have rune or char as entity types
type Obj interface{}

// ObjEqFunc test if two entities are equal
type ObjEqFunc func(Obj, Obj) bool

// Pos real type is left to the entity stream
type Pos interface{}

// Val real type is left to the entity stream
type Val interface{}

// ObjStreamer interface for a stream that can emit objects to pattern matcher
type ObjStreamer interface {
	Peek() (Obj, error)
	Read() (Obj, error)
	Finished() bool
	Pos() Pos
	SetPos(Pos) error
	ValForRange(Pos, Pos) Val
}

// Result contains matched pattern position and identifier
type Result struct {
	ID    string
	Begin Pos
	End   Pos
	Val   Val
}

// NewResult creates a new match result
func NewResult(id string, begin Pos, end Pos, val Val) *Result {
	return &Result{
		ID:    id,
		Begin: begin,
		End:   end,
		Val:   val,
	}
}

// TransFunc transforms match result to final value
type TransFunc func(*Result, TransTable) Val

// TransTable is used to map matcher identifiers to a transform function
type TransTable map[string]TransFunc

// Transform a match result to a value
func (t TransTable) Transform(m *Result) Val {
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
func NewMismatch(id string, begin Pos, end Pos, subMisMatch *Result, subMatches []*Result, err error) *Mismatch {
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

// Matcher can match a pattern from a stream, has an identifier and indicates if we need to log
// mismatches
type Matcher interface {
	Match(ObjStreamer, Logger) (bool, *Result, error)
	ID() string
}

// SingleFunc can match a single entity against a pattern
type SingleFunc func(Obj) bool

// Single object matcher
type Single struct {
	id        string
	logging   bool
	matchFunc SingleFunc
}

// NewvF creates a new obj match
func NewSingleF(id string, logging bool, matchFunc SingleFunc) *Single {
	return &Single{
		id:        id,
		logging:   logging,
		matchFunc: matchFunc,
	}
}

func NewSingle(matchFunction SingleFunc) *Single {
	return NewSingleF("", false, matchFunction)
}

// Identifier of this match
func (m *Single) ID() string {
	return m.id
}

// Match entity
func (m *Single) Match(s ObjStreamer, l Logger) (bool, *Result, error) {
	pos := s.Pos()
	entity, err := s.Read()

	if err != nil {
		return false, nil, err
	}

	if m.matchFunc(entity) {
		return true, NewResult(m.id, pos, s.Pos(), s.ValForRange(pos, s.Pos())), nil
	} else if m.logging && l != nil {
		l.Log(NewMismatch(m.id, pos, s.Pos(), nil, nil, nil))
	}

	return false, nil, nil
}

type Series struct {
	id      string
	logging bool
	eqFunc  ObjEqFunc
	series  []Obj
}

func NewSeriesF(id string, logging bool, series []Obj, eqFunc ObjEqFunc) *Series {
	return &Series{
		id:      id,
		logging: logging,
		series:  series,
		eqFunc:  eqFunc,
	}
}

func NewSeries(series []Obj, eqFunc ObjEqFunc) *Series {
	return NewSeriesF("", false, series, eqFunc)
}

func (m *Series) ID() string {
	return m.id
}

func (m *Series) Match(s ObjStreamer, l Logger) (bool, *Result, error) {
	beginPos := s.Pos()

	for _, e1 := range m.series {
		e2, err := s.Read()
		if err != nil {
			return false, nil, err
		}

		if !m.eqFunc(e1, e2) {
			if m.logging && l != nil {
				l.Log(NewMismatch(m.id, beginPos, s.Pos(), nil, nil, nil))
			}

			return false, nil, nil
		}
	}

	endPos := s.Pos()

	return true, NewResult(m.id, beginPos, endPos, s.ValForRange(beginPos, endPos)), nil
}

// And matches a slice of patterns AND style
type And struct {
	id       string
	logging  bool
	Patterns []Matcher
}

// NewAndF creates a new concatenation match
func NewAndF(id string, logging bool, patterns []Matcher) *And {
	return &And{
		id:       id,
		logging:  logging,
		Patterns: patterns,
	}
}

func NewAnd(patterns []Matcher) *And {
	return NewAndF("", false, patterns)
}

func (m *And) ID() string {
	return m.id
}

func (m *And) Match(s ObjStreamer, l Logger) (bool, *Result, error) {
	beginPos := s.Pos()

	matches := []*Result{}

	for _, pm := range m.Patterns {
		subBeginPos := s.Pos()

		matched, result, err := pm.Match(s, l)
		if err != nil {
			return false, nil, err
		}

		if matched {
			matches = append(matches, result)
		} else {
			subEndPos := s.Pos()

			if m.logging && l != nil {
				l.Log(NewMismatch(
					m.id, beginPos, subEndPos, NewResult(pm.ID(), subBeginPos, subEndPos, nil), matches, nil),
				)
			}

			return false, nil, nil
		}
	}

	return true, NewResult(m.id, beginPos, s.Pos(), matches), nil
}

// Or matches a slice of patterns OR style
type Or struct {
	id       string
	logging  bool
	Patterns []Matcher
}

func NewOrF(id string, logging bool, patterns []Matcher) *Or {
	return &Or{
		id:       id,
		logging:  logging,
		Patterns: patterns,
	}
}

func NewOr(patterns []Matcher) *Or {
	return NewOrF("", false, patterns)
}

func (m *Or) ID() string {
	return m.id
}

func (m *Or) Match(s ObjStreamer, l Logger) (bool, *Result, error) {
	beginPos := s.Pos()

	for _, pm := range m.Patterns {
		s.SetPos(beginPos)

		matched, result, err := pm.Match(s, l)
		if err != nil {
			return false, nil, err
		}

		if matched {
			return true, NewResult(m.id, beginPos, s.Pos(), result), nil
		}
	}

	if m.logging && l != nil {
		l.Log(NewMismatch(m.id, beginPos, s.Pos(), nil, nil, nil))
	}

	return false, nil, nil
}

// Rep matches a pattern min and max times repetition
type Rep struct {
	id      string
	logging bool
	Pattern Matcher
	min     int
	max     int
}

func NewRepF(id string, logging bool, pattern Matcher, min int, max int) *Rep {
	return &Rep{
		id:      id,
		logging: logging,
		Pattern: pattern,
		min:     min,
		max:     max,
	}
}

func NewRep(pattern Matcher, min int, max int) *Rep {
	return NewRepF("", false, pattern, min, max)
}

func NewOpt(pattern Matcher) *Rep {
	return NewRep(pattern, 0, 1)
}

func NewAny(pattern Matcher) *Rep {
	return NewRep(pattern, 0, 0)
}

func NewN(pattern Matcher, n int) *Rep {
	return NewRep(pattern, n, n)
}

func (m *Rep) ID() string {
	return m.id
}

func (m *Rep) Match(s ObjStreamer, l Logger) (bool, *Result, error) {
	beginPos := s.Pos()
	matches := []*Result{}

	for {
		if s.Finished() {
			break
		}

		resetPos := s.Pos()

		matched, result, err := m.Pattern.Match(s, l)
		if err != nil {
			return false, nil, err
		}

		if !matched {
			s.SetPos(resetPos)
			break
		}

		matches = append(matches, result)
		if m.max != 0 && len(matches) == m.max {
			break
		}
	}

	if len(matches) < m.min {
		if m.logging && l != nil {
			l.Log(NewMismatch(m.id, beginPos, s.Pos(), nil, nil, fmt.Errorf("expected minimum of %d repetitions", m.min)))
		}

		return false, nil, nil
	}

	return true, NewResult(m.id, beginPos, s.Pos(), matches), nil
}

// Except must match MustMatch but first must not match Except
type Except struct {
	id        string
	logging   bool
	MustMatch Matcher
	Except    Matcher
}

func NewExceptF(id string, logging bool, mustMatch Matcher, except Matcher) *Except {
	return &Except{
		id:        id,
		logging:   logging,
		MustMatch: mustMatch,
		Except:    except,
	}
}

func NewExcept(mustMatch Matcher, except Matcher) *Except {
	return NewExceptF("", false, mustMatch, except)
}

func (m *Except) ID() string {
	return m.id
}

func (m *Except) Match(s ObjStreamer, l Logger) (bool, *Result, error) {
	beginPos := s.Pos()

	// First check for the exception match, we do not want to match the exception
	matched, result, err := m.Except.Match(s, l)
	if err != nil {
		return false, nil, err
	}

	if matched {
		if m.logging && l != nil {
			l.Log(NewMismatch(m.id, beginPos, s.Pos(), result, nil, nil))
		}

		return false, nil, nil
	}

	// Reset the position and return the mustMatch result
	s.SetPos(beginPos)

	return m.MustMatch.Match(s, l)
}

// End matches the end of stream
type End struct {
	id      string
	logging bool
}

// NewEndF creates a new end of stream match
func NewEndF(id string, logging bool) *End {
	return &End{
		id:      id,
		logging: logging,
	}
}

func NewEnd() *End {
	return NewEndF("", false)
}

func (m *End) ID() string {
	return m.id
}

func (m *End) Match(s ObjStreamer, l Logger) (bool, *Result, error) {
	if s.Finished() {
		return true, NewResult(m.id, s.Pos(), s.Pos(), nil), nil
	}

	if m.logging && l != nil {
		l.Log(NewMismatch(m.id, s.Pos(), s.Pos(), nil, nil, nil))
	}

	return false, nil, nil
}
