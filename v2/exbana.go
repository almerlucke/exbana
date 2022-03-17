package exbana

import (
	"bytes"
	"fmt"
	"io"
	"math/rand"
)

// ObjectReader interface for a stream that can serve objects to a pattern matcher
type ObjectReader[T, P any] interface {
	Peek() (T, error)
	Read() (T, error)
	Finished() bool
	Position() P
	SetPosition(P) error
	Range(P, P) []T
}

// ObjectWriter interface to write generated objects
type ObjectWriter[T any] interface {
	Write(...T) error
	Finish() error
}

// Result contains matched pattern position, identifier, optional value and optional components
type Result[T, P any] struct {
	ID         string
	Begin      P
	End        P
	Value      []T
	Components []*Result[T, P]
}

// NewResult creates a new pattern match result
func NewResult[T, P any](id string, begin P, end P, value []T, components []*Result[T, P]) *Result[T, P] {
	return &Result[T, P]{
		ID:         id,
		Begin:      begin,
		End:        end,
		Value:      value,
		Components: components,
	}
}

// Values for components (Concat & Repeat)
func (r *Result[T, P]) Values() [][]T {
	components := r.Components
	values := make([][]T, len(components))
	for index, component := range components {
		values[index] = component.Value
	}
	return values
}

// Optional result
func (r *Result[T, P]) Optional() *Result[T, P] {
	if len(r.Components) > 0 {
		return r.Components[0]
	}

	return nil
}

// TransformTable is used to map matcher identifiers to a transform function
type TransformTable[T, P any] map[string]func(*Result[T, P], TransformTable[T, P], ObjectReader[T, P]) any

// Transform a match result to a value
func (t TransformTable[T, P]) Transform(m *Result[T, P], stream ObjectReader[T, P]) any {
	f, ok := t[m.ID]
	if ok {
		return f(m, t, stream)
	}

	return m.Value
}

// Mismatch can hold information about a pattern mismatch and possibly the sub pattern that caused the mismatch
// and the sub patterns that matched so far, an optional error can be passed to give more specific information
type Mismatch[T, P any] struct {
	Result[T, P]
	SubMismatch *Result[T, P]
	SubMatches  []*Result[T, P]
	Error       error
}

// NewMismatch creates a new pattern mismatch
func NewMismatch[T, P any](id string, begin P, end P, subMisMatch *Result[T, P], subMatches []*Result[T, P], err error) *Mismatch[T, P] {
	return &Mismatch[T, P]{
		Result: Result[T, P]{
			ID:         id,
			Begin:      begin,
			End:        end,
			Value:      nil,
			Components: nil,
		},
		SubMismatch: subMisMatch,
		SubMatches:  subMatches,
		Error:       err,
	}
}

// Logger can be used to log pattern mismatches during pattern matching
type Logger[T, P any] interface {
	Log(mismatch *Mismatch[T, P])
}

// Pattern can match objects from a stream, has an identifier
type Pattern[T, P any] interface {
	Match(ObjectReader[T, P], Logger[T, P]) (bool, *Result[T, P], error)
	Generate(ObjectWriter[T]) error
	Print(io.Writer) error
	ID() string
}

// Patterns is a convenience type for a slice of pattern interfaces
type Patterns[T, P any] []Pattern[T, P]

// Scan stream for pattern and return all results
func Scan[T, P any](stream ObjectReader[T, P], pattern Pattern[T, P]) ([]*Result[T, P], error) {
	results := []*Result[T, P]{}
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

// PrintRules prints all rules and returns a string
func PrintRules[T, P any](patterns []Pattern[T, P]) (string, error) {
	var buf bytes.Buffer

	for _, pattern := range patterns {
		_, err := buf.WriteString(fmt.Sprintf("%v = ", pattern.ID()))
		if err != nil {
			return "", err
		}
		err = pattern.Print(&buf)
		if err != nil {
			return "", err
		}
		_, err = buf.WriteString("\n")
		if err != nil {
			return "", err
		}
	}

	return buf.String(), nil
}

// UnitPattern represents a single object pattern
type UnitPattern[T, P any] struct {
	id           string
	logging      bool
	matchFunc    func(T) bool
	GenerateFunc func() T
	PrintOutput  string
}

// Unitx creates a new unit pattern with identifier and logging
func Unitx[T, P any](id string, logging bool, matchFunc func(T) bool) *UnitPattern[T, P] {
	return &UnitPattern[T, P]{
		id:           id,
		logging:      logging,
		matchFunc:    matchFunc,
		GenerateFunc: nil,
	}
}

// Unit creates a new unit pattern
func Unit[T, P any](matchFunction func(T) bool) *UnitPattern[T, P] {
	return Unitx[T, P]("", false, matchFunction)
}

// ID returns the unit pattern ID
func (p *UnitPattern[T, P]) ID() string {
	return p.id
}

// Match matches the unit object against a stream
func (p *UnitPattern[T, P]) Match(s ObjectReader[T, P], l Logger[T, P]) (bool, *Result[T, P], error) {
	pos := s.Position()
	entity, err := s.Read()

	if err != nil {
		return false, nil, err
	}

	if p.matchFunc(entity) {
		return true, NewResult(p.id, pos, s.Position(), s.Range(pos, s.Position()), nil), nil
	} else if p.logging && l != nil {
		l.Log(NewMismatch[T](p.id, pos, s.Position(), nil, nil, nil))
	}

	return false, nil, nil
}

// Generate writes an object to an object writer
func (p *UnitPattern[T, P]) Generate(wr ObjectWriter[T]) error {
	if p.GenerateFunc != nil {
		return wr.Write(p.GenerateFunc())
	}

	return nil
}

// Print writes EBNF to io.Writer
func (p *UnitPattern[T, P]) Print(wr io.Writer) error {
	_, err := wr.Write([]byte(p.PrintOutput))
	if err != nil {
		return err
	}

	return nil
}

// SeriesPattern represents a series of objects to match
type SeriesPattern[T, P any] struct {
	id          string
	logging     bool
	eqFunc      func(T, T) bool
	series      []T
	PrintOutput string
}

// Seriesx creates a new series pattern with identifier and logging
func Seriesx[T, P any](id string, logging bool, eqFunc func(T, T) bool, series ...T) *SeriesPattern[T, P] {
	return &SeriesPattern[T, P]{
		id:      id,
		logging: logging,
		series:  series,
		eqFunc:  eqFunc,
	}
}

// Series creates a new series pattern
func Series[T, P any](eqFunc func(T, T) bool, series ...T) *SeriesPattern[T, P] {
	return Seriesx[T, P]("", false, eqFunc, series...)
}

// ID return the series pattern ID
func (p *SeriesPattern[T, P]) ID() string {
	return p.id
}

// Match matches the series pattern against a stream
func (p *SeriesPattern[T, P]) Match(s ObjectReader[T, P], l Logger[T, P]) (bool, *Result[T, P], error) {
	beginPos := s.Position()

	for _, e1 := range p.series {
		e2, err := s.Read()
		if err != nil {
			return false, nil, err
		}

		if !p.eqFunc(e1, e2) {
			if p.logging && l != nil {
				l.Log(NewMismatch[T](p.id, beginPos, s.Position(), nil, nil, nil))
			}

			return false, nil, nil
		}
	}

	endPos := s.Position()

	return true, NewResult(p.id, beginPos, endPos, s.Range(beginPos, endPos), nil), nil
}

// Generate writes a series of objects to an object writer
func (p *SeriesPattern[T, P]) Generate(wr ObjectWriter[T]) error {
	return wr.Write(p.series...)
}

// Print writes EBNF to io.Writer
func (p *SeriesPattern[T, P]) Print(wr io.Writer) error {
	_, err := wr.Write([]byte(p.PrintOutput))
	if err != nil {
		return err
	}

	return nil
}

// printChild prints child pattern
func printChild[T, P any](wr io.Writer, child Pattern[T, P]) error {
	id := child.ID()
	if id != "" {
		_, err := wr.Write([]byte(id))
		if err != nil {
			return err
		}
	} else {
		err := child.Print(wr)
		if err != nil {
			return err
		}
	}

	return nil
}

// Concat matches a series of patterns AND style in order (concatenation)
type ConcatPattern[T, P any] struct {
	id       string
	logging  bool
	Patterns Patterns[T, P]
}

// Concatx creates a new concat pattern with identifier and logging
func Concatx[T, P any](id string, logging bool, patterns ...Pattern[T, P]) *ConcatPattern[T, P] {
	return &ConcatPattern[T, P]{
		id:       id,
		logging:  logging,
		Patterns: patterns,
	}
}

// Concat creates a new AND pattern
func Concat[T, P any](patterns ...Pattern[T, P]) *ConcatPattern[T, P] {
	return Concatx("", false, patterns...)
}

// ID returns the AND pattern ID
func (p *ConcatPattern[T, P]) ID() string {
	return p.id
}

// Match matches And against a stream, fails if any of the patterns mismatches
func (p *ConcatPattern[T, P]) Match(s ObjectReader[T, P], l Logger[T, P]) (bool, *Result[T, P], error) {
	beginPos := s.Position()

	matches := []*Result[T, P]{}

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
					p.id, beginPos, subEndPos, NewResult[T](pm.ID(), subBeginPos, subEndPos, nil, nil), matches, nil),
				)
			}

			return false, nil, nil
		}
	}

	return true, NewResult(p.id, beginPos, s.Position(), nil, matches), nil
}

// Generate writes a concatenation of patterns to a writer
func (p *ConcatPattern[T, P]) Generate(wr ObjectWriter[T]) error {
	for _, child := range p.Patterns {
		err := child.Generate(wr)
		if err != nil {
			return err
		}
	}

	return nil
}

// Print EBNF concatenation group
func (p *ConcatPattern[T, P]) Print(wr io.Writer) error {
	_, err := wr.Write([]byte("("))
	if err != nil {
		return err
	}

	first := true

	for _, child := range p.Patterns {
		if !first {
			_, err = wr.Write([]byte(", "))
			if err != nil {
				return err
			}
		}

		err = printChild(wr, child)
		if err != nil {
			return err
		}

		first = false
	}

	_, err = wr.Write([]byte(")"))

	return err
}

// AltPattern matches a series of patterns OR style in order (alternation)
type AltPattern[T, P any] struct {
	id       string
	logging  bool
	Patterns Patterns[T, P]
}

// Altx creates a new Alt pattern with identifier and logging
func Altx[T, P any](id string, logging bool, patterns ...Pattern[T, P]) *AltPattern[T, P] {
	return &AltPattern[T, P]{
		id:       id,
		logging:  logging,
		Patterns: patterns,
	}
}

// Alt creates a new OR pattern
func Alt[T, P any](patterns ...Pattern[T, P]) *AltPattern[T, P] {
	return Altx("", false, patterns...)
}

// ID returns the ID of the OR pattern
func (p *AltPattern[T, P]) ID() string {
	return p.id
}

// Match matches the OR pattern against a stream, fails if all of the patterns mismatch
func (p *AltPattern[T, P]) Match(s ObjectReader[T, P], l Logger[T, P]) (bool, *Result[T, P], error) {
	beginPos := s.Position()

	for _, pm := range p.Patterns {
		s.SetPosition(beginPos)

		matched, result, err := pm.Match(s, l)
		if err != nil {
			return false, nil, err
		}

		if matched {
			return true, NewResult(p.id, beginPos, s.Position(), nil, []*Result[T, P]{result}), nil
		}
	}

	if p.logging && l != nil {
		l.Log(NewMismatch[T](p.id, beginPos, s.Position(), nil, nil, nil))
	}

	return false, nil, nil
}

// Generate writes an alternation of patterns to a writer, randomly chosen
func (p *AltPattern[T, P]) Generate(wr ObjectWriter[T]) error {
	return p.Patterns[rand.Intn(len(p.Patterns))].Generate(wr)
}

// Print EBNF alternation group
func (p *AltPattern[T, P]) Print(wr io.Writer) error {
	_, err := wr.Write([]byte("("))
	if err != nil {
		return err
	}

	first := true

	for _, child := range p.Patterns {
		if !first {
			_, err = wr.Write([]byte(" | "))
			if err != nil {
				return err
			}
		}
		err = printChild(wr, child)
		if err != nil {
			return err
		}

		first = false
	}

	_, err = wr.Write([]byte(")"))

	return err
}

// RepPattern matches a pattern repetition
type RepPattern[T, P any] struct {
	id      string
	logging bool
	Pattern Pattern[T, P]
	min     int
	max     int
	MaxGen  int
}

// Repx creates a new repetition pattern
func Repx[T, P any](id string, logging bool, pattern Pattern[T, P], min int, max int) *RepPattern[T, P] {
	return &RepPattern[T, P]{
		id:      id,
		logging: logging,
		Pattern: pattern,
		min:     min,
		max:     max,
		MaxGen:  5,
	}
}

// Rep creates a new repetition pattern
func Rep[T, P any](pattern Pattern[T, P], min int, max int) *RepPattern[T, P] {
	return Repx("", false, pattern, min, max)
}

// Optx creates a new optional pattern
func Optx[T, P any](id string, logging bool, pattern Pattern[T, P]) *RepPattern[T, P] {
	return Repx(id, logging, pattern, 0, 1)
}

// Opt creates a new optional pattern
func Opt[T, P any](pattern Pattern[T, P]) *RepPattern[T, P] {
	return Optx("", false, pattern)
}

// Anyx creates a new any repetition pattern
func Anyx[T, P any](id string, logging bool, pattern Pattern[T, P]) *RepPattern[T, P] {
	return Repx(id, logging, pattern, 0, 0)
}

// Any creates a new any repetition pattern
func Any[T, P any](pattern Pattern[T, P]) *RepPattern[T, P] {
	return Anyx("", false, pattern)
}

// Nx creates a new repetition pattern for exactly n times
func Nx[T, P any](id string, logging bool, pattern Pattern[T, P], n int) *RepPattern[T, P] {
	return Repx(id, logging, pattern, n, n)
}

// N creates a new repetition pattern for exactly n times
func N[T, P any](pattern Pattern[T, P], n int) *RepPattern[T, P] {
	return Nx("", false, pattern, n)
}

// ID returns the ID of the repetition pattern
func (p *RepPattern[T, P]) ID() string {
	return p.id
}

// Match matches the repetition pattern aginst a stream
func (p *RepPattern[T, P]) Match(s ObjectReader[T, P], l Logger[T, P]) (bool, *Result[T, P], error) {
	beginPos := s.Position()
	matches := []*Result[T, P]{}

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
			l.Log(NewMismatch[T](p.id, beginPos, s.Position(), nil, nil, fmt.Errorf("expected minimum of %d repetitions", p.min)))
		}

		return false, nil, nil
	}

	return true, NewResult(p.id, beginPos, s.Position(), nil, matches), nil
}

// Generate writes pattern to a writer a random number of times
func (p *RepPattern[T, P]) Generate(wr ObjectWriter[T]) error {
	min := p.min
	max := p.max

	if p.max == 0 {
		max = min + p.MaxGen
	}

	n := rand.Intn(max-min+1) + min

	for i := 0; i < n; i += 1 {
		err := p.Pattern.Generate(wr)
		if err != nil {
			return err
		}
	}

	return nil
}

// printAny prints EBNF zero or more
func (p *RepPattern[T, P]) printAny(wr io.Writer) error {
	err := p.Pattern.Print(wr)
	if err != nil {
		return err
	}

	_, err = wr.Write([]byte("*"))

	return err
}

// printAny prints EBNF optional
func (p *RepPattern[T, P]) printOptional(wr io.Writer) error {
	err := p.Pattern.Print(wr)
	if err != nil {
		return err
	}

	_, err = wr.Write([]byte("?"))

	return err
}

// printAny prints EBNF at least one
func (p *RepPattern[T, P]) printAtLeastOne(wr io.Writer) error {
	err := p.Pattern.Print(wr)
	if err != nil {
		return err
	}

	_, err = wr.Write([]byte("+"))

	return err
}

// Print EBNF repetition pattern
func (p *RepPattern[T, P]) Print(wr io.Writer) error {
	if p.min == 0 && p.max == 0 {
		return p.printAny(wr)
	} else if p.min == 0 && p.max == 1 {
		return p.printOptional(wr)
	} else if p.min == 1 && p.max == 0 {
		return p.printAtLeastOne(wr)
	}

	var err error
	oneValue := p.min == p.max

	if !oneValue {
		_, err = wr.Write([]byte("("))
		if err != nil {
			return err
		}
	}

	_, err = wr.Write([]byte(fmt.Sprintf("%v * ", p.min)))
	if err != nil {
		return err
	}

	err = p.Pattern.Print(wr)
	if err != nil {
		return err
	}

	if !oneValue {
		_, err = wr.Write([]byte(fmt.Sprintf(", %v * ", p.max-p.min)))
		if err != nil {
			return err
		}

		err = p.Pattern.Print(wr)
		if err != nil {
			return err
		}

		_, err = wr.Write([]byte("?)"))
		if err != nil {
			return err
		}
	}

	return nil
}

// ExceptPattern must not match the Except pattern but must match the MustMatch pattern
type ExceptPattern[T, P any] struct {
	id        string
	logging   bool
	MustMatch Pattern[T, P]
	Except    Pattern[T, P]
}

// Exceptx creates a new except pattern
func Exceptx[T, P any](id string, logging bool, mustMatch Pattern[T, P], except Pattern[T, P]) *ExceptPattern[T, P] {
	return &ExceptPattern[T, P]{
		id:        id,
		logging:   logging,
		MustMatch: mustMatch,
		Except:    except,
	}
}

// Except creates a new except pattern
func Except[T, P any](mustMatch Pattern[T, P], except Pattern[T, P]) *ExceptPattern[T, P] {
	return Exceptx("", false, mustMatch, except)
}

// ID returns the except pattern ID
func (p *ExceptPattern[T, P]) ID() string {
	return p.id
}

// Match matches the exception against a stream
func (p *ExceptPattern[T, P]) Match(s ObjectReader[T, P], l Logger[T, P]) (bool, *Result[T, P], error) {
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
func (p *ExceptPattern[T, P]) Generate(wr ObjectWriter[T]) error {
	return p.MustMatch.Generate(wr)
}

// Print EBNF except pattern
func (p *ExceptPattern[T, P]) Print(wr io.Writer) error {
	err := p.MustMatch.Print(wr)
	if err != nil {
		return err
	}
	_, err = wr.Write([]byte(" - "))
	if err != nil {
		return err
	}

	err = p.Except.Print(wr)

	return err
}

// EndPattern matches the end of stream
type EndPattern[T, P any] struct {
	id      string
	logging bool
}

// EndF creates a new end of stream pattern
func Endx[T, P any](id string, logging bool) *EndPattern[T, P] {
	return &EndPattern[T, P]{
		id:      id,
		logging: logging,
	}
}

// End creates a new end of stream pattern
func End[T, P any]() *EndPattern[T, P] {
	return Endx[T, P]("", false)
}

// ID returns end of stream pattern ID
func (p *EndPattern[T, P]) ID() string {
	return p.id
}

// Match matches a end of stream pattern against a stream
func (p *EndPattern[T, P]) Match(s ObjectReader[T, P], l Logger[T, P]) (bool, *Result[T, P], error) {
	if s.Finished() {
		return true, NewResult[T](p.id, s.Position(), s.Position(), nil, nil), nil
	}

	if p.logging && l != nil {
		l.Log(NewMismatch[T](p.id, s.Position(), s.Position(), nil, nil, nil))
	}

	return false, nil, nil
}

// Generate sends finish to writer
func (p *EndPattern[T, P]) Generate(wr ObjectWriter[T]) error {
	return wr.Finish()
}

// Print EBNF end of stream (does nothing)
func (p *EndPattern[T, P]) Print(wr io.Writer) error {
	return nil
}
