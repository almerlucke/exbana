package main

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestMelodyIsMusical(t *testing.T) {
	for seed := int64(0); seed < 200; seed++ {
		notes, err := Compose(seed)
		if err != nil {
			t.Fatal(err)
		}

		// 2 phrases of 3 free bars and a cadence
		total, bar := 0.0, 0.0
		for i, n := range notes {
			if n.Degree < 0 || n.Degree > maxDegree {
				t.Fatalf("seed %d: note %d out of range: %v", seed, i, n)
			}
			total += n.Beats
			bar += n.Beats
			if bar > 4 {
				t.Fatalf("seed %d: note %d crosses a bar line", seed, i)
			}
			if bar == 4 {
				bar = 0
			}
		}
		if total != 32 || bar != 0 {
			t.Errorf("seed %d: %v beats, bar remainder %v", seed, total, bar)
		}

		// every phrase ends on a whole tonic
		last := notes[len(notes)-1]
		if last != (Note{0, 4}) {
			t.Errorf("seed %d: ends on %v", seed, last)
		}
	}
}

func TestMelodyIsReproducible(t *testing.T) {
	a, _ := Compose(99)
	b, _ := Compose(99)
	c, _ := Compose(100)
	if !reflect.DeepEqual(a, b) {
		t.Error("same seed gave different melodies")
	}
	if reflect.DeepEqual(a, c) {
		t.Error("different seeds gave the same melody")
	}
}

func TestMelodyMostlyMovesByStep(t *testing.T) {
	steps, leaps := 0, 0
	for seed := int64(0); seed < 100; seed++ {
		notes, _ := Compose(seed)
		for i := 1; i < len(notes); i++ {
			d := notes[i].Degree - notes[i-1].Degree
			if d < 0 {
				d = -d
			}
			if d <= 1 {
				steps++
			} else {
				leaps++
			}
		}
	}
	if steps < leaps {
		t.Errorf("expected mostly stepwise motion, got %d steps and %d leaps", steps, leaps)
	}
}

func TestTheBuiltInSongsAreReadByTheGrammar(t *testing.T) {
	melodies, titles, err := builtin()
	if err != nil {
		t.Fatal(err)
	}

	for i, notes := range melodies {
		bars, err := Parse(notes)
		if err != nil {
			t.Errorf("%s: %v", titles[i], err)
			continue
		}

		if len(bars) != 6 {
			t.Errorf("%s: %d bars, want 6", titles[i], len(bars))
		}
	}

	// Ode to Joy: three bars of quarters, then (after the cadence) two of quarters and a bar of quarters and a half
	notes, _ := ParseNotes(corpus[0].notes)
	bars, _ := Parse(notes)

	if want := []int{0, 0, 0, 0, 0, 2}; fmt.Sprint(bars) != fmt.Sprint(want) {
		t.Errorf("Ode to Joy has the bars %v, want %v", bars, want)
	}
}

// What the grammar writes, it reads: the parse of a generated melody says the rhythm of the bars it was made from.
func TestWhatTheGrammarWritesItReads(t *testing.T) {
	melodies, _, _ := builtin()

	learned, err := Learn(melodies, 0.25)
	if err != nil {
		t.Fatal(err)
	}

	for _, m := range []*Model{DefaultModel(), learned} {
		for seed := int64(0); seed < 100; seed++ {
			notes, err := ComposeWith(m, seed)
			if err != nil {
				t.Fatal(err)
			}

			bars, err := Parse(notes)
			if err != nil {
				t.Fatalf("seed %d: %v", seed, err)
			}

			// the bars the parse found are the bars of the melody: the rhythm of each of them is its name
			var rhythms []string
			var rhythm string
			for _, n := range notes {
				if n.Beats == 4 {
					continue // the cadence
				}

				rhythm += durations[n.Beats]
				if len(rhythm) > 0 && sumBeats(rhythm) == 4 {
					rhythms = append(rhythms, rhythm)
					rhythm = ""
				}
			}

			if len(bars) != len(rhythms) {
				t.Fatalf("seed %d: %d bars read, %d made", seed, len(bars), len(rhythms))
			}

			for i, b := range bars {
				if barNames[b] != rhythms[i] {
					t.Fatalf("seed %d: bar %d read as %s, made as %s", seed, i, barNames[b], rhythms[i])
				}
			}
		}
	}
}

func TestMelodiesThatAreNotPiecesAreRefused(t *testing.T) {
	for name, text := range map[string]string{
		"a bar of five beats":   "C4/q C4/q C4/q C4/h D4/q D4/q D4/q D4/q E4/q E4/q E4/q E4/q C4/w C4/q C4/q C4/q C4/q D4/q D4/q D4/q D4/q E4/q E4/q E4/q E4/q C4/w",
		"no cadence":            "C4/q C4/q C4/q C4/q D4/q D4/q D4/q D4/q E4/q E4/q E4/q E4/q E4/w C4/q C4/q C4/q C4/q D4/q D4/q D4/q D4/q E4/q E4/q E4/q E4/q C4/w",
		"too short":             "C4/q C4/q C4/q C4/q C4/w",
		"a note after the end":  "C4/q C4/q C4/q C4/q D4/q D4/q D4/q D4/q E4/q E4/q E4/q E4/q C4/w C4/q C4/q C4/q C4/q D4/q D4/q D4/q D4/q E4/q E4/q E4/q E4/q C4/w C4/q",
		"a whole note in a bar": "C4/w D4/q D4/q D4/q D4/q E4/q E4/q E4/q E4/q C4/w C4/q C4/q C4/q C4/q D4/q D4/q D4/q D4/q E4/q E4/q E4/q E4/q C4/w",
		"nothing":               "",
	} {
		notes, err := ParseNotes(text)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}

		if bars, err := Parse(notes); err == nil {
			t.Errorf("%s: read as the bars %v", name, bars)
		}
	}

	// the error says where the grammar got stuck: the 5th beat is the first note that cannot be in the bar, note 4 (0-based)
	notes, _ := ParseNotes("C4/q C4/q C4/q C4/h")
	if _, err := Parse(notes); err == nil || !strings.Contains(err.Error(), "syntax error") {
		t.Errorf("got %v", err)
	}
}

// A model that is learned from melodies is what they were made with, when there are enough of them.
func TestLearningFindsTheOddsAMelodyWasMadeWith(t *testing.T) {
	made := &Model{Bars: []float64{1, 0, 0, 0, 9}, Deltas: []float64{0, 0, 0, 0, 1, 0, 8, 0, 0}}

	var melodies [][]Note
	for seed := int64(0); seed < 400; seed++ {
		notes, err := ComposeWith(made, seed)
		if err != nil {
			t.Fatal(err)
		}

		melodies = append(melodies, notes)
	}

	learned, err := Learn(melodies, 0)
	if err != nil {
		t.Fatal(err)
	}

	share := func(ws []float64, i int) float64 {
		sum := 0.0
		for _, w := range ws {
			sum += w
		}

		return ws[i] / sum
	}

	if got := share(learned.Bars, 4); got < 0.85 || got > 0.95 {
		t.Errorf("q h q is %.2f of the bars, made with 0.9", got)
	}

	for _, i := range []int{1, 2, 3} {
		if learned.Bars[i] != 0 {
			t.Errorf("bar %s was never made but is learned", barNames[i])
		}
	}

	// the walk is held in by the range of the notes, so the steps up and down are not all as made: but nothing else is
	for i, w := range learned.Deltas {
		if w != 0 && deltas[i] != 0 && deltas[i] != 2 && deltas[i] != -2 {
			t.Errorf("step %d was never made but is learned", deltas[i])
		}
	}
}

func TestLearnedMelodiesMoveLikeTheSongs(t *testing.T) {
	melodies, _, _ := builtin()

	learned, err := Learn(melodies, 0.25)
	if err != nil {
		t.Fatal(err)
	}

	// the songs hardly ever leap, and neither do the melodies learned from them, more than the hand-made ones do
	leaps := func(m *Model) float64 {
		big, all := 0, 0
		for seed := int64(0); seed < 200; seed++ {
			notes, _ := ComposeWith(m, seed)
			for i := 1; i < len(notes); i++ {
				d := notes[i].Degree - notes[i-1].Degree
				if d < 0 {
					d = -d
				}
				if d > 2 {
					big++
				}
				all++
			}
		}

		return float64(big) / float64(all)
	}

	if l, h := leaps(learned), leaps(DefaultModel()); l >= h {
		t.Errorf("learned melodies leap %.3f of the time, hand-made ones %.3f", l, h)
	}
}

func TestLearningNeedsMelodiesOfTheGrammar(t *testing.T) {
	notes, _ := ParseNotes("C4/q C4/q C4/q C4/h")
	if _, err := Learn([][]Note{notes}, 1); err == nil || !strings.Contains(err.Error(), "melody 1") {
		t.Errorf("got %v", err)
	}

	// a leap the model has no room for
	bad, _ := ParseNotes("C4/q C5/q C4/q C4/q D4/q D4/q D4/q D4/q E4/q E4/q E4/q E4/q C4/w C4/q C4/q C4/q C4/q D4/q D4/q D4/q D4/q E4/q E4/q E4/q E4/q C4/w")
	if _, err := Learn([][]Note{bad}, 1); err == nil || !strings.Contains(err.Error(), "leap") {
		t.Errorf("got %v", err)
	}
}

func TestNotesAreWrittenAndRead(t *testing.T) {
	notes, err := ParseNotes("C4/q | D4/h E5/q B4/w")
	if err != nil {
		t.Fatal(err)
	}

	want := []Note{{0, 1}, {1, 2}, {9, 1}, {6, 4}}
	if !reflect.DeepEqual(notes, want) {
		t.Fatalf("got %v", notes)
	}

	var parts []string
	for _, n := range notes {
		parts = append(parts, n.String())
	}

	if got := strings.Join(parts, " "); got != "C4/q D4/h E5/q B4/w" {
		t.Errorf("written as %q", got)
	}

	for _, bad := range []string{"C4", "H4/q", "C3/q", "C4/x", "C44/q", "C4/qq"} {
		if _, err := ParseNotes(bad); err == nil {
			t.Errorf("%q was read", bad)
		}
	}
}

func sumBeats(rhythm string) float64 {
	total := 0.0
	for _, r := range rhythm {
		switch r {
		case 'q':
			total++
		case 'h':
			total += 2
		}
	}

	return total
}
