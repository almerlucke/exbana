package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/examples/melody/abc"
	"github.com/almerlucke/exbana/v3/readers/slice"
)

// Corpus is tunes made into cells, with why the other tunes are not there.
type Corpus struct {
	Pieces []*Piece
	Left   map[string]int // why tunes were left out, and how many
	Unread []error        // tunes the ABC reader could not read
	Tunes  int            // the tunes that were read
}

// LoadCorpus reads the ABC files of a directory (the Nottingham Music Database, for one), and makes the tunes into cells.
func LoadCorpus(dir string) (*Corpus, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.abc"))
	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no .abc files in %s", dir)
	}

	c := &Corpus{Left: map[string]int{}}

	for _, name := range files {
		f, err := os.Open(name)
		if err != nil {
			return nil, err
		}

		tunes, errs := abc.Read(f)
		f.Close()

		c.Unread = append(c.Unread, errs...)
		c.Tunes += len(tunes)

		for _, t := range tunes {
			p, err := Fold(t)
			if err != nil {
				c.Left[err.Error()]++
				continue
			}

			c.Pieces = append(c.Pieces, p)
		}
	}

	return c, nil
}

// Meters says how many tunes there are of each meter.
func (c *Corpus) Meters() map[abc.Meter]int {
	out := map[abc.Meter]int{}
	for _, p := range c.Pieces {
		out[p.Meter]++
	}

	return out
}

// bar is where a bar of a parse is, and what kind it is.
type barSpan struct {
	kind       string // bar, half or full
	begin, end int
	q          int // for half and full: the slot the last note starts in
}

// move is a step of a melody, and where it was taken from.
type move struct{ prev, strength, last, delta int }

// parseFolk reads a tune with the grammar. It says the bars of the parse and the kinds of parts, or why the tune is not
// a tune of the grammar.
func parseFolk(g cpattern, p *Piece) ([]barSpan, []PartKind, error) {
	rd := slice.New(p.Cells)
	log := ebnf.NewFarthestLog[Cell, int](rd)
	ctx := ebnf.NewParseContext[Cell, int](rd).WithLogger(log).WithMemo(func(i int) int { return i })

	ok, m, err := ebnf.Parse(g, ctx)
	if err != nil {
		return nil, nil, err
	}

	if !ok {
		if e := log.Last(); e != nil {
			return nil, nil, fmt.Errorf("not a tune of the grammar: %w", e)
		}

		return nil, nil, fmt.Errorf("not a tune of the grammar")
	}

	var (
		bars  []barSpan
		kinds []PartKind
	)

	var walk func(*ebnf.Match[Cell, int])
	walk = func(m *ebnf.Match[Cell, int]) {
		id := m.ID()

		switch {
		case id == "bar":
			bars = append(bars, barSpan{kind: "bar", begin: m.Begin, end: m.End})

			return
		case strings.HasPrefix(id, "half:") || strings.HasPrefix(id, "full:"):
			var kind string

			var q, r int

			fmt.Sscanf(strings.Replace(id, ":", " ", -1), "%s %d %d", &kind, &q, &r)
			bars = append(bars, barSpan{kind: kind, begin: m.Begin, end: m.End, q: q})

			return
		case strings.HasPrefix(id, "part:"):
			var k PartKind

			fmt.Sscanf(strings.Replace(id, ":", " ", -1), "part %d %d", &k.Phrases, &k.Ending)
			kinds = append(kinds, k)
		}

		for _, c := range m.Components {
			walk(c)
		}
	}
	walk(m)

	return bars, kinds, nil
}

// Learned is what learning found out about a corpus.
type Learned struct {
	Model  *FolkModel
	Tunes  int      // the tunes of the meter that were read with the grammar
	Failed []string // the tunes the grammar could not read, and why
}

// LearnFolk reads the tunes of a meter with the grammar, and counts what they do: the shape of the tune, where phrases
// come to rest and on what note, how the rhythm goes, and how far the notes step. The counts are the odds of the model.
func LearnFolk(c *Corpus, meter abc.Meter) (*Learned, error) {
	m := NewFolkModel(meter)
	n := m.slots()
	g := FolkGrammar(m)

	var (
		out       = &Learned{Model: m}
		rhythm    = make([][maxRun][3]float64, n)
		closing   = map[string][]float64{"half": make([]float64, n), "full": make([]float64, n)}
		final     = map[string][]float64{"half": make([]float64, 7), "full": make([]float64, 7)}
		moves     []move
		degrees   []int
		starts    []int
		shapes    = map[string]*Shape{}
		shapeKeys []string
	)

	for _, p := range c.Pieces {
		if p.Meter != meter {
			continue
		}

		bars, kinds, err := parseFolk(g, p)
		if err != nil {
			out.Failed = append(out.Failed, fmt.Sprintf("tune %d (%s): %v", p.Ref, p.Title, err))
			continue
		}

		out.Tunes++

		key := fmt.Sprint(kinds)
		if shapes[key] == nil {
			shapes[key] = &Shape{Parts: kinds}
			shapeKeys = append(shapeKeys, key)
		}

		shapes[key].Weight++

		// the cells are read the way they are written: the same state, in the same order
		st := &folkState{m: m}

		for _, b := range bars {
			free := b.end
			if b.kind != "bar" {
				free = b.begin + b.q
			}

			for j := b.begin; j < free; j++ {
				c, slot := p.Cells[j], j-b.begin

				if st.started {
					idx := map[Kind]int{Hold: 0, Onset: 1, Rest: 2}[c.Kind]
					rhythm[slot][min(st.run, maxRun)-1][idx]++
				}

				switch c.Kind {
				case Hold:
					st.run++
				case Rest:
					st.run, st.started = 1, true
				case Onset:
					if st.hasNote {
						if d := c.Degree - st.last; d >= -maxStep && d <= maxStep {
							moves = append(moves, move{st.delta, strength(meter, slot), st.last, d})
						}

						st.delta = c.Degree - st.last
					} else {
						starts = append(starts, c.Degree)
						st.delta = 0
					}

					degrees = append(degrees, c.Degree)
					st.run, st.started, st.hasNote, st.last = 1, true, true, c.Degree
				}
			}

			if b.kind == "bar" {
				continue
			}

			// the last note of the phrase, and what is after it
			c := p.Cells[b.begin+b.q]
			closing[b.kind][b.q]++
			final[b.kind][(c.Degree%7+7)%7]++
			degrees = append(degrees, c.Degree)

			if st.hasNote {
				st.delta = c.Degree - st.last
			}

			st.run, st.started, st.hasNote, st.last = 1, true, true, c.Degree

			for j := b.begin + b.q + 1; j < b.end; j++ {
				if p.Cells[j].Kind == Hold {
					st.run++
				} else {
					st.run = 1
				}
			}
		}
	}

	if out.Tunes == 0 {
		return nil, fmt.Errorf("no tunes in %d/%d to learn from", meter.Num, meter.Den)
	}

	// the range the melodies keep to: not the rare note that is far out
	sort.Ints(degrees)
	m.Lo, m.Hi = degrees[len(degrees)/200], degrees[len(degrees)-1-len(degrees)/200]

	m.Start = make([]float64, m.Hi-m.Lo+1)
	for _, d := range starts {
		m.Start[min(max(d, m.Lo), m.Hi)-m.Lo]++
	}

	// The rhythm is counted for every slot and run, and most of them are seen few times. So the odds of a slot are the counts
	// of that slot, and where there are few of them, the odds of places in the bar that are as strong, and where there are
	// few of those, the odds for a note that has gone on as long. That is a back-off: a rare place is like its likes.
	const trust = 4.0

	var (
		byRun    [maxRun][3]float64
		byStrong [4][maxRun][3]float64
	)

	for s := range rhythm {
		for r := range rhythm[s] {
			for k := 0; k < 3; k++ {
				byRun[r][k] += rhythm[s][r][k]
				byStrong[strength(meter, s)][r][k] += rhythm[s][r][k]
			}
		}
	}

	odds := func(counts [3]float64, back [3]float64) [3]float64 {
		n := counts[0] + counts[1] + counts[2]

		var out [3]float64

		for k := range out {
			out[k] = (counts[k] + trust*back[k]) / (n + trust)
		}

		return out
	}

	// a note or rest is always followed by something, so this adds up to one with the smoothing at the bottom
	flat := func(counts [3]float64) [3]float64 {
		return odds(counts, [3]float64{0.5, 0.498, 0.002})
	}

	for s := range rhythm {
		for r := range rhythm[s] {
			rhythm[s][r] = odds(rhythm[s][r], odds(byStrong[strength(meter, s)][r], flat(byRun[r])))
		}
	}

	add := func(ws []float64, a float64) {
		for i := range ws {
			ws[i] += a
		}
	}

	for _, kind := range []string{"half", "full"} {
		add(closing[kind], 0.2)
		add(final[kind], 0.2)
	}

	step := StepModel{Motion: rows(28), Degree: rows(14), Zone: rows(3), Marginal: make([]float64, 2*maxStep+1)}

	for _, set := range [][][]float64{step.Motion, step.Degree, step.Zone} {
		for _, row := range set {
			for i := range row {
				row[i] = 0
			}
		}
	}

	for _, mv := range moves {
		i := mv.delta + maxStep
		step.Motion[motionRow(mv.prev, mv.strength)][i]++
		step.Degree[degreeRow(mv.last, mv.strength)][i]++
		step.Zone[zoneRow(mv.last, m.Lo, m.Hi)][i]++
		step.Marginal[i]++
	}

	// every row is made into odds that add up to one, with a little added for the steps that never happened
	normalize := func(row []float64) {
		sum := 0.0
		for i := range row {
			row[i] += 0.05
			sum += row[i]
		}

		for i := range row {
			row[i] /= sum
		}
	}

	for _, set := range [][][]float64{step.Motion, step.Degree, step.Zone, {step.Marginal}} {
		for _, row := range set {
			normalize(row)
		}
	}

	add(m.Start, 0.2)

	m.Rhythm, m.Closing, m.Final, m.Step = rhythm, closing, final, step

	sort.Strings(shapeKeys)

	for _, k := range shapeKeys {
		m.Shapes = append(m.Shapes, *shapes[k])
	}

	return out, nil
}
