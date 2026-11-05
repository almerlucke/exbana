package abc

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// tune reads a tune with the header the test wants (the default: 4/4, quarter notes, C) and the music.
func tune(t *testing.T, header, music string) *Tune {
	t.Helper()

	h := map[string]string{"M": "4/4", "L": "1/4", "K": "C"}

	for _, f := range strings.Fields(header) {
		h[f[:1]] = f[2:]
	}

	text := fmt.Sprintf("X:1\nT:test\nM:%s\nL:%s\nK:%s\n%s\n", h["M"], h["L"], h["K"], music)
	if h["L"] == "none" {
		text = strings.Replace(text, "L:none\n", "", 1)
	}

	tu, err := ParseTune(text)
	if err != nil {
		t.Fatalf("%s: %v", music, err)
	}

	return tu
}

// events of the first bar, as degree/ticks, or r/ticks for a rest
func shape(es []Event) string {
	var parts []string

	for _, e := range es {
		if e.Rest {
			parts = append(parts, fmt.Sprintf("r/%d", e.Ticks))
		} else {
			parts = append(parts, fmt.Sprintf("%d/%d", e.Degree, e.Ticks))
		}
	}

	return strings.Join(parts, " ")
}

func TestKeys(t *testing.T) {
	for in, want := range map[string]Key{
		"C":       {"C", "major", 0},
		"D":       {"D", "major", 2},
		"Am":      {"A", "minor", 0},
		"Bb":      {"Bb", "major", -2},
		"F#m":     {"F#", "minor", 3},
		"Gdor":    {"G", "dorian", -1},
		"D mix":   {"D", "mixolydian", 1},
		"E min":   {"E", "minor", 1},
		"Ador":    {"A", "dorian", 1},
		"Bm":      {"B", "minor", 2},
		"G Major": {"G", "major", 1},
	} {
		got, err := parseKey(in)
		if err != nil || got != want {
			t.Errorf("K:%s is %v (%v), want %v", in, got, err, want)
		}
	}

	for _, bad := range []string{"", "H", "Gxyz", "C#####"} {
		if _, err := parseKey(bad); err == nil {
			t.Errorf("K:%q was read", bad)
		}
	}
}

func TestNotesAreStepsOfTheScale(t *testing.T) {
	// D major: D F# A d f#, with the F# of the key and no accidentals
	tu := tune(t, "K:D", "D F A d f")
	got := tu.Bars[0].Events

	wantDegree := []int{0, 2, 4, 7, 9}
	wantMIDI := []int{62, 66, 69, 74, 78}

	for i, e := range got {
		if e.Degree != wantDegree[i] || e.MIDI != wantMIDI[i] || e.Chroma != 0 {
			t.Errorf("note %d: %+v, want degree %d and MIDI %d", i, e, wantDegree[i], wantMIDI[i])
		}
	}

	// the same tune in G is the same steps
	if g := tune(t, "K:G", "G B d g b").Bars[0].Events; shape(g) != shape(got) {
		t.Errorf("in G: %s, in D: %s", shape(g), shape(got))
	}

	// octave marks: C, c, c', C, are four octaves apart, in steps
	oct := tune(t, "", "C, C c c'").Bars[0].Events
	if d := []int{oct[0].Degree, oct[1].Degree, oct[2].Degree, oct[3].Degree}; !reflect.DeepEqual(d, []int{-7, 0, 7, 14}) {
		t.Errorf("octaves are %v", d)
	}
}

func TestAccidentalsLastForTheBar(t *testing.T) {
	tu := tune(t, "K:C", "^F F =F F | F")

	var chroma []int
	for _, b := range tu.Bars {
		for _, e := range b.Events {
			chroma = append(chroma, e.Chroma)
		}
	}

	// sharp, still sharp, natural, still natural, and the next bar is the key again
	if want := []int{1, 1, 0, 0, 0}; !reflect.DeepEqual(chroma, want) {
		t.Errorf("chroma %v, want %v", chroma, want)
	}

	// in G, the F is sharp already: a natural is a flattening
	tu = tune(t, "K:G", "=F F | F")
	e := tu.Bars[0].Events

	if e[0].Chroma != -1 || e[1].Chroma != -1 || e[0].MIDI != 65 || tu.Bars[1].Events[0].Chroma != 0 || tu.Bars[1].Events[0].MIDI != 66 {
		t.Errorf("got %+v and %+v", e, tu.Bars[1].Events)
	}

	// an accidental is for the octave it is in
	tu = tune(t, "K:C", "^F f F")
	if c := tu.Bars[0].Events; c[0].Chroma != 1 || c[1].Chroma != 0 || c[2].Chroma != 1 {
		t.Errorf("got %+v", c)
	}
}

func TestLengths(t *testing.T) {
	tu := tune(t, "L:1/8 K:A", "A A2 A/2 A/ A3/2 A// z2")
	if got, want := shape(tu.Bars[0].Events), "0/12 0/24 0/6 0/6 0/18 0/3 r/24"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}

	// without L: an eighth, but a sixteenth in a meter shorter than 3/4
	if got := tune(t, "L:none", "A").Bars[0].Events[0].Ticks; got != 12 {
		t.Errorf("in 4/4 a note is %d ticks", got)
	}

	if got := tune(t, "L:none M:2/4", "A").Bars[0].Events[0].Ticks; got != 6 {
		t.Errorf("in 2/4 a note is %d ticks", got)
	}

	if got := tune(t, "M:6/8", "Z").Bars[0].Ticks; got != 72 {
		t.Errorf("a whole bar rest in 6/8 is %d ticks", got)
	}
}

func TestBrokenRhythmAndTriplets(t *testing.T) {
	for music, want := range map[string]string{
		"A>B":       "0/18 1/6",
		"A<B":       "0/6 1/18",
		"A>>B":      "0/21 1/3",
		"(3ABc":     "0/8 1/8 2/8",
		"(3:2:3ABc": "0/8 1/8 2/8",
		"(2AB":      "0/18 1/18",
		"(3A/B/c/":  "0/4 1/4 2/4",
		"(3ABc d":   "0/8 1/8 2/8 3/12",
	} {
		if got := shape(tune(t, "L:1/8 K:A", music).Bars[0].Events); got != want {
			t.Errorf("%s: got %s, want %s", music, got, want)
		}
	}

	// a bar is still full: broken rhythm gives and takes the same
	if tu := tune(t, "L:1/8", "A>B c<d e>f g<a"); tu.Bars[0].Ticks != 96 {
		t.Errorf("the bar is %d ticks", tu.Bars[0].Ticks)
	}
}

func TestChordsRestsAndThingsThatAreSkipped(t *testing.T) {
	tu := tune(t, "K:C", `"Am"[ceg]2 z !trill!B ~C .D {AB}E (F G) "x"H`+" x")
	// the chord is its top note, the rest and the decorated notes are read, grace notes and slurs are not in the way
	got := shape(tu.Bars[0].Events)

	// c e g is the g (degree 11, an octave plus a fourth... above C4: g is the 5th above c, 7+4); H is a decoration
	if want := "11/48 r/24 6/24 0/24 1/24 2/24 3/24 4/24 r/24"; got != want {
		t.Errorf("got %s\nwant %s", got, want)
	}
}

func TestTies(t *testing.T) {
	tu := tune(t, "", "A2-A2 | A-B A2 | A- | A")

	var tied []bool
	for _, b := range tu.Bars {
		for _, e := range b.Events {
			tied = append(tied, e.Tie)
		}
	}

	if want := []bool{true, false, true, false, false, true, false}; !reflect.DeepEqual(tied, want) {
		t.Errorf("ties %v, want %v", tied, want)
	}

	// the tied A2-A2 is one note; A tied to B is nothing to merge, and the A tied over the bar line to the next is one
	if got, want := shape(tu.Melody()), "5/96 5/24 6/24 5/48 5/48"; got != want {
		t.Errorf("melody %s, want %s", got, want)
	}

	tu = tune(t, "", "A2-A2 | A-A- | A4")
	if got, want := shape(tu.Melody()), "5/96 5/144"; got != want {
		t.Errorf("melody %s, want %s", got, want)
	}
}

func TestRepeatsAndEndings(t *testing.T) {
	// each bar is one note with its own name, so the order can be read
	play := func(music string) string {
		tu := tune(t, "", music)

		var out []string
		for _, b := range tu.Playthrough() {
			out = append(out, string("CDEFGAB"[b.Events[0].Degree]))
		}

		return strings.Join(out, "")
	}

	for music, want := range map[string]string{
		"C4 | D4 | E4":                    "CDE",
		"|: C4 | D4 :| E4":                "CDCDE",
		"C4 | D4 :| E4":                   "CDCDE",
		"|: C4 | D4 :| E4 | F4 :|":        "CDCDEFEF",
		"|: C4 |1 D4 :|2 E4 || F4":        "CDCEF",
		"|: C4 | D4 |[1 E4 :|[2 F4 || G4": "CDECDFG",
		"|: C4 | D4 |1 E4 | F4 :|2 G4 :|": "CDEFCDG",
		"|: C4 | D4 :: E4 | F4 :|":        "CDCDEFEF",
		"|: C4 | D4 :|: E4 | F4 :|]":      "CDCDEFEF",
		"|: C4 :| |: D4 :| |: E4 :|":      "CCDDEE",
		"C4 || |: D4 :|":                  "CDD",
		"|: C4 |1 D4 :|2 E4 :| F4":        "CDCEF",
	} {
		if got := play(music); got != want {
			t.Errorf("%s: played %s, want %s", music, got, want)
		}
	}
}

func TestKeyAndMeterChangeInTheTune(t *testing.T) {
	text := "X:1\nT:t\nM:4/4\nL:1/4\nK:D\nD F A d |\nK:G\nG B d g |\nM:3/4\nG B d |\n"

	tu, err := ParseTune(text)
	if err != nil {
		t.Fatal(err)
	}

	if len(tu.Bars) != 3 || tu.Bars[0].Key.Tonic != "D" || tu.Bars[1].Key.Tonic != "G" || tu.Bars[2].Meter != (Meter{3, 4}) {
		t.Fatalf("got %+v", tu.Bars)
	}

	// the same steps in either key
	if shape(tu.Bars[0].Events) != shape(tu.Bars[1].Events) {
		t.Errorf("%s and %s", shape(tu.Bars[0].Events), shape(tu.Bars[1].Events))
	}

	if bad := tu.Irregular(); len(bad) != 0 {
		t.Errorf("irregular bars %v", bad)
	}
}

func TestIrregularBars(t *testing.T) {
	cases := map[string]int{
		"C4 | D4 | E4":              0,
		"C | D4 | E4 | F4":          0, // a pickup
		"C4 | D3 | E4":              1, // a short bar in the middle
		"C4 | D5 | E4":              1, // a long one
		"|: C4 | D2 :|: E2 | F4 :|": 0, // a part ends short, and the next starts with a pickup
		"C4 | D4 | E2":              0, // the end of a bar
	}

	for music, want := range cases {
		if got := len(tune(t, "", music).Irregular()); got != want {
			t.Errorf("%s: %d irregular bars, want %d", music, got, want)
		}
	}
}

func TestWhatIsMusicAndWhatIsAField(t *testing.T) {
	// "G:|" is a note and a repeat, "P:A" is a field
	tu := tune(t, "", "P:A\nC D E F |\nG:|\nA B C D |")
	if len(tu.Bars) != 3 || !tu.Bars[1].RepeatEnd {
		t.Errorf("got %+v", tu.Bars)
	}

	for line, want := range map[string]bool{"K:D": true, "P:A": true, "T:x": true, "G:|": false, "A:|": false, "B::": false, "abc": false, "": false, "w:la": true} {
		if isField(line) != want {
			t.Errorf("isField(%q) is %v", line, !want)
		}
	}
}

func TestATuneThatCantBeReadDoesNotStopTheOthers(t *testing.T) {
	text := `X:1
T:fine
M:4/4
L:1/4
K:C
C D E F |

X:2
T:broken
M:4/4
L:1/4
K:C
C D & F |

X:3
T:no key
M:4/4
C D E F |

X:4
T:also fine
M:4/4
L:1/4
K:G
G B d g |
`

	tunes, errs := Read(strings.NewReader(text))
	if len(tunes) != 2 || tunes[0].Title != "fine" || tunes[1].Title != "also fine" {
		t.Errorf("tunes %v", tunes)
	}

	if len(errs) != 2 {
		t.Fatalf("errors %v", errs)
	}

	e := errs[0].(*Error)
	if e.Ref != 2 || e.Title != "broken" || e.Line != 13 || !strings.Contains(e.Error(), "'&'") {
		t.Errorf("got %v (%d)", e, e.Line)
	}

	if !strings.Contains(errs[1].Error(), "no K") {
		t.Errorf("got %v", errs[1])
	}
}

func TestBadMusicIsRefused(t *testing.T) {
	for _, music := range []string{`"C D`, "!trill C", "A99999999999", "(3:7557:5ABC", "(3:0ABC", "A/99999999999", "(3A1000000000000000000000", strings.Repeat("A", 1) + strings.Repeat(">", 70) + "A", "{AB C", "C [K:G] D", "[CE", "A3/0", "^", "\u00e4", ":", "C :"} {
		if _, err := ParseTune(fmt.Sprintf("X:1\nM:4/4\nL:1/4\nK:C\n%s\n", music)); err == nil {
			t.Errorf("%q was read", music)
		}
	}
}

// The Nottingham Music Database, when it is there: NOTTINGHAM_DIR is where its ABC files are.
func TestNottingham(t *testing.T) {
	dir := os.Getenv("NOTTINGHAM_DIR")
	if dir == "" {
		t.Skip("NOTTINGHAM_DIR is not set")
	}

	files, _ := filepath.Glob(filepath.Join(dir, "*.abc"))
	if len(files) == 0 {
		t.Fatalf("no ABC files in %s", dir)
	}

	var tunes []*Tune

	for _, name := range files {
		f, err := os.Open(name)
		if err != nil {
			t.Fatal(err)
		}

		ts, errs := Read(f)
		f.Close()

		for _, err := range errs {
			t.Log(err)
		}

		tunes = append(tunes, ts...)
	}

	irregular := 0
	for _, tu := range tunes {
		if len(tu.Irregular()) > 0 {
			irregular++
		}

		for _, b := range tu.Bars {
			sum := 0
			for _, e := range b.Events {
				sum += e.Ticks
			}

			if sum != b.Ticks {
				t.Fatalf("tune %d: bar of %d ticks has events of %d", tu.Ref, b.Ticks, sum)
			}
		}
	}

	t.Logf("%d tunes, %d with bars that don't fill their meter", len(tunes), irregular)

	// the database has 1034 tunes, of which all but a few are within the reach of the reader, and nearly all are whole
	if len(tunes) < 1025 {
		t.Errorf("only %d tunes were read", len(tunes))
	}

	if irregular > len(tunes)/10 {
		t.Errorf("%d of %d tunes have bars of the wrong length", irregular, len(tunes))
	}
}

func FuzzRead(f *testing.F) {
	for _, s := range []string{
		"X:1\nM:4/4\nL:1/8\nK:G\n|:\"G\"GABc d2 (3efg|1 a>b c'2:|2 [ce]z-z||\n",
		"X:1\nM:6/8\nK:Am\nA3 B3|c3 d3:|\n",
		"X:2\nK:D\n[1 A [2 B ::\n",
	} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		tunes, _ := Read(strings.NewReader(s))

		for _, tu := range tunes {
			for _, b := range tu.Bars {
				sum := 0
				for _, e := range b.Events {
					sum += e.Ticks
					if e.Ticks <= 0 {
						t.Fatalf("an event of %d ticks", e.Ticks)
					}
				}

				if sum != b.Ticks {
					t.Fatalf("a bar of %d ticks has events of %d", b.Ticks, sum)
				}
			}

			// these must not panic, and must play no more than twice what is written
			_ = tu.Irregular()
			_ = tu.Melody()

			if n := len(tu.Playthrough()); n > 2*len(tu.Bars) {
				t.Fatalf("%d bars played from %d", n, len(tu.Bars))
			}
		}
	})
}

func TestSignatures(t *testing.T) {
	// the sharps come in the order F C G D A E B, and the flats in the opposite order
	sharps, flats := "FCGDAEB", "BEADGCF"

	for n := -7; n <= 7; n++ {
		sig := signature(n)

		for i, l := range letters {
			want := 0

			if k := strings.IndexRune(sharps, l); n > 0 && k < n {
				want = 1
			}

			if k := strings.IndexRune(flats, l); n < 0 && k < -n {
				want = -1
			}

			if sig[i] != want {
				t.Errorf("%d sharps: %c is %d, want %d", n, l, sig[i], want)
			}
		}
	}
}

func TestLowerCaseNotesAreAnOctaveUp(t *testing.T) {
	for i, l := range "abcdefg" {
		low := strings.IndexRune(letters, l-32)
		got := tune(t, "", string(l)).Bars[0].Events[0]

		if got.Degree != low+7 || got.MIDI != 72+semitones[low] {
			t.Errorf("%c is %+v", l, got)
		}

		_ = i
	}
}

func TestEndingsEndAtDoubleBars(t *testing.T) {
	endings := func(music string) []int {
		var out []int
		for _, b := range tune(t, "", music).Bars {
			out = append(out, b.Ending)
		}

		return out
	}

	for music, want := range map[string][]int{
		"|: C4 |1 D4 :|2 E4 || F4":      {0, 1, 2, 0},
		"|: C4 |1 D4 :|2 E4 |] F4":      {0, 1, 2, 0},
		"|: C4 |1 D4 | E4 :|2 F4 | G4":  {0, 1, 1, 2, 2},
		"|: C4 | D4 |1 E4 :|2 F4 :| G4": {0, 0, 1, 2, 0},
	} {
		if got := endings(music); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: endings %v, want %v", music, got, want)
		}
	}
}
