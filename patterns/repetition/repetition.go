package repetition

import (
	"fmt"
	ebnf "github.com/almerlucke/exbana/v2"
	"io"
	"math/rand"
)

// Repetition matches a pattern repetition
type Repetition[T, P any] struct {
	*ebnf.BasePattern[T, P]
	pattern ebnf.Pattern[T, P]
	min     int
	max     int
	maxGen  int
}

// New creates a new repetition pattern
func New[T, P any](pattern ebnf.Pattern[T, P], min int, max int) *Repetition[T, P] {
	rep := &Repetition[T, P]{
		pattern: pattern,
		min:     min,
		max:     max,
	}

	rep.SetSelf(rep)

	return rep
}

func Optional[T, P any](pattern ebnf.Pattern[T, P]) *Repetition[T, P] {
	return New[T, P](pattern, 0, 1)
}

func Any[T, P any](pattern ebnf.Pattern[T, P]) *Repetition[T, P] {
	return New[T, P](pattern, 0, 0)
}

func OneOrMore[T, P any](pattern ebnf.Pattern[T, P]) *Repetition[T, P] {
	return New[T, P](pattern, 1, 0)
}

// Match matches the repetition pattern aginst a stream
func (rep *Repetition[T, P]) Match(r ebnf.Reader[T, P]) (bool, *ebnf.Match[T, P], error) {
	var matches []*ebnf.Match[T, P]

	beginPos, err := r.Position()
	if ebnf.IsStreamError(err) {
		return false, nil, err
	}

	for {
		if r.Finished() {
			break
		}

		resetPos, err := r.Position()
		if ebnf.IsStreamError(err) {
			return false, nil, err
		}

		matched, result, err := rep.pattern.Match(r)
		if err != nil {
			return false, nil, err
		}

		if !matched {
			err = r.SetPosition(resetPos)
			if ebnf.IsStreamError(err) {
				return false, nil, err
			}

			break
		}

		matches = append(matches, result)
		if rep.max != 0 && len(matches) == rep.max {
			break
		}
	}

	if len(matches) < rep.min {
		endPos, err := r.Position()
		if ebnf.IsStreamError(err) {
			return false, nil, err
		}

		rep.Logger().LogMismatch(ebnf.NewMismatch[T, P](rep, beginPos, endPos, nil, matches))

		return false, nil, nil
	}

	endPos, err := r.Position()
	if ebnf.IsStreamError(err) {
		return false, nil, err
	}

	return true, ebnf.NewMatch[T, P](rep, beginPos, endPos, nil, matches), nil
}

// SetMaxGen sets the maximum generated entities on top of min
func (rep *Repetition[T, P]) SetMaxGen(maxGen int) {
	rep.maxGen = maxGen
}

// Generate writes pattern to a writer a random number of times
func (rep *Repetition[T, P]) Generate(w ebnf.Writer[T]) error {
	repMin := rep.min
	repMax := rep.max

	if rep.max == 0 {
		repMax = repMin + rep.maxGen
	}

	n := rand.Intn(repMax-repMin+1) + repMin

	for i := 0; i < n; i++ {
		err := rep.pattern.Generate(w)
		if err != nil {
			return err
		}
	}

	return nil
}

// printAny prints EBNF zero or more
func (rep *Repetition[T, P]) printAny(w io.Writer) error {
	err := rep.pattern.Print(w)
	if err != nil {
		return err
	}

	_, err = w.Write([]byte("*"))

	return err
}

// printAny prints EBNF optional
func (rep *Repetition[T, P]) printOptional(w io.Writer) error {
	err := rep.pattern.Print(w)
	if err != nil {
		return err
	}

	_, err = w.Write([]byte("?"))

	return err
}

// printAny prints EBNF at least one
func (rep *Repetition[T, P]) printAtLeastOne(w io.Writer) error {
	err := rep.pattern.Print(w)
	if err != nil {
		return err
	}

	_, err = w.Write([]byte("+"))

	return err
}

// Print EBNF repetition pattern
func (rep *Repetition[T, P]) Print(w io.Writer) error {
	if rep.min == 0 && rep.max == 0 {
		return rep.printAny(w)
	} else if rep.min == 0 && rep.max == 1 {
		return rep.printOptional(w)
	} else if rep.min == 1 && rep.max == 0 {
		return rep.printAtLeastOne(w)
	}

	var err error
	oneValue := rep.min == rep.max

	if !oneValue {
		_, err = w.Write([]byte("("))
		if err != nil {
			return err
		}
	}

	_, err = w.Write([]byte(fmt.Sprintf("%v * ", rep.min)))
	if err != nil {
		return err
	}

	err = rep.pattern.Print(w)
	if err != nil {
		return err
	}

	if !oneValue {
		_, err = w.Write([]byte(fmt.Sprintf(", %v * ", rep.max-rep.min)))
		if err != nil {
			return err
		}

		err = rep.pattern.Print(w)
		if err != nil {
			return err
		}

		_, err = w.Write([]byte("?)"))
		if err != nil {
			return err
		}
	}

	return nil
}
