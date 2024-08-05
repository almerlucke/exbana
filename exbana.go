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
