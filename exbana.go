package exbana

import (
	"fmt"
	"math/rand"
)

// Object returned from streamer, a real implementation could have rune as object type
type Object interface{}

// ObjectEqualFunc test if two objects are equal
type ObjectEqualFunc func(Object, Object) bool

// Position is an abstract position from a stream implementation
type Position interface{}

// Value is an abstract return value from result
type Value interface{}

// ObjectReader interface for a stream that can serve objects to a pattern matcher
type ObjectReader interface {
	Peek() (Object, error)
	Read() (Object, error)
	Finished() bool
	Position() Position
	SetPosition(Position) error
	ValueForRange(Position, Position) Value
}

// ObjectWriter interface to write generated objects
type ObjectWriter interface {
	Write(...Object) error
	Finish() error
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

// Components (Concat & Alt)
func (r *Result) Components() []*Result {
	return r.Value.([]*Result)
}

// Component at index (Concat & Alt)
func (r *Result) Component(index int) *Result {
	return r.Value.([]*Result)[index]
}

// Values for components (Concat & Alt)
func (r *Result) Values() []Value {
	components := r.Value.([]*Result)
	values := make([]Value, len(components))
	for index, component := range components {
		values[index] = component.Value
	}
	return values
}

// NestedResult for Alt
func (r *Result) NestedResult() *Result {
	return r.Value.(*Result)
}

// NestedValue for Alt
func (r *Result) NestedValue() Value {
	return r.Value.(*Result).Value
}

// Optional result
func (r *Result) Optional() *Result {
	components := r.Value.([]*Result)
	if len(components) > 0 {
		return components[0]
	}

	return nil
}

// TransformFunc can transform match result to final value
type TransformFunc func(*Result, TransformTable, ObjectReader) Value

// TransformTable is used to map matcher identifiers to a transform function
type TransformTable map[string]TransformFunc

// Transform a match result to a value
func (t TransformTable) Transform(m *Result, stream ObjectReader) Value {
	f, ok := t[m.ID]
	if ok {
		return f(m, t, stream)
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
	Match(ObjectReader, Logger) (bool, *Result, error)
	Generate(ObjectWriter)
	ID() string
}

// Patterns is a convenience type for a slice of pattern interfaces
type Patterns []Pattern

// Scan stream for pattern and return all results
func Scan(stream ObjectReader, pattern Pattern) ([]*Result, error) {
	results := []*Result{}
	for !stream.Finished() {
		pos := stream.Position()
		matched, result, err := pattern.Match(stream, nil)
		if err != nil {
			return nil, err
		}
		if matched {
			results = append(results, result)
		} else {
			stream.SetPosition(pos)
			stream.Read()
		}
	}

	return results, nil
}

// UnitMatchFunc matches a single object
type UnitMatchFunc func(Object) bool

// UnitGenerateFunc generates a single object
type UnitGenerateFunc func() Object

// UnitPattern represents a single object pattern
type UnitPattern struct {
	id           string
	logging      bool
	matchFunc    UnitMatchFunc
	GenerateFunc UnitGenerateFunc
}

// Unitx creates a new unit pattern with identifier and logging
func Unitx(id string, logging bool, matchFunc UnitMatchFunc) *UnitPattern {
	return &UnitPattern{
		id:           id,
		logging:      logging,
		matchFunc:    matchFunc,
		GenerateFunc: nil,
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
func (p *UnitPattern) Match(s ObjectReader, l Logger) (bool, *Result, error) {
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

// Generate writes an object to an object writer
func (p *UnitPattern) Generate(wr ObjectWriter) {
	if p.GenerateFunc != nil {
		wr.Write(p.GenerateFunc())
	}
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
func (p *SeriesPattern) Match(s ObjectReader, l Logger) (bool, *Result, error) {
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

// Generate writes a series of objects to an object writer
func (p *SeriesPattern) Generate(wr ObjectWriter) {
	wr.Write(p.series...)
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
func (p *ConcatPattern) Match(s ObjectReader, l Logger) (bool, *Result, error) {
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

// Generate writes a concatenation of patterns to a writer
func (p *ConcatPattern) Generate(wr ObjectWriter) {
	for _, childPattern := range p.Patterns {
		childPattern.Generate(wr)
	}
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
func (p *AltPattern) Match(s ObjectReader, l Logger) (bool, *Result, error) {
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

// Generate writes an alternation of patterns to a writer, randomly chosen
func (p *AltPattern) Generate(wr ObjectWriter) {
	p.Patterns[rand.Intn(len(p.Patterns))].Generate(wr)
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
func (p *RepPattern) Match(s ObjectReader, l Logger) (bool, *Result, error) {
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

// Generate writes pattern to a writer a random number of times
func (p *RepPattern) Generate(wr ObjectWriter) {
	min := p.min
	max := p.max

	if p.max == 0 {
		max = min + 5
	}

	n := rand.Intn(max-min+1) + min

	for i := 0; i < n; i += 1 {
		p.Pattern.Generate(wr)
	}
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
func (p *ExceptPattern) Match(s ObjectReader, l Logger) (bool, *Result, error) {
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

// Generate let's MustMatch generate to writer
func (p *ExceptPattern) Generate(wr ObjectWriter) {
	p.MustMatch.Generate(wr)
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
func (p *EndPattern) Match(s ObjectReader, l Logger) (bool, *Result, error) {
	if s.Finished() {
		return true, NewResult(p.id, s.Position(), s.Position(), nil), nil
	}

	if p.logging && l != nil {
		l.Log(NewMismatch(p.id, s.Position(), s.Position(), nil, nil, nil))
	}

	return false, nil, nil
}

// Generate sends finish to writer
func (p *EndPattern) Generate(wr ObjectWriter) {
	wr.Finish()
}
