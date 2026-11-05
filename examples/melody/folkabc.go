package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/almerlucke/exbana/v3/examples/melody/abc"
)

// tonicIndex is the number of the note a key is on, counting from C, for the steps of the scale to count from.
func tonicIndex(key string) (int, error) {
	if key == "" || !strings.Contains("CDEFGAB", key[:1]) {
		return 0, fmt.Errorf("%q is not a key like D or Bb", key)
	}

	return strings.Index("CDEFGAB", key[:1]), nil
}

// abcNote writes the note on a step of the scale, for a tonic. The tonic is in the octave of middle C.
func abcNote(tonic, step int) string {
	abs := tonic + step
	octave := 4 + floorDiv(abs, 7)
	letter := string("CDEFGAB"[((abs%7)+7)%7])

	if octave >= 5 {
		return strings.ToLower(letter) + strings.Repeat("'", octave-5)
	}

	return letter + strings.Repeat(",", 4-octave)
}

func floorDiv(a, b int) int {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}

	return q
}

// event is a note or a rest, and if it is tied to the next.
type event struct {
	pitch string // the note, or z for a rest
	slots int
	tied  bool
}

func (e event) String() string {
	s := e.pitch
	if e.slots > 1 {
		s += strconv.Itoa(e.slots)
	}

	if e.tied {
		s += "-"
	}

	return s
}

// abcPart is a part of a tune: its bars, and how many of them come before the second ending, if it has one.
type abcPart struct {
	bars   [][]*event
	ending int // the bars up to the second ending, 0 if there is none
}

// ABC writes the cells of a tune as a tune in ABC notation, that any ABC player can play. Every part is written with its
// repeat, and with a second ending where it has one. A note that goes on over a bar line is tied.
func ABC(cells []Cell, meter abc.Meter, key, title string, ref int) (string, error) {
	tonic, err := tonicIndex(key)
	if err != nil {
		return "", err
	}

	n := slotsOf(meter)

	var (
		parts []*abcPart
		cur   = &abcPart{}
		bar   []*event
		prev  *event // the last note or rest
		inBar int
	)

	endBar := func() {
		if len(bar) > 0 {
			cur.bars = append(cur.bars, bar)
		}

		bar, inBar = nil, 0
	}

	for _, c := range cells {
		switch c.Kind {
		case Volta:
			endBar()
			cur.ending = len(cur.bars)

			continue
		case EndMark:
			endBar()
			parts = append(parts, cur)
			cur = &abcPart{}

			continue
		}

		if inBar == n {
			endBar()
		}

		inBar++

		switch {
		case c.Kind == Onset:
			prev = &event{pitch: abcNote(tonic, c.Degree), slots: 1}
			bar = append(bar, prev)
		case c.Kind == Rest:
			prev = &event{pitch: "z", slots: 1}
			bar = append(bar, prev)
		case prev == nil:
			return "", fmt.Errorf("the tune starts with a hold")
		case inBar == 1:
			// the note goes on over the bar line: it is tied to the note it goes on from
			if prev.pitch != "z" {
				prev.tied = true
			}

			prev = &event{pitch: prev.pitch, slots: 1}
			bar = append(bar, prev)
		default:
			prev.slots++
		}
	}

	endBar()

	if len(cur.bars) > 0 {
		parts = append(parts, cur)
	}

	var b strings.Builder

	fmt.Fprintf(&b, "X:%d\nT:%s\nM:%d/%d\nL:1/16\nK:%s\n", ref, title, meter.Num, meter.Den, key)

	for _, p := range parts {
		b.WriteString("|:")

		for i, bar := range p.bars {
			words := make([]string, len(bar))
			for j, e := range bar {
				words[j] = e.String()
			}

			switch {
			case p.ending > 0 && i == p.ending-1:
				b.WriteString(" |1 ") // the last bar of the body is the first ending
			case p.ending > 0 && i == p.ending:
				b.WriteString(" :|2 ")
			case i > 0:
				b.WriteString(" | ")
			default:
				b.WriteString(" ")
			}

			b.WriteString(strings.Join(words, " "))
		}

		if p.ending > 0 {
			b.WriteString(" |]\n")
		} else {
			b.WriteString(" :|\n")
		}
	}

	return b.String(), nil
}
