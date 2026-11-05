// Package abc reads tunes written in ABC notation, the text format that folk tune collections like the Nottingham
// Music Database come in, and turns them into notes that are ready to learn from.
//
// Every note comes out as a step of the scale of the key (a Degree, counted from the tonic), with how far it is off the
// scale (Chroma), the pitch it sounds (MIDI) and its length in Ticks. That makes tunes in every key comparable:
// "step up, step up, leap down" is the same in D major and in G major.
//
// It reads the part of ABC that melodies are made of: notes with accidentals, octave marks and lengths, rests, ties,
// broken rhythm (a>b), triplets, bar lines with repeats and first and second endings, and changes of key, meter and
// unit length in the middle of a tune. Chord symbols, decorations, grace notes, slurs, lyrics and other header fields
// are skipped. What it does not understand is an error for that tune, and does not stop the others from being read.
package abc

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// maxFactor is the most a note length, a meter or a unit length can multiply or divide by. Nothing in a tune comes near
// it, and it keeps the arithmetic of ticks from overflowing.
const maxFactor = 1 << 10

// Lengths are counted in ticks, so that a triplet of eighths and a sixteenth are whole numbers.
const (
	TicksPerQuarter = 24
	TicksPerWhole   = 4 * TicksPerQuarter
)

// Event is a note or a rest.
type Event struct {
	Rest   bool
	Degree int  // steps of the scale, from the tonic in the octave of middle C; 7 is the tonic an octave up
	Chroma int  // semitones from the note the key gives: 0 for a note in the scale, 1 for a sharpened one
	MIDI   int  // the pitch that sounds, 60 is middle C
	Ticks  int  // the length
	Tie    bool // tied to the event that follows: they are one note
}

// Meter is the time signature.
type Meter struct{ Num, Den int }

// Ticks is the length of a bar.
func (m Meter) Ticks() int { return m.Num * TicksPerWhole / m.Den }

// Key is the key signature.
type Key struct {
	Tonic  string // A to G, with # or b
	Mode   string // major, minor, dorian, phrygian, lydian, mixolydian or locrian
	Fifths int    // sharps (positive) or flats (negative) in the signature
}

func (k Key) String() string { return k.Tonic + " " + k.Mode }

// Bar is the events between two bar lines.
type Bar struct {
	Events      []Event
	Ticks       int // how much the events fill
	Meter       Meter
	Key         Key
	RepeatStart bool // the bar starts a repeat, |:
	RepeatEnd   bool // the bar ends a repeat, :|
	Ending      int  // 1 or 2 for a bar in a first or second ending, 0 for a bar in neither
}

// Tune is a tune as it is written.
type Tune struct {
	Ref   int // the number of the X: field
	Title string
	Bars  []Bar
}

// Error is why a tune could not be read.
type Error struct {
	Ref   int
	Title string
	Line  int // the line in the input, 0 if it is not about one line
	Err   error
}

func (e *Error) Error() string {
	where := fmt.Sprintf("tune %d", e.Ref)
	if e.Title != "" {
		where += fmt.Sprintf(" (%s)", e.Title)
	}

	if e.Line > 0 {
		where += fmt.Sprintf(", line %d", e.Line)
	}

	return where + ": " + e.Err.Error()
}

func (e *Error) Unwrap() error { return e.Err }

// Read reads all tunes from a file. A tune that can't be read is an error and the other tunes are still read.
func Read(r io.Reader) ([]*Tune, []error) {
	var (
		tunes []*Tune
		errs  []error
		block []string
		first int
		line  int
	)

	flush := func() {
		if len(block) == 0 {
			return
		}

		t, err := parseTune(block, first)
		if err != nil {
			errs = append(errs, err)
		} else {
			tunes = append(tunes, t)
		}

		block = nil
	}

	sc := bufio.NewScanner(r)
	sc.Buffer(nil, 1<<20)

	for sc.Scan() {
		line++
		text := sc.Text()

		if strings.HasPrefix(text, "X:") {
			flush()
			first = line
		}

		if len(block) > 0 || strings.HasPrefix(text, "X:") {
			block = append(block, text)
		}
	}

	flush()

	if err := sc.Err(); err != nil {
		errs = append(errs, err)
	}

	return tunes, errs
}

// ParseTune reads a single tune.
func ParseTune(text string) (*Tune, error) {
	tunes, errs := Read(strings.NewReader(text))
	if len(errs) > 0 {
		return nil, errs[0]
	}

	if len(tunes) != 1 {
		return nil, fmt.Errorf("abc: expected one tune starting with X:, found %d", len(tunes))
	}

	return tunes[0], nil
}

// isField is true for a line like "K:D", and not for music like "G:|".
func isField(line string) bool {
	if len(line) < 2 || line[1] != ':' || !(line[0] >= 'A' && line[0] <= 'Z' || line[0] >= 'a' && line[0] <= 'z') {
		return false
	}

	return len(line) == 2 || !strings.ContainsRune("|:]", rune(line[2]))
}

func parseTune(lines []string, firstLine int) (*Tune, error) {
	t := &Tune{}
	p := &parser{tune: t, unit: -1}

	fail := func(line int, err error) error {
		return &Error{Ref: t.Ref, Title: t.Title, Line: firstLine + line, Err: err}
	}

	haveKey := false

	for i, line := range lines {
		if i := strings.Index(line, "%"); i == 0 {
			continue
		}

		line = strings.TrimRight(line, " \t\r")

		if isField(line) {
			value := strings.TrimSpace(line[2:])

			switch line[0] {
			case 'X':
				n, err := strconv.Atoi(value)
				if err != nil {
					return nil, fail(i, fmt.Errorf("X: %q is not a number", value))
				}

				t.Ref = n
			case 'T':
				if t.Title == "" {
					t.Title = value
				}
			case 'M':
				m, err := parseMeter(value)
				if err != nil {
					return nil, fail(i, err)
				}

				p.setMeter(m)
			case 'L':
				u, err := parseUnit(value)
				if err != nil {
					return nil, fail(i, err)
				}

				p.unit = u
			case 'K':
				k, err := parseKey(value)
				if err != nil {
					return nil, fail(i, err)
				}

				p.setKey(k)
				haveKey = true
			}

			continue
		}

		if !haveKey {
			continue // the header, up to K:, has nothing to play
		}

		if err := p.music(line); err != nil {
			return nil, fail(i, err)
		}
	}

	if !haveKey {
		return nil, fail(0, fmt.Errorf("no K: field, so no key"))
	}

	p.endBar(false)

	if len(t.Bars) == 0 {
		return nil, fail(0, fmt.Errorf("no notes"))
	}

	return t, nil
}

func parseMeter(s string) (Meter, error) {
	switch s {
	case "C":
		return Meter{4, 4}, nil
	case "C|":
		return Meter{2, 2}, nil
	}

	num, den, ok := strings.Cut(s, "/")
	n, err1 := strconv.Atoi(num)
	d, err2 := strconv.Atoi(den)

	if !ok || err1 != nil || err2 != nil || n <= 0 || d <= 0 || n > maxFactor || d > maxFactor || TicksPerWhole%d != 0 {
		return Meter{}, fmt.Errorf("M: %q is not a meter like 4/4", s)
	}

	return Meter{n, d}, nil
}

func parseUnit(s string) (int, error) {
	num, den, ok := strings.Cut(s, "/")
	n, err1 := strconv.Atoi(num)
	d, err2 := strconv.Atoi(den)

	if !ok || err1 != nil || err2 != nil || n <= 0 || d <= 0 || n > maxFactor || d > maxFactor || n*TicksPerWhole%d != 0 {
		return 0, fmt.Errorf("L: %q is not a note length like 1/8", s)
	}

	return n * TicksPerWhole / d, nil
}

// the notes of the scale, counted from C
const letters = "CDEFGAB"

var semitones = [7]int{0, 2, 4, 5, 7, 9, 11}

// fifthsOf is how far a major key on a note is round the circle of fifths: C is 0, G is 1, F is -1.
var fifthsOf = map[byte]int{'C': 0, 'G': 1, 'D': 2, 'A': 3, 'E': 4, 'B': 5, 'F': -1}

var modes = []struct {
	prefix string
	name   string
	fifths int
}{
	{"maj", "major", 0}, {"ion", "major", 0},
	{"mix", "mixolydian", -1}, {"dor", "dorian", -2},
	{"min", "minor", -3}, {"aeo", "minor", -3}, {"m", "minor", -3},
	{"phr", "phrygian", -4}, {"lyd", "lydian", 1}, {"loc", "locrian", -5},
}

func parseKey(s string) (Key, error) {
	orig := s
	s = strings.TrimSpace(s)

	if s == "" || s[0] < 'A' || s[0] > 'G' {
		return Key{}, fmt.Errorf("K: %q is not a key like D or Gm", orig)
	}

	k := Key{Tonic: s[:1], Mode: "major"}
	fifths := fifthsOf[s[0]]
	s = s[1:]

	if s != "" && (s[0] == '#' || s[0] == 'b') {
		k.Tonic += s[:1]

		if s[0] == '#' {
			fifths += 7
		} else {
			fifths -= 7
		}

		s = s[1:]
	}

	s = strings.ToLower(strings.TrimSpace(s))

	if s != "" {
		found := false

		for _, m := range modes {
			if strings.HasPrefix(s, m.prefix) {
				k.Mode, fifths, found = m.name, fifths+m.fifths, true
				break
			}
		}

		if !found {
			return Key{}, fmt.Errorf("K: %q is not a key like D or Gm", orig)
		}
	}

	if fifths < -7 || fifths > 7 {
		return Key{}, fmt.Errorf("K: %q needs more than 7 sharps or flats", orig)
	}

	k.Fifths = fifths

	return k, nil
}

// signature is the sharps and flats of a key, for every note from C to B.
func signature(fifths int) [7]int {
	var sig [7]int

	sharps, flats := "FCGDAEB", "BEADGCF"

	for i := 0; i < fifths; i++ {
		sig[strings.IndexByte(letters, sharps[i])] = 1
	}

	for i := 0; i < -fifths; i++ {
		sig[strings.IndexByte(letters, flats[i])] = -1
	}

	return sig
}

type parser struct {
	tune *Tune

	meter Meter
	unit  int // ticks of a note without a length, -1 if the tune doesn't say
	key   Key
	sig   [7]int
	tonic int // the number of the tonic's letter, from C

	// the bar that is being read
	cur       []Event
	curTicks  int
	accidents map[[2]int]int // what notes were altered by in this bar, by letter and octave
	flags     struct {
		start  bool
		ending int
	}
	ending int // the ending the bars belong to

	// what comes with the next note
	tuplet     struct{ count, num, den int }
	brokenNext [2]int // the length of the next note as a fraction, 0 if it is as written
}

func (p *parser) setMeter(m Meter) {
	p.meter = m
}

func (p *parser) setKey(k Key) {
	p.key = k
	p.sig = signature(k.Fifths)
	p.tonic = strings.IndexByte(letters, k.Tonic[0])
}

func (p *parser) unitTicks() int {
	if p.unit > 0 {
		return p.unit
	}

	// the default of ABC: a sixteenth in meters under 3/4 and an eighth in the others
	if p.meter.Num*4 < p.meter.Den*3 {
		return TicksPerWhole / 16
	}

	return TicksPerWhole / 8
}

// endBar closes the bar that is being read. A bar without events is not a bar: what it carries goes on to the next.
func (p *parser) endBar(repeatEnd bool) {
	if len(p.cur) == 0 {
		return
	}

	if p.flags.ending != 0 {
		p.ending = p.flags.ending
	}

	b := Bar{
		Events: p.cur, Ticks: p.curTicks, Meter: p.meter, Key: p.key,
		RepeatStart: p.flags.start, RepeatEnd: repeatEnd, Ending: p.ending,
	}

	p.tune.Bars = append(p.tune.Bars, b)

	if p.flags.start {
		p.ending = 0
	}

	p.cur, p.curTicks, p.accidents = nil, 0, nil
	p.flags.start, p.flags.ending = false, 0
}

// music reads a line of notes.
func (p *parser) music(s string) error {
	if p.meter.Num == 0 {
		return fmt.Errorf("no M: field, so no meter")
	}

	for i := 0; i < len(s); {
		c := s[i]

		switch {
		case c == ' ' || c == '\t' || c == '\\' || c == '\r':
			i++
		case c == '%':
			return nil
		case c == '"': // a chord symbol
			end := strings.IndexByte(s[i+1:], '"')
			if end < 0 {
				return fmt.Errorf("the quote at column %d is not closed", i+1)
			}

			i += end + 2
		case c == '!' || c == '+': // a decoration
			end := strings.IndexByte(s[i+1:], c)
			if end < 0 {
				return fmt.Errorf("the %c at column %d is not closed", c, i+1)
			}

			i += end + 2
		case c == '{': // grace notes
			end := strings.IndexByte(s[i:], '}')
			if end < 0 {
				return fmt.Errorf("the { at column %d is not closed", i+1)
			}

			i += end + 1
		case strings.IndexByte("~.HLMOPSTuv)", c) >= 0:
			i++
		case c == '(':
			if i+1 < len(s) && s[i+1] >= '2' && s[i+1] <= '9' {
				n, err := p.startTuplet(s[i+1:])
				if err != nil {
					return err
				}

				i += n
			} else {
				i++ // a slur
			}
		case c == '-':
			if n := len(p.cur); n > 0 && !p.cur[n-1].Rest {
				p.cur[n-1].Tie = true
			} else if m := len(p.tune.Bars); n == 0 && m > 0 {
				if last := &p.tune.Bars[m-1]; !last.Events[len(last.Events)-1].Rest {
					last.Events[len(last.Events)-1].Tie = true
				}
			}

			i++
		case c == '>' || c == '<':
			n, err := p.broken(s[i:])
			if err != nil {
				return err
			}

			i += n
		case c == '[' && i+1 < len(s) && strings.IndexByte("^_=ABCDEFGabcdefgzx+", s[i+1]) >= 0:
			n, err := p.note(s[i:])
			if err != nil {
				return err
			}

			i += n
		case c == '|' || c == ':' || c == '[':
			n, err := p.barLine(s[i:])
			if err != nil {
				return err
			}

			i += n
		case strings.IndexByte("^_=ABCDEFGabcdefgzxZ", c) >= 0:
			n, err := p.note(s[i:])
			if err != nil {
				return err
			}

			i += n
		default:
			return fmt.Errorf("don't know what %q is, at column %d", c, i+1)
		}
	}

	return nil
}

func (p *parser) startTuplet(s string) (int, error) {
	// (p, (p:q or (p:q:r
	n := 1
	count := int(s[0] - '0')
	q := 0

	if len(s) > n && s[n] == ':' {
		n++
		start := n

		for n < len(s) && s[n] >= '0' && s[n] <= '9' {
			n++
		}

		if n > start {
			var err error
			if q, err = strconv.Atoi(s[start:n]); err != nil || q <= 0 || q > maxFactor {
				return 0, fmt.Errorf("a tuplet of %s notes in the time of %q", string(s[0]), s[start:n])
			}
		}

		if n < len(s) && s[n] == ':' { // r, the number of notes it covers, is count
			n++

			for n < len(s) && s[n] >= '0' && s[n] <= '9' {
				n++
			}
		}
	}

	if q == 0 {
		// the default of ABC: as many notes as fit in the time of 2 (or 3, in compound meter) of them
		switch count {
		case 2:
			q = 3
		case 3:
			q = 2
		case 4:
			q = 3
		case 6:
			q = 2
		case 8:
			q = 3
		default:
			q = 2
			if p.meter.Num%3 == 0 && p.meter.Num > 3 {
				q = 3
			}
		}
	}

	p.tuplet.count, p.tuplet.num, p.tuplet.den = count, q, count

	return n + 1, nil
}

// broken is a>b and a<b: one note of the pair is longer by half and the other shorter by as much.
func (p *parser) broken(s string) (int, error) {
	n := 1
	for n < len(s) && s[n] == s[0] {
		n++
	}

	if n > 3 {
		return 0, fmt.Errorf("a broken rhythm of more than three %c", s[0])
	}

	if len(p.cur) == 0 {
		return 0, fmt.Errorf("a broken rhythm (%c) needs a note before it in the bar", s[0])
	}

	// the note before is changed now, the note after when it comes
	pow := 1 << n
	long, short := 2*pow-1, 1

	prev := &p.cur[len(p.cur)-1]
	if s[0] == '>' {
		if err := p.stretch(prev, long, pow); err != nil {
			return 0, err
		}

		p.brokenNext = [2]int{short, pow}
	} else {
		if err := p.stretch(prev, short, pow); err != nil {
			return 0, err
		}

		p.brokenNext = [2]int{long, pow}
	}

	return n, nil
}

func (p *parser) stretch(e *Event, num, den int) error {
	t := e.Ticks * num
	if t%den != 0 {
		return fmt.Errorf("a note of %d ticks can't be made %d/%d as long", e.Ticks, num, den)
	}

	p.curTicks += t/den - e.Ticks
	e.Ticks = t / den

	return nil
}

// barLine reads a bar line: | || |] |: :| :|: :: and the number of an ending, |1 :|2 [1 [2
func (p *parser) barLine(s string) (int, error) {
	i := 0

	if s[0] == '[' {
		switch {
		case len(s) > 1 && s[1] >= '1' && s[1] <= '9':
			p.flags.ending = int(s[1] - '0')
			return 2, nil
		case len(s) > 1 && s[1] == '|':
			i = 1
		default:
			return 0, fmt.Errorf("[ is a chord or a field, which are not read")
		}
	}

	start := i
	for i < len(s) && (s[i] == '|' || s[i] == ':') {
		i++
	}

	tok := s[start:i]
	if i < len(s) && s[i] == ']' && strings.HasSuffix(tok, "|") {
		i++
	}

	if tok == "" || tok == ":" {
		return 0, fmt.Errorf("a lonely : is not a bar line")
	}

	end, begin := strings.HasPrefix(tok, ":"), strings.HasSuffix(tok, ":")

	p.endBar(end)

	// a bar line that ends a repeat belongs to the bar before it, even if that bar has been closed by an earlier line
	if end && len(p.tune.Bars) > 0 {
		p.tune.Bars[len(p.tune.Bars)-1].RepeatEnd = true
	}

	if end || strings.Contains(tok, "||") || strings.Contains(s[start:i], "|]") {
		p.ending = 0
	}

	if begin {
		p.flags.start = true
	}

	p.accidents = nil
	p.brokenNext = [2]int{}

	if i < len(s) && s[i] >= '1' && s[i] <= '9' {
		p.flags.ending = int(s[i] - '0')
		i++
	}

	return i, nil
}

// note reads a note or a rest, with its accidental, octave and length, or a chord, of which the top note is the melody.
func (p *parser) note(s string) (int, error) {
	if s[0] == '[' {
		return p.chord(s)
	}

	e, i, err := p.pitch(s)
	if err != nil {
		return 0, err
	}

	n, err := p.length(&e, s[i:])
	if err != nil {
		return 0, err
	}

	return i + n, p.commit(e)
}

// chord reads [CEG]2: the notes sound together, and the tune is the highest of them.
func (p *parser) chord(s string) (int, error) {
	i := 1

	var top Event

	have := false

	for i < len(s) && s[i] != ']' {
		if s[i] == ' ' || s[i] == '+' {
			i++
			continue
		}

		e, n, err := p.pitch(s[i:])
		if err != nil {
			return 0, err
		}

		// a length inside the chord is the length of the note
		m, err := p.length(&e, s[i+n:])
		if err != nil {
			return 0, err
		}

		i += n + m

		// a tie in a chord is the tie of the note before it
		if i < len(s) && s[i] == '-' {
			e.Tie = !e.Rest
			i++
		}

		if !have || !e.Rest && (top.Rest || e.MIDI > top.MIDI) {
			top, have = e, true
		}
	}

	if i >= len(s) || !have {
		return 0, fmt.Errorf("the chord is not closed or has no notes")
	}

	// a length after the chord goes for all of its notes
	n, err := p.length(&top, s[i+1:])
	if err != nil {
		return 0, err
	}

	return i + 1 + n, p.commit(top)
}

// pitch reads a note or a rest with its accidental and octave, and gives it the length of a unit.
func (p *parser) pitch(s string) (Event, int, error) {
	i := 0
	alter, explicit := 0, false

	for i < len(s) && (s[i] == '^' || s[i] == '_' || s[i] == '=') {
		explicit = true

		switch s[i] {
		case '^':
			alter++
		case '_':
			alter--
		}

		i++
	}

	if i >= len(s) {
		return Event{}, 0, fmt.Errorf("an accidental without a note")
	}

	c := s[i]
	i++

	e := Event{Rest: c == 'z' || c == 'x' || c == 'Z', Ticks: p.unitTicks()}
	if c == 'Z' {
		e.Ticks = p.meter.Ticks()
	}

	if e.Rest {
		return e, i, nil
	}

	letter := strings.IndexByte(letters, c&^0x20)
	if letter < 0 {
		return Event{}, 0, fmt.Errorf("%q is not a note", c)
	}

	octave := 4
	if c >= 'a' {
		octave = 5
	}

	for i < len(s) && (s[i] == '\'' || s[i] == ',') {
		if s[i] == '\'' {
			octave++
		} else {
			octave--
		}

		i++
	}

	key := [2]int{letter, octave}

	sounding := p.sig[letter]
	if explicit {
		sounding = alter
		if p.accidents == nil {
			p.accidents = map[[2]int]int{}
		}

		p.accidents[key] = alter
	} else if a, ok := p.accidents[key]; ok {
		sounding = a
	}

	e.Degree = letter + 7*octave - (p.tonic + 7*4)
	e.Chroma = sounding - p.sig[letter]
	e.MIDI = 12*(octave+1) + semitones[letter] + sounding

	return e, i, nil
}

// length reads a length that follows a note: 2, /2, /, //, 3/2. It scales the length the event has, and says how much
// of s it read.
func (p *parser) length(e *Event, s string) (int, error) {
	i := 0
	num, den := 1, 1

	digits := func() (int, bool) {
		start := i
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}

		n, _ := strconv.Atoi(s[start:i])

		return n, i > start
	}

	if n, ok := digits(); ok {
		num = n
	}

	if i < len(s) && s[i] == '/' {
		slashes := 0
		for i < len(s) && s[i] == '/' {
			slashes++
			i++
		}

		if n, ok := digits(); ok && slashes == 1 {
			den = n
		} else {
			den = 1 << slashes
		}
	}

	if num <= 0 || den <= 0 || num > maxFactor || den > maxFactor {
		return 0, fmt.Errorf("a note length of %d/%d", num, den)
	}

	ticks := e.Ticks * num
	if ticks%den != 0 {
		return 0, fmt.Errorf("a note length of %d/%d is not a whole number of ticks", num, den)
	}

	e.Ticks = ticks / den

	return i, nil
}

// commit puts a note in the bar, as a part of a triplet or the second of a broken rhythm if it is.
func (p *parser) commit(e Event) error {
	scale := func(num, den int) error {
		t := e.Ticks * num
		if t%den != 0 {
			return fmt.Errorf("a note of %d ticks can't be made %d/%d as long", e.Ticks, num, den)
		}

		e.Ticks = t / den

		return nil
	}

	if p.tuplet.count > 0 {
		if err := scale(p.tuplet.num, p.tuplet.den); err != nil {
			return err
		}

		p.tuplet.count--
	}

	if p.brokenNext[1] != 0 {
		if err := scale(p.brokenNext[0], p.brokenNext[1]); err != nil {
			return err
		}

		p.brokenNext = [2]int{}
	}

	p.cur = append(p.cur, e)
	p.curTicks += e.Ticks

	return nil
}
