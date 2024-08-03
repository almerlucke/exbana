package exbana

import (
	"bytes"
	"fmt"
	"io"
)

// IsStreamError check if err is set and not io.EOF
func IsStreamError(err error) bool {
	return err != nil && err != io.EOF
}

// Scan stream for pattern and return all results
func Scan[T, P any](stream Reader[T, P], pattern Pattern[T, P]) ([]*Match[T, P], error) {
	var results []*Match[T, P]

	for !stream.Finished() {
		pos, err := stream.Position()
		if IsStreamError(err) {
			return nil, err
		}
		matched, result, err := pattern.Match(stream)
		if err != nil {
			return nil, err
		}
		if matched {
			results = append(results, result)
		} else {
			err = stream.SetPosition(pos)
			if IsStreamError(err) {
				return nil, err
			}
			_, err = stream.Skip(1)
			if IsStreamError(err) {
				return nil, err
			}
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

func PrintChild[T, P any](w io.Writer, child Pattern[T, P]) error {
	id := child.ID()
	if id != "" {
		_, err := w.Write([]byte(id))
		if err != nil {
			return err
		}
	} else {
		err := child.Print(w)
		if err != nil {
			return err
		}
	}

	return nil
}

//
//// printChild prints child pattern
//func printChild[T, P any](wr io.Writer, child Pattern[T, P]) error {
//	id := child.ID()
//	if id != "" {
//		_, err := wr.Write([]byte(id))
//		if err != nil {
//			return err
//		}
//	} else {
//		err := child.Print(wr)
//		if err != nil {
//			return err
//		}
//	}
//
//	return nil
//}
//

//
//// AltPattern matches a series of patterns OR style in order (alternation)
//type AltPattern[T, P any] struct {
//	id       string
//	logging  bool
//	Patterns Patterns[T, P]
//}
//
//// Altx creates a new Alt pattern with identifier and logging
//func Altx[T, P any](id string, logging bool, patterns ...Pattern[T, P]) *AltPattern[T, P] {
//	return &AltPattern[T, P]{
//		id:       id,
//		logging:  logging,
//		Patterns: patterns,
//	}
//}
//
//// Alt creates a new OR pattern
//func Alt[T, P any](patterns ...Pattern[T, P]) *AltPattern[T, P] {
//	return Altx("", false, patterns...)
//}
//
//// ID returns the ID of the OR pattern
//func (p *AltPattern[T, P]) ID() string {
//	return p.id
//}
//
//// Match matches the OR pattern against a stream, fails if all of the patterns mismatch
//func (p *AltPattern[T, P]) Match(s Reader[T, P], l MismatchLogger[T, P]) (bool, *Match[T, P], error) {
//	beginPos, err := s.Position()
//	if IsStreamError(err) {
//		return false, nil, err
//	}
//
//	for _, pm := range p.Patterns {
//		err := s.SetPosition(beginPos)
//		if IsStreamError(err) {
//			return false, nil, err
//		}
//
//		matched, result, err := pm.Match(s, l)
//		if err != nil {
//			return false, nil, err
//		}
//
//		if matched {
//			endPos, err := s.Position()
//			if IsStreamError(err) {
//				return false, nil, err
//			}
//
//			return true, NewMatch[T, P](p, beginPos, endPos, nil, []*Match[T, P]{result}), nil
//		}
//	}
//
//	if p.logging && l != nil {
//		endPos, err := s.Position()
//		if IsStreamError(err) {
//			return false, nil, err
//		}
//
//		l.Log(NewMismatch[T, P](p, beginPos, endPos))
//	}
//
//	return false, nil, nil
//}
//
//// Generate writes an alternation of patterns to a writer, randomly chosen
//func (p *AltPattern[T, P]) Generate(wr Writer[T]) error {
//	return p.Patterns[rand.Intn(len(p.Patterns))].Generate(wr)
//}
//
//// Print EBNF alternation group
//func (p *AltPattern[T, P]) Print(wr io.Writer) error {
//	_, err := wr.Write([]byte("("))
//	if err != nil {
//		return err
//	}
//
//	first := true
//
//	for _, child := range p.Patterns {
//		if !first {
//			_, err = wr.Write([]byte(" | "))
//			if err != nil {
//				return err
//			}
//		}
//		err = printChild(wr, child)
//		if err != nil {
//			return err
//		}
//
//		first = false
//	}
//
//	_, err = wr.Write([]byte(")"))
//
//	return err
//}
//
//// RepPattern matches a pattern repetition
//type RepPattern[T, P any] struct {
//	id      string
//	logging bool
//	Pattern Pattern[T, P]
//	Min     int
//	Max     int
//	MaxGen  int
//}
//
//// Repx creates a new repetition pattern
//func Repx[T, P any](id string, logging bool, pattern Pattern[T, P], min int, max int) *RepPattern[T, P] {
//	return &RepPattern[T, P]{
//		id:      id,
//		logging: logging,
//		Pattern: pattern,
//		Min:     min,
//		Max:     max,
//		MaxGen:  5,
//	}
//}
//
//// Rep creates a new repetition pattern
//func Rep[T, P any](pattern Pattern[T, P], min int, max int) *RepPattern[T, P] {
//	return Repx("", false, pattern, min, max)
//}
//
//// Optx creates a new optional pattern
//func Optx[T, P any](id string, logging bool, pattern Pattern[T, P]) *RepPattern[T, P] {
//	return Repx(id, logging, pattern, 0, 1)
//}
//
//// Opt creates a new optional pattern
//func Opt[T, P any](pattern Pattern[T, P]) *RepPattern[T, P] {
//	return Optx("", false, pattern)
//}
//
//// Anyx creates a new any repetition pattern
//func Anyx[T, P any](id string, logging bool, pattern Pattern[T, P]) *RepPattern[T, P] {
//	return Repx(id, logging, pattern, 0, 0)
//}
//
//// Any creates a new any repetition pattern
//func Any[T, P any](pattern Pattern[T, P]) *RepPattern[T, P] {
//	return Anyx("", false, pattern)
//}
//
//// Nx creates a new repetition pattern for exactly n times
//func Nx[T, P any](id string, logging bool, pattern Pattern[T, P], n int) *RepPattern[T, P] {
//	return Repx(id, logging, pattern, n, n)
//}
//
//// N creates a new repetition pattern for exactly n times
//func N[T, P any](pattern Pattern[T, P], n int) *RepPattern[T, P] {
//	return Nx("", false, pattern, n)
//}
//
//// ID returns the ID of the repetition pattern
//func (p *RepPattern[T, P]) ID() string {
//	return p.id
//}
//
//// Match matches the repetition pattern aginst a stream
//func (p *RepPattern[T, P]) Match(s Reader[T, P], l MismatchLogger[T, P]) (bool, *Match[T, P], error) {
//	beginPos, err := s.Position()
//	if IsStreamError(err) {
//		return false, nil, err
//	}
//
//	matches := []*Match[T, P]{}
//
//	for {
//		if s.Finished() {
//			break
//		}
//
//		resetPos, err := s.Position()
//		if IsStreamError(err) {
//			return false, nil, err
//		}
//
//		matched, result, err := p.Pattern.Match(s, l)
//		if err != nil {
//			return false, nil, err
//		}
//
//		if !matched {
//			err = s.SetPosition(resetPos)
//			if IsStreamError(err) {
//				return false, nil, err
//			}
//
//			break
//		}
//
//		matches = append(matches, result)
//		if p.Max != 0 && len(matches) == p.Max {
//			break
//		}
//	}
//
//	if len(matches) < p.Min {
//		if p.logging && l != nil {
//			endPos, err := s.Position()
//			if IsStreamError(err) {
//				return false, nil, err
//			}
//
//			l.Log(NewMismatchx[T, P](p, beginPos, endPos, nil, matches))
//		}
//
//		return false, nil, nil
//	}
//
//	endPos, err := s.Position()
//	if IsStreamError(err) {
//		return false, nil, err
//	}
//
//	return true, NewMatch[T, P](p, beginPos, endPos, nil, matches), nil
//}
//
//// Generate writes pattern to a writer a random number of times
//func (p *RepPattern[T, P]) Generate(wr Writer[T]) error {
//	min := p.Min
//	max := p.Max
//
//	if p.Max == 0 {
//		max = min + p.MaxGen
//	}
//
//	n := rand.Intn(max-min+1) + min
//
//	for i := 0; i < n; i += 1 {
//		err := p.Pattern.Generate(wr)
//		if err != nil {
//			return err
//		}
//	}
//
//	return nil
//}
//
//// printAny prints EBNF zero or more
//func (p *RepPattern[T, P]) printAny(wr io.Writer) error {
//	err := p.Pattern.Print(wr)
//	if err != nil {
//		return err
//	}
//
//	_, err = wr.Write([]byte("*"))
//
//	return err
//}
//
//// printAny prints EBNF optional
//func (p *RepPattern[T, P]) printOptional(wr io.Writer) error {
//	err := p.Pattern.Print(wr)
//	if err != nil {
//		return err
//	}
//
//	_, err = wr.Write([]byte("?"))
//
//	return err
//}
//
//// printAny prints EBNF at least one
//func (p *RepPattern[T, P]) printAtLeastOne(wr io.Writer) error {
//	err := p.Pattern.Print(wr)
//	if err != nil {
//		return err
//	}
//
//	_, err = wr.Write([]byte("+"))
//
//	return err
//}
//
//// Print EBNF repetition pattern
//func (p *RepPattern[T, P]) Print(wr io.Writer) error {
//	if p.Min == 0 && p.Max == 0 {
//		return p.printAny(wr)
//	} else if p.Min == 0 && p.Max == 1 {
//		return p.printOptional(wr)
//	} else if p.Min == 1 && p.Max == 0 {
//		return p.printAtLeastOne(wr)
//	}
//
//	var err error
//	oneValue := p.Min == p.Max
//
//	if !oneValue {
//		_, err = wr.Write([]byte("("))
//		if err != nil {
//			return err
//		}
//	}
//
//	_, err = wr.Write([]byte(fmt.Sprintf("%v * ", p.Min)))
//	if err != nil {
//		return err
//	}
//
//	err = p.Pattern.Print(wr)
//	if err != nil {
//		return err
//	}
//
//	if !oneValue {
//		_, err = wr.Write([]byte(fmt.Sprintf(", %v * ", p.Max-p.Min)))
//		if err != nil {
//			return err
//		}
//
//		err = p.Pattern.Print(wr)
//		if err != nil {
//			return err
//		}
//
//		_, err = wr.Write([]byte("?)"))
//		if err != nil {
//			return err
//		}
//	}
//
//	return nil
//}
//
//// ExceptPattern must not match the Except pattern but must match the MustMatch pattern
//type ExceptPattern[T, P any] struct {
//	id        string
//	logging   bool
//	MustMatch Pattern[T, P]
//	Except    Pattern[T, P]
//}
//
//// Exceptx creates a new except pattern
//func Exceptx[T, P any](id string, logging bool, mustMatch Pattern[T, P], except Pattern[T, P]) *ExceptPattern[T, P] {
//	return &ExceptPattern[T, P]{
//		id:        id,
//		logging:   logging,
//		MustMatch: mustMatch,
//		Except:    except,
//	}
//}
//
//// Except creates a new except pattern
//func Except[T, P any](mustMatch Pattern[T, P], except Pattern[T, P]) *ExceptPattern[T, P] {
//	return Exceptx("", false, mustMatch, except)
//}
//
//// ID returns the except pattern ID
//func (p *ExceptPattern[T, P]) ID() string {
//	return p.id
//}
//
//// Match matches the exception against a stream
//func (p *ExceptPattern[T, P]) Match(s Reader[T, P], l MismatchLogger[T, P]) (bool, *Match[T, P], error) {
//	beginPos, err := s.Position()
//	if IsStreamError(err) {
//		return false, nil, err
//	}
//
//	// First check for the exception match, we do not want to match the exception
//	matched, result, err := p.Except.Match(s, l)
//	if err != nil {
//		return false, nil, err
//	}
//
//	if matched {
//		if p.logging && l != nil {
//			endPos, err := s.Position()
//			if IsStreamError(err) {
//				return false, nil, err
//			}
//
//			l.Log(NewMismatchx[T, P](p, beginPos, endPos, result, nil))
//		}
//
//		return false, nil, nil
//	}
//
//	// Reset the position and return the mustMatch result
//	err = s.SetPosition(beginPos)
//	if IsStreamError(err) {
//		return false, nil, err
//	}
//
//	return p.MustMatch.Match(s, l)
//}
//
//// Generate let's MustMatch generate to writer
//func (p *ExceptPattern[T, P]) Generate(wr Writer[T]) error {
//	return p.MustMatch.Generate(wr)
//}
//
//// Print EBNF except pattern
//func (p *ExceptPattern[T, P]) Print(wr io.Writer) error {
//	err := p.MustMatch.Print(wr)
//	if err != nil {
//		return err
//	}
//	_, err = wr.Write([]byte(" - "))
//	if err != nil {
//		return err
//	}
//
//	err = p.Except.Print(wr)
//
//	return err
//}
//
//// EndPattern matches the end of stream
//type EndPattern[T, P any] struct {
//	id      string
//	logging bool
//}
//
//// EndF creates a new end of stream pattern
//func Endx[T, P any](id string, logging bool) *EndPattern[T, P] {
//	return &EndPattern[T, P]{
//		id:      id,
//		logging: logging,
//	}
//}
//
//// End creates a new end of stream pattern
//func End[T, P any]() *EndPattern[T, P] {
//	return Endx[T, P]("", false)
//}
//
//// ID returns end of stream pattern ID
//func (p *EndPattern[T, P]) ID() string {
//	return p.id
//}
//
//// Match matches a end of stream pattern against a stream
//func (p *EndPattern[T, P]) Match(s Reader[T, P], l MismatchLogger[T, P]) (bool, *Match[T, P], error) {
//	pos, err := s.Position()
//	if IsStreamError(err) {
//		return false, nil, err
//	}
//
//	if s.Finished() {
//		return true, NewMatch[T, P](p, pos, pos, nil, nil), nil
//	}
//
//	if p.logging && l != nil {
//		l.Log(NewMismatch[T, P](p, pos, pos))
//	}
//
//	return false, nil, nil
//}
//
//// Generate sends finish to writer
//func (p *EndPattern[T, P]) Generate(wr Writer[T]) error {
//	return wr.Finish()
//}
//
//// Print EBNF end of stream (does nothing)
//func (p *EndPattern[T, P]) Print(wr io.Writer) error {
//	return nil
//}
