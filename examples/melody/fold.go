package main

import (
	"errors"
	"fmt"

	"github.com/almerlucke/exbana/v3/examples/melody/abc"
)

// A folk tune, for the grammar, is a row of cells: one for every sixteenth note of the tune. A note is a cell that starts
// it (an Onset, with the step of the scale it is on) and cells that hold it, so a quarter note is an onset and three holds.
// The end of a part and the start of a second ending are cells too, the way they are marks in the notation.
//
//	| D F A d | f2 e2 d4 | ... |  becomes  Onset Hold Hold Hold  Onset Hold Hold Hold ...
type Kind uint8

const (
	Onset   Kind = iota // a note starts
	Hold                // the note, or rest, before goes on
	Rest                // a rest starts
	EndMark             // the end of a part: a repeat or a double bar
	Volta               // a second ending starts
)

// Cell is one sixteenth note of a tune, or a mark.
type Cell struct {
	Kind   Kind
	Degree int // the step of the scale of an Onset, from the tonic
}

func (c Cell) String() string {
	switch c.Kind {
	case Onset:
		return fmt.Sprintf("%d", c.Degree)
	case Hold:
		return "-"
	case Rest:
		return "z"
	case EndMark:
		return "||"
	}

	return "[2"
}

// slotTicks is how long a cell is.
const slotTicks = abc.TicksPerQuarter / 4

// Piece is a tune made into cells.
type Piece struct {
	Ref   int
	Title string
	Meter abc.Meter
	Cells []Cell
}

// Slots is the number of cells in a bar.
func (p *Piece) Slots() int { return slotsOf(p.Meter) }

func slotsOf(m abc.Meter) int { return m.Ticks() / slotTicks }

// Why a tune is left out. A grammar of four-bar phrases in parts is not a grammar of every tune.
var (
	ErrNotMajor  = errors.New("not in a major key")
	ErrChange    = errors.New("changes key or meter")
	ErrOffGrid   = errors.New("has notes that are not on the sixteenth note grid (triplets and the like)")
	ErrIrregular = errors.New("has bars that are not as long as the meter")
	ErrPhrases   = errors.New("has parts that are not made of phrases of four bars")
	ErrEnding    = errors.New("has a second ending of more than a bar")
	ErrParts     = errors.New("has more than four parts")
	ErrNoNotes   = errors.New("has a bar of nothing but rests")
)

// part is the bars between one repeat and the next, and the bars of the second ending that follow.
type part struct{ body, ending []abc.Bar }

// Fold makes a tune into cells. A bar that comes before the first full bar of a part (a pickup) is left out, and
// the last bar of a part, which is the short rest of the bar the pickup began, is filled up with rests. The parts are
// of 4k bars, and may have a second ending of a bar. A tune that is not like that is an error, that says why.
func Fold(t *abc.Tune) (*Piece, error) {
	first := t.Bars[0]

	if first.Key.Mode != "major" {
		return nil, ErrNotMajor
	}

	full := first.Meter.Ticks()

	for _, b := range t.Bars {
		if b.Key != first.Key || b.Meter != first.Meter {
			return nil, ErrChange
		}

		for _, e := range b.Events {
			if e.Ticks%slotTicks != 0 {
				return nil, ErrOffGrid
			}
		}
	}

	var (
		parts []part
		cur   part
	)

	bars := t.Bars

	for i := 0; i < len(bars); i++ {
		cur.body = append(cur.body, bars[i])

		if !bars[i].RepeatEnd {
			continue
		}

		// the bars of the second ending belong to the part
		for i+1 < len(bars) && bars[i+1].Ending == 2 && !bars[i+1].RepeatStart {
			i++
			cur.ending = append(cur.ending, bars[i])

			if bars[i].RepeatEnd {
				break
			}
		}

		parts = append(parts, cur)
		cur = part{}
	}

	if len(cur.body) > 0 {
		parts = append(parts, cur)
	}

	if len(parts) > maxParts {
		return nil, ErrParts
	}

	p := &Piece{Ref: t.Ref, Title: t.Title, Meter: first.Meter}

	for _, pt := range parts {
		body := pt.body

		if len(body) > 0 && body[0].Ticks < full {
			body = body[1:] // the pickup
		}

		if len(pt.ending) > 1 {
			return nil, ErrEnding
		}

		if n := len(body); n == 0 || n%4 != 0 || n > 4*maxPhrases {
			return nil, ErrPhrases
		}

		all := append(append([]abc.Bar(nil), body...), pt.ending...)

		for i, b := range all {
			// only the last bar of the body and of the ending can be short, they are the rest of a bar
			last := i == len(body)-1 || i == len(all)-1

			if b.Ticks > full || b.Ticks < full && !last {
				return nil, ErrIrregular
			}

			if len(pt.ending) > 0 && i == len(body) {
				p.Cells = append(p.Cells, Cell{Kind: Volta})
			}

			cells, err := barCells(b, full)
			if err != nil {
				return nil, err
			}

			p.Cells = append(p.Cells, cells...)
		}

		p.Cells = append(p.Cells, Cell{Kind: EndMark})
	}

	return p, nil
}

// barCells makes the cells of a bar. Notes that are tied inside the bar are one note, a tie over the bar line is not:
// the note is struck again. A bar that is short is filled up with a rest.
func barCells(b abc.Bar, full int) ([]Cell, error) {
	var cells []Cell

	events := make([]abc.Event, 0, len(b.Events))

	for _, e := range b.Events {
		if n := len(events); n > 0 && events[n-1].Tie && !e.Rest && !events[n-1].Rest && events[n-1].MIDI == e.MIDI {
			events[n-1].Ticks += e.Ticks
			events[n-1].Tie = e.Tie

			continue
		}

		events = append(events, e)
	}

	add := func(kind Kind, degree, slots int) {
		cells = append(cells, Cell{Kind: kind, Degree: degree})

		for i := 1; i < slots; i++ {
			cells = append(cells, Cell{Kind: Hold})
		}
	}

	notes := 0

	for _, e := range events {
		if e.Rest {
			add(Rest, 0, e.Ticks/slotTicks)
		} else {
			add(Onset, e.Degree, e.Ticks/slotTicks)
			notes++
		}
	}

	if short := full - b.Ticks; short > 0 {
		add(Rest, 0, short/slotTicks)
	}

	if notes == 0 {
		return nil, ErrNoNotes
	}

	return cells, nil
}
