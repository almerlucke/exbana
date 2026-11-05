package main

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/almerlucke/exbana/v3/examples/melody/abc"
)

var meter44 = abc.Meter{Num: 4, Den: 4}

func cellsOf(s string) []Cell {
	var out []Cell

	for _, f := range strings.Fields(s) {
		switch f {
		case "-":
			out = append(out, Cell{Kind: Hold})
		case "z":
			out = append(out, Cell{Kind: Rest})
		case "||":
			out = append(out, Cell{Kind: EndMark})
		case "[2":
			out = append(out, Cell{Kind: Volta})
		default:
			var d int
			if _, err := fmt.Sscanf(f, "%d", &d); err != nil {
				panic(f)
			}

			out = append(out, Cell{Kind: Onset, Degree: d})
		}
	}

	return out
}

func foldABC(t *testing.T, header, music string) (*Piece, error) {
	t.Helper()

	h := map[string]string{"M": "4/4", "L": "1/4", "K": "C"}

	for _, f := range strings.Fields(header) {
		h[f[:1]] = f[2:]
	}

	tu, err := abc.ParseTune(fmt.Sprintf("X:1\nT:test\nM:%s\nL:%s\nK:%s\n%s\n", h["M"], h["L"], h["K"], music))
	if err != nil {
		t.Fatal(err)
	}

	return Fold(tu)
}

func TestFoldMakesCellsOfATune(t *testing.T) {
	// a pickup, three bars and the short rest of the bar the pickup began: four bars
	p, err := foldABC(t, "", "|: G | c e g e | d f a f | e g c' g | c3 :|")
	if err != nil {
		t.Fatal(err)
	}

	want := cellsOf(`
		7 - - - 9 - - - 11 - - - 9 - - -
		8 - - - 10 - - - 12 - - - 10 - - -
		9 - - - 11 - - - 14 - - - 11 - - -
		7 - - - - - - - - - - - z - - -
		||`)

	if !reflect.DeepEqual(p.Cells, want) {
		t.Errorf("got  %v\nwant %v", p.Cells, want)
	}

	// ties inside a bar are one note, over a bar line the note is struck again; a rest is a rest
	p, err = foldABC(t, "L:1/8", "c2-c2 d2 z2 | c4- c4 | c2 c2 c2 c2 | c2 c2 c2 c2 | c2 c2 c2 c2 | c2 c2 c2 c2 | c2 c2 c2 c2 | c2 c2 c2 c2||")
	if err != nil {
		t.Fatal(err)
	}

	if got := p.Cells[:16]; !reflect.DeepEqual(got, cellsOf("7 - - - - - - - 8 - - - z - - -")) {
		t.Errorf("first bar: %v", got)
	}

	// a tie between two notes of different pitch is a slur, and no tie
	p, err = foldABC(t, "L:1/8", "c2-d2 c4 | "+strings.Repeat("c8 |", 3)+"|")
	if err != nil {
		t.Fatal(err)
	}

	if got := p.Cells[:8]; !reflect.DeepEqual(got, cellsOf("7 - - - 8 - - -")) {
		t.Errorf("a slur: %v", got)
	}

	p, err = foldABC(t, "L:1/8", "c2-c2 d2 z2 | c4- c4 | c2 c2 c2 c2 | c2 c2 c2 c2 | c2 c2 c2 c2 | c2 c2 c2 c2 | c2 c2 c2 c2 | c2 c2 c2 c2||")
	if err != nil {
		t.Fatal(err)
	}

	// c4- c4 inside the bar are tied: one note of the whole bar... but they are not, the tie joins them
	if got := p.Cells[16:32]; !reflect.DeepEqual(got, cellsOf("7 - - - - - - - - - - - - - - -")) {
		t.Errorf("second bar: %v", got)
	}
}

func TestFoldSaysWhyATuneIsLeftOut(t *testing.T) {
	bars := func(n int) string { return strings.Repeat("c4 |", n) + "|" }

	for name, c := range map[string]struct {
		header, music string
		want          error
	}{
		"fine":                {"", "|: " + bars(4) + " :|", nil},
		"two parts":           {"", "|: " + bars(4) + " :| |: " + bars(8) + " :|", nil},
		"a second ending":     {"", "|: c4 | c4 | c4 |1 c4 :|2 c4 | c4 | c4 | c4 |]", ErrEnding},
		"a one bar ending":    {"", "|: c4 | c4 | c4 |1 c4 :|2 c4 |]", nil},
		"a triplet":           {"L:1/8", "|: (3ccc c c c c c c " + strings.Repeat("c8 |", 4) + " :|", ErrOffGrid},
		"minor":               {"K:Am", "|: " + bars(4) + " :|", ErrNotMajor},
		"three bars":          {"", "|: " + bars(3) + " :|", ErrPhrases},
		"six bars":            {"", "|: " + bars(6) + " :|", ErrPhrases},
		"a bar too short":     {"", "|: c4 | c3 | c4 | c4 :|", ErrIrregular},
		"a bar of rests only": {"", "|: c4 | c4 | c4 | z4 :|", ErrNoNotes},
		"five parts":          {"", strings.Repeat("|: "+bars(4)+" :| ", 5), ErrParts},
	} {
		_, err := foldABC(t, c.header, c.music)
		if err != c.want {
			t.Errorf("%s: got %v, want %v", name, err, c.want)
		}
	}

	// a change of key in the middle of a tune
	tu, err := abc.ParseTune("X:1\nM:4/4\nL:1/4\nK:C\n|: c4 | c4 | c4 | c4 :|\nK:G\n|: c4 | c4 | c4 | c4 :|\n")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := Fold(tu); err != ErrChange {
		t.Errorf("a change of key: %v", err)
	}
}

// aModel is a model of tunes that are as plain as can be: eighth notes, steps up and down, cadences on the tonic with the
// last note an eighth, and two shapes. What learning finds should be that.
func aModel(meter abc.Meter) *FolkModel {
	m := NewFolkModel(meter)
	n := m.slots()

	for s := range m.Rhythm {
		for r := range m.Rhythm[s] {
			if s%2 == 0 {
				m.Rhythm[s][r] = [3]float64{0, 1, 0}
			} else {
				m.Rhythm[s][r] = [3]float64{1, 0, 0}
			}
		}
	}

	for _, kind := range []string{"half", "full"} {
		for i := range m.Closing[kind] {
			m.Closing[kind][i] = 0
		}

		m.Closing[kind][n-2] = 1

		for i := range m.Final[kind] {
			m.Final[kind][i] = 0
		}
	}

	m.Final["full"][0] = 1 // a tune ends on the tonic
	m.Final["half"][4] = 1 // and a phrase on the fifth

	for _, set := range [][][]float64{m.Step.Motion, m.Step.Degree, m.Step.Zone} {
		for _, row := range set {
			for i := range row {
				row[i] = 0
			}

			row[maxStep+1], row[maxStep-1] = 1, 1
		}
	}

	m.Shapes = []Shape{
		{Parts: []PartKind{{2, 0}, {2, 1}}, Weight: 3},
		{Parts: []PartKind{{4, 0}}, Weight: 1},
	}

	return m
}

func piecesOf(t *testing.T, m *FolkModel, seeds int) []*Piece {
	t.Helper()

	var out []*Piece

	for seed := int64(0); seed < int64(seeds); seed++ {
		cells, err := ComposeFolk(m, seed)
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}

		out = append(out, &Piece{Ref: int(seed), Title: "made", Meter: m.Meter, Cells: cells})
	}

	return out
}

// What the grammar writes, it reads: the parse of a generated tune finds the parts and the bars it was made of.
func TestWhatTheFolkGrammarWritesItReads(t *testing.T) {
	for _, meter := range []abc.Meter{meter44, {Num: 6, Den: 8}, {Num: 3, Den: 4}} {
		m := aModel(meter)
		g := FolkGrammar(NewFolkModel(meter))
		n := m.slots()

		for _, p := range piecesOf(t, m, 40) {
			bars, kinds, err := parseFolk(g, p)
			if err != nil {
				t.Fatalf("%v seed %d: %v", meter, p.Ref, err)
			}

			// the parts, and the bars: 4 for every phrase, and a bar for a second ending
			want := 0

			for _, k := range kinds {
				want += 4*k.Phrases + k.Ending
			}

			if len(bars) != want {
				t.Fatalf("%v seed %d: %d bars, want %d for %v", meter, p.Ref, len(bars), want, kinds)
			}

			for _, b := range bars {
				if b.end-b.begin != n {
					t.Fatalf("a bar of %d cells, want %d", b.end-b.begin, n)
				}
			}

			if shape := fmt.Sprint(kinds); shape != "[{2 0} {2 1}]" && shape != "[{4 0}]" {
				t.Fatalf("the parts %v are not a shape of the model", kinds)
			}
		}
	}
}

func TestATuneThatIsNotOfTheGrammarIsRefused(t *testing.T) {
	g := FolkGrammar(NewFolkModel(meter44))

	good := piecesOf(t, aModel(meter44), 1)[0]

	cut := func(i int) *Piece {
		c := append(append([]Cell(nil), good.Cells[:i]...), good.Cells[i+1:]...)

		return &Piece{Meter: meter44, Cells: c}
	}

	for name, p := range map[string]*Piece{
		"a cell short":        cut(5),
		"no end mark":         {Meter: meter44, Cells: good.Cells[:len(good.Cells)-1]},
		"nothing":             {Meter: meter44},
		"two end marks":       {Meter: meter44, Cells: append(append([]Cell(nil), good.Cells...), Cell{Kind: EndMark})},
		"a hold at the start": {Meter: meter44, Cells: append([]Cell{{Kind: Hold}}, good.Cells...)},
	} {
		if _, _, err := parseFolk(g, p); err == nil {
			t.Errorf("%s was read", name)
		} else if !strings.Contains(err.Error(), "not a tune of the grammar") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// A model that is learned from tunes is what they were made with.
func TestLearningFindsWhatTunesWereMadeWith(t *testing.T) {
	made := aModel(meter44)
	c := &Corpus{Pieces: piecesOf(t, made, 300), Left: map[string]int{}}

	learned, err := LearnFolk(c, meter44)
	if err != nil {
		t.Fatal(err)
	}

	if learned.Tunes != 300 || len(learned.Failed) != 0 {
		t.Fatalf("%d tunes read, %v failed", learned.Tunes, learned.Failed)
	}

	m := learned.Model

	share := func(ws []float64, i int) float64 {
		sum := 0.0
		for _, w := range ws {
			sum += w
		}

		return ws[i] / sum
	}

	// the shapes, about 3 to 1
	got := map[string]float64{}
	total := 0.0

	for _, s := range m.Shapes {
		got[fmt.Sprint(s.Parts)] = s.Weight
		total += s.Weight
	}

	if len(got) != 2 || got["[{2 0} {2 1}]"]/total < 0.65 || got["[{2 0} {2 1}]"]/total > 0.85 {
		t.Errorf("the shapes are %v, made 3 to 1", got)
	}

	// phrases end on the fifth, and the tune on the tonic, with the last note at the slot it was made at
	if s := share(m.Final["full"], 0); s < 0.95 {
		t.Errorf("tunes end on the tonic %.2f of the time", s)
	}

	if s := share(m.Final["half"], 4); s < 0.95 {
		t.Errorf("phrases end on the fifth %.2f of the time", s)
	}

	if s := share(m.Closing["full"], 14); s < 0.95 {
		t.Errorf("the last note starts at slot 14 %.2f of the time", s)
	}

	// eighth notes: a new note on the even slots, a hold on the odd ones, whatever the note has gone on for
	for s := 0; s < 16; s++ {
		r := m.Rhythm[s][1-s%2] // the run is 2 at even slots, 1 at odd ones
		if s%2 == 0 && r[1] < 0.9 || s%2 == 1 && r[0] < 0.9 {
			t.Errorf("slot %d: %v", s, r)
		}
	}

	// steps are up or down by one
	for _, row := range m.Step.Marginal[:maxStep-1] {
		_ = row
	}

	if s := m.Step.Marginal[maxStep+1] + m.Step.Marginal[maxStep-1]; s < 0.95 {
		t.Errorf("steps of one are %.2f of the steps", s)
	}

	// and new tunes made with it are like those: they read with the grammar
	g := FolkGrammar(NewFolkModel(meter44))

	for _, p := range piecesOf(t, m, 20) {
		if _, _, err := parseFolk(g, p); err != nil {
			t.Fatalf("seed %d: %v", p.Ref, err)
		}
	}
}

func TestLearningNeedsTunesOfTheMeter(t *testing.T) {
	c := &Corpus{Pieces: piecesOf(t, aModel(meter44), 3)}

	if _, err := LearnFolk(c, abc.Meter{Num: 6, Den: 8}); err == nil {
		t.Error("learned from nothing")
	}
}

// A tune that is written as ABC and read again is the same tune: the writer, the reader and the folding agree. A note
// that goes on over a bar line is the exception, it is struck again, as folding says.
func TestABCOfATuneIsTheTune(t *testing.T) {
	for _, meter := range []abc.Meter{meter44, {Num: 6, Den: 8}, {Num: 2, Den: 4}} {
		made := aModel(meter)

		// notes that go on over the bar line, and rests, so that the writer has them to write
		for s := range made.Rhythm {
			for r := range made.Rhythm[s] {
				if s == 0 {
					made.Rhythm[s][r] = [3]float64{1, 3, 1}
				}
			}
		}

		for _, p := range piecesOf(t, made, 60) {
			text, err := ABC(p.Cells, meter, "D", "made", 1)
			if err != nil {
				t.Fatalf("%v seed %d: %v", meter, p.Ref, err)
			}

			tu, err := abc.ParseTune(text)
			if err != nil {
				t.Fatalf("%v seed %d: %v\n%s", meter, p.Ref, err, text)
			}

			back, err := Fold(tu)
			if err != nil {
				t.Fatalf("%v seed %d: %v\n%s", meter, p.Ref, err, text)
			}

			if want := restruck(p.Cells, meter); !reflect.DeepEqual(back.Cells, want) {
				t.Fatalf("%v seed %d:\n%s\nwritten %v\nread    %v", meter, p.Ref, text, want, back.Cells)
			}
		}
	}
}

// restruck is cells the way folding makes them: a hold that starts a bar is a note struck again.
func restruck(cells []Cell, meter abc.Meter) []Cell {
	n := slotsOf(meter)
	out := append([]Cell(nil), cells...)

	var prev Cell

	slot := 0

	for i, c := range out {
		if c.Kind == EndMark || c.Kind == Volta {
			continue
		}

		if c.Kind == Hold && slot%n == 0 {
			out[i] = prev
			c = prev
		}

		if c.Kind != Hold {
			prev = c
		}

		slot++
	}

	return out
}

func TestABCNotation(t *testing.T) {
	cells := cellsOf(`
		0 - 2 - 4 - - - 7 - - - 9 - - -
		4 - - - 2 - - - 0 - - - 0 - - -
		5 - - - 3 - - - 7 - - - 7 - - -
		0 - - - - - - - - - - - - - - -
		||`)

	text, err := ABC(cells, meter44, "G", "four", 7)
	if err != nil {
		t.Fatal(err)
	}

	want := "X:7\nT:four\nM:4/4\nL:1/16\nK:G\n|: G2 B2 d4 g4 b4 | d4 B4 G4 G4 | e4 c4 g4 g4 | G16 :|\n"
	if text != want {
		t.Errorf("got\n%s\nwant\n%s", text, want)
	}

	// a second ending
	cells = cellsOf(`
		0 - - - - - - - - - - - - - - -
		0 - - - - - - - - - - - - - - -
		0 - - - - - - - - - - - - - - -
		0 - - - - - - - - - - - - - - -
		[2
		0 - - - - - - - - - - - - - - -
		||`)

	text, _ = ABC(cells, meter44, "C", "ending", 1)
	if want := "|: C16 | C16 | C16 |1 C16 :|2 C16 |]\n"; !strings.HasSuffix(text, want) {
		t.Errorf("got\n%s", text)
	}

	// a note that goes on over a bar line is tied
	cells = cellsOf("0 - - - - - - - - - - - - - - - - - - - 3 - - - - - - - - - - - ||")
	text, _ = ABC(cells, meter44, "C", "tie", 1)

	if want := "|: C16- | C4 F12 :|\n"; !strings.HasSuffix(text, want) {
		t.Errorf("got\n%s", text)
	}

	for _, bad := range []string{"", "H", "x"} {
		if _, err := ABC(cells, meter44, bad, "t", 1); err == nil {
			t.Errorf("key %q was taken", bad)
		}
	}

	// the notes: octaves, up and down
	for _, c := range []struct {
		tonic, step int
		want        string
	}{{0, 0, "C"}, {0, 6, "B"}, {0, 7, "c"}, {0, 14, "c'"}, {0, -1, "B,"}, {0, -8, "B,,"}, {1, 0, "D"}, {1, 6, "c"}, {5, 2, "c"}} {
		if got := abcNote(c.tonic, c.step); got != c.want {
			t.Errorf("tonic %d step %d is %s, want %s", c.tonic, c.step, got, c.want)
		}
	}
}

// With the Nottingham Music Database, when it is there (NOTTINGHAM_DIR is where its ABC files are): the tunes that can be
// folded are read by the grammar, and what is learned from them makes tunes that read with the grammar too.
func TestNottinghamIsReadByTheGrammar(t *testing.T) {
	dir := os.Getenv("NOTTINGHAM_DIR")
	if dir == "" {
		t.Skip("NOTTINGHAM_DIR is not set")
	}

	c, err := LoadCorpus(dir)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("%d tunes, %d made into cells, left out: %v", c.Tunes, len(c.Pieces), c.Left)

	if len(c.Pieces) < 600 {
		t.Errorf("only %d tunes were made into cells", len(c.Pieces))
	}

	for _, meter := range []abc.Meter{meter44, {Num: 6, Den: 8}} {
		learned, err := LearnFolk(c, meter)
		if err != nil {
			t.Fatal(err)
		}

		t.Logf("%v: %d tunes read, %d shapes", meter, learned.Tunes, len(learned.Model.Shapes))

		if len(learned.Failed) > 0 {
			t.Errorf("%v: %d tunes the grammar could not read, the first: %s", meter, len(learned.Failed), learned.Failed[0])
		}

		if learned.Tunes < 200 {
			t.Errorf("%v: only %d tunes", meter, learned.Tunes)
		}

		g := FolkGrammar(NewFolkModel(meter))

		for seed := int64(0); seed < 30; seed++ {
			cells, err := ComposeFolk(learned.Model, seed)
			if err != nil {
				t.Fatal(err)
			}

			if _, _, err := parseFolk(g, &Piece{Meter: meter, Cells: cells}); err != nil {
				t.Fatalf("%v seed %d: %v", meter, seed, err)
			}
		}
	}
}

func TestStrengthOfAPlaceInTheBar(t *testing.T) {
	str := func(m abc.Meter) string {
		var out []byte

		for s := 0; s < slotsOf(m); s++ {
			out = append(out, byte('0'+strength(m, s)))
		}

		return string(out)
	}

	// 4/4: a beat every four sixteenths; 6/8: two beats of a dotted quarter; 3/4 and 2/2 by their own beats
	for m, want := range map[abc.Meter]string{
		{Num: 4, Den: 4}: "0323132313231323",
		{Num: 6, Den: 8}: "032323132323",
		{Num: 3, Den: 4}: "032313231323",
		{Num: 2, Den: 2}: "0323232313232323",
		{Num: 9, Den: 8}: "032323132323132323",
	} {
		if got := str(m); got != want {
			t.Errorf("%v: %s, want %s", m, got, want)
		}
	}
}

// A melody that has gone up goes on up, and one that has gone down goes on down: the step before is remembered.
func TestStepsRememberTheStepBefore(t *testing.T) {
	made := aModel(meter44)

	for str := 0; str < 4; str++ {
		for i := range made.Step.Motion[motionRow(1, str)] {
			made.Step.Motion[motionRow(1, str)][i] = 0
			made.Step.Motion[motionRow(-1, str)][i] = 0
		}

		made.Step.Motion[motionRow(1, str)][maxStep+1] = 1
		made.Step.Motion[motionRow(-1, str)][maxStep-1] = 1
	}

	same, all := 0, 0

	for _, p := range piecesOf(t, made, 100) {
		var steps []int

		last, has := 0, false

		for _, c := range p.Cells {
			if c.Kind == Onset {
				if has {
					steps = append(steps, c.Degree-last)
				}

				last, has = c.Degree, true
			}
		}

		for i := 1; i < len(steps); i++ {
			if steps[i-1] == 1 || steps[i-1] == -1 {
				all++

				if steps[i] == steps[i-1] {
					same++
				}
			}
		}
	}

	// the melody turns at the ends of its range, and where a phrase comes to rest: for the rest it goes on in its direction
	if share := float64(same) / float64(all); share < 0.8 {
		t.Errorf("a step is followed by the same step %.2f of the time, the model says always", share)
	}

	learned, err := LearnFolk(&Corpus{Pieces: piecesOf(t, made, 300)}, meter44)
	if err != nil {
		t.Fatal(err)
	}

	row := learned.Model.Step.Motion[motionRow(1, 2)]
	if row[maxStep+1] < 0.8 {
		t.Errorf("after a step up, another is %.2f likely", row[maxStep+1])
	}
}

func TestOnlyWhatIsWrittenHasNoRests(t *testing.T) {
	// rests at the end of a bar are there to read tunes with a pickup, and nothing generates them
	for _, p := range piecesOf(t, aModel(meter44), 200) {
		for _, c := range p.Cells {
			if c.Kind == Rest {
				t.Fatalf("seed %d has a rest", p.Ref)
			}
		}
	}
}

// shares says what part of the notes of some tunes have a length, of the steps are of a size, and of the notes are on a
// step of the scale.
type shares struct {
	lengths, steps, degrees map[int]float64
}

func normalized(m map[int]float64) map[int]float64 {
	total := 0.0
	for _, v := range m {
		total += v
	}

	out := map[int]float64{}
	for k, v := range m {
		out[k] = v / total
	}

	return out
}

func sharesOf(tunes [][]Cell) shares {
	lengths, steps, degrees := map[int]float64{}, map[int]float64{}, map[int]float64{}

	for _, cells := range tunes {
		run, last, has := 0, 0, false

		end := func() {
			if run > 0 {
				lengths[run]++
			}
		}

		for _, c := range cells {
			switch c.Kind {
			case Hold:
				run++
			case Onset, Rest:
				end()
				run = 1

				if c.Kind == Onset {
					if has {
						steps[min(max(c.Degree-last, -3), 3)]++
					}

					degrees[(c.Degree%7+7)%7]++
					last, has = c.Degree, true
				}
			default:
				end()
				run = 0
			}
		}
	}

	return shares{normalized(lengths), normalized(steps), normalized(degrees)}
}

// What is made is like what was learned from: the notes are as long, the steps as big and the notes the same.
func TestNottinghamTunesAreLikeTheOnesTheyWereLearnedFrom(t *testing.T) {
	dir := os.Getenv("NOTTINGHAM_DIR")
	if dir == "" {
		t.Skip("NOTTINGHAM_DIR is not set")
	}

	c, err := LoadCorpus(dir)
	if err != nil {
		t.Fatal(err)
	}

	for _, meter := range []abc.Meter{meter44, {Num: 6, Den: 8}} {
		learned, err := LearnFolk(c, meter)
		if err != nil {
			t.Fatal(err)
		}

		var real, made [][]Cell

		for _, p := range c.Pieces {
			if p.Meter == meter {
				real = append(real, p.Cells)
			}
		}

		for seed := int64(0); seed < 300; seed++ {
			cells, err := ComposeFolk(learned.Model, seed)
			if err != nil {
				t.Fatal(err)
			}

			made = append(made, cells)
		}

		r, m := sharesOf(real), sharesOf(made)

		check := func(what string, a, b map[int]float64, keys ...int) {
			for _, k := range keys {
				if d := a[k] - b[k]; d > 0.04 || d < -0.04 {
					t.Errorf("%v %s %d: %.3f in the tunes, %.3f in what is made", meter, what, k, a[k], b[k])
				}
			}
		}

		check("note length", r.lengths, m.lengths, 1, 2, 3, 4, 6, 8, 12)
		check("step", r.steps, m.steps, -3, -2, -1, 0, 1, 2, 3)

		check("degree", r.degrees, m.degrees, 0, 1, 2, 3, 4, 5, 6)
	}
}
