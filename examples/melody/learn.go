package main

import (
	"fmt"
	"strings"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/patterns/end"
	"github.com/almerlucke/exbana/v3/readers/slice"
)

// Parse reads a known melody with the grammar. It says which rhythm every bar has (an index into barNames), or why the
// melody is not a piece of the grammar.
func Parse(notes []Note) ([]int, error) {
	// the end makes the parse take the whole melody, or backtrack to another way of reading it
	piece := conc(GrammarOf(DefaultModel()), end.New[Note, int]())

	ok, m, err := ebnf.Parse(piece, ebnf.NewParseContext[Note, int](slice.New(notes)))
	if err != nil {
		return nil, err
	}

	if !ok {
		return nil, fmt.Errorf("not a piece of the grammar: %s", explain(notes))
	}

	var bars []int

	var walk func(*ebnf.Match[Note, int])
	walk = func(m *ebnf.Match[Note, int]) {
		for i, name := range barNames {
			if m.ID() == name {
				bars = append(bars, i)
				return
			}
		}

		for _, c := range m.Components {
			walk(c)
		}
	}
	walk(m)

	return bars, nil
}

// explain says where a melody stops being a piece: the number of the note that the grammar could not go on with
func explain(notes []Note) string {
	rd := slice.New(notes)
	log := ebnf.NewFarthestLog[Note, int](rd)
	_, _, _ = ebnf.Parse(conc(GrammarOf(DefaultModel()), end.New[Note, int]()), ebnf.NewParseContext[Note, int](rd).WithLogger(log))

	if err := log.Last(); err != nil {
		return err.Error()
	}

	return "no note fits"
}

// Learn reads known melodies with the grammar and counts what they do: which rhythm every bar has, and how far each
// note is from the note before. The counts are the odds of a new model. smoothing is added to every count, so that
// what the melodies never did is rare and not impossible.
func Learn(melodies [][]Note, smoothing float64) (*Model, error) {
	m := &Model{Bars: make([]float64, len(barNames)), Deltas: make([]float64, len(deltas))}
	for i := range m.Bars {
		m.Bars[i] = smoothing
	}

	for i := range m.Deltas {
		m.Deltas[i] = smoothing
	}

	for n, notes := range melodies {
		bars, err := Parse(notes)
		if err != nil {
			return nil, fmt.Errorf("melody %d: %w", n+1, err)
		}

		for _, b := range bars {
			m.Bars[b]++
		}

		// a piece starts on the tonic, as far as the generator is concerned
		last := 0

		for i, note := range notes {
			if note.Degree < 0 || note.Degree > maxDegree {
				return nil, fmt.Errorf("melody %d: note %d (%v) is out of range", n+1, i+1, note)
			}

			// the cadence is not a step: the generator goes to the tonic there, wherever it was
			if note == (Note{0, 4}) {
				last = 0
				continue
			}

			at := -1
			for j, d := range deltas {
				if note.Degree-last == d {
					at = j
				}
			}

			if at < 0 {
				return nil, fmt.Errorf("melody %d: note %d (%v) is a leap of %d steps, the model has up to 4", n+1, i+1, note, note.Degree-last)
			}

			m.Deltas[at]++
			last = note.Degree
		}
	}

	return m, nil
}

// ParseNotes reads melodies written as "E4/q E4/q F4/q G4/h ...": the letter of the note in C major, the octave (4 is
// the one of middle C), and q, h or w for a quarter, half or whole note. Bar lines (|) are for the eye.
func ParseNotes(s string) ([]Note, error) {
	var notes []Note

	for _, field := range strings.Fields(s) {
		if field == "|" {
			continue
		}

		pitch, dur, ok := strings.Cut(field, "/")
		if !ok || len(pitch) != 2 || len(dur) != 1 {
			return nil, fmt.Errorf("%q is not a note like E4/q", field)
		}

		letter, octave := pitch[:1], int(pitch[1]-'0')

		degree := -1
		for i, n := range names {
			if n == letter {
				degree = i
			}
		}

		var beats float64
		for b, d := range durations {
			if d == dur {
				beats = b
			}
		}

		if degree < 0 || octave < 4 || octave > 5 || beats == 0 {
			return nil, fmt.Errorf("%q is not a note like E4/q (C to B, octave 4 or 5, q h or w)", field)
		}

		notes = append(notes, Note{degree + 7*(octave-4), beats})
	}

	return notes, nil
}
