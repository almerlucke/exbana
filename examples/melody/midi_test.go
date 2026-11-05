package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"reflect"
	"testing"

	"github.com/almerlucke/exbana/v3/examples/melody/abc"
)

// decoded is what a MIDI file says, read by a decoder that has nothing to do with the writer.
type decoded struct {
	format, tracks, division int
	name                     string
	tempo                    int // microseconds a quarter note
	meter                    [2]int
	key                      int8
	program                  int
	notes                    []midiNote
	end                      int // the tick of the end of the track
}

type midiNote struct{ at, pitch, length, velocity int }

func readVarLen(b []byte) (v int, n int) {
	for {
		c := b[n]
		n++
		v = v<<7 | int(c&0x7F)

		if c&0x80 == 0 {
			return
		}
	}
}

func decode(t *testing.T, data []byte) decoded {
	t.Helper()

	var d decoded

	if len(data) < 22 || string(data[:4]) != "MThd" || binary.BigEndian.Uint32(data[4:8]) != 6 {
		t.Fatalf("not a MIDI file: % x", data[:min(len(data), 16)])
	}

	d.format, d.tracks, d.division = int(binary.BigEndian.Uint16(data[8:])), int(binary.BigEndian.Uint16(data[10:])), int(binary.BigEndian.Uint16(data[12:]))

	if string(data[14:18]) != "MTrk" {
		t.Fatalf("no track: % x", data[14:18])
	}

	size := int(binary.BigEndian.Uint32(data[18:]))
	track := data[22:]

	if size != len(track) {
		t.Fatalf("the track says %d bytes, there are %d", size, len(track))
	}

	var (
		clock   int
		running byte
		open    = map[int]*midiNote{}
	)

	for i := 0; i < len(track); {
		delta, n := readVarLen(track[i:])
		i += n
		clock += delta

		status := track[i]

		if status == 0xFF {
			kind := track[i+1]
			length, n := readVarLen(track[i+2:])
			body := track[i+2+n : i+2+n+length]
			i += 2 + n + length

			switch kind {
			case 0x03:
				d.name = string(body)
			case 0x51:
				d.tempo = int(body[0])<<16 | int(body[1])<<8 | int(body[2])
			case 0x58:
				d.meter = [2]int{int(body[0]), 1 << body[1]}
			case 0x59:
				d.key = int8(body[0])
			case 0x2F:
				d.end = clock

				if i != len(track) {
					t.Fatalf("%d bytes after the end of the track", len(track)-i)
				}

				return d
			}

			continue
		}

		if status&0x80 != 0 {
			running = status
			i++
		}

		switch running & 0xF0 {
		case 0xC0:
			d.program = int(track[i])
			i++
		case 0x90:
			pitch, velocity := int(track[i]), int(track[i+1])
			i += 2

			if velocity == 0 {
				note := open[pitch]
				note.length = clock - note.at
				delete(open, pitch)

				break
			}

			if len(open) > 0 {
				t.Fatalf("a note starts while another sounds, at tick %d", clock)
			}

			d.notes = append(d.notes, midiNote{at: clock, pitch: pitch, velocity: velocity})
			open[pitch] = &d.notes[len(d.notes)-1]
		case 0x80:
			pitch := int(track[i])
			i += 2

			note := open[pitch]
			if note == nil {
				t.Fatalf("a note ends that did not start, at tick %d", clock)
			}

			note.length = clock - note.at
			delete(open, pitch)
		default:
			t.Fatalf("event %#x at byte %d", status, i)
		}
	}

	t.Fatal("the track does not end")

	return d
}

func TestVariableLengthNumbers(t *testing.T) {
	for v, want := range map[int][]byte{
		0: {0}, 1: {1}, 127: {0x7F}, 128: {0x81, 0x00}, 8192: {0xC0, 0x00}, 16383: {0xFF, 0x7F}, 16384: {0x81, 0x80, 0x00},
		0x0FFFFFFF: {0xFF, 0xFF, 0xFF, 0x7F},
	} {
		if got := varLen(v); !bytes.Equal(got, want) {
			t.Errorf("%d is % x, want % x", v, got, want)
		}

		if back, n := readVarLen(want); back != v || n != len(want) {
			t.Errorf("reading % x: %d", want, back)
		}
	}
}

func TestKeysAndPitches(t *testing.T) {
	for key, want := range map[string][2]int{
		"C": {60, 0}, "D": {62, 2}, "G": {67, 1}, "A": {69, 3}, "F": {65, -1}, "Bb": {70, -2}, "Eb": {63, -3}, "F#": {66, 6}, "B": {71, 5},
	} {
		tonic, fifths, err := keyOf(key)
		if err != nil || tonic != want[0] || fifths != want[1] {
			t.Errorf("%s: %d, %d (%v), want %v", key, tonic, fifths, err, want)
		}
	}

	for _, bad := range []string{"", "H", "Dx", "D#m", "G#", "Fb"} {
		if _, _, err := keyOf(bad); err == nil && bad != "Fb" {
			t.Errorf("%q was taken", bad)
		}
	}

	// D major: the tonic, the scale up, an octave, and below the tonic
	for step, want := range map[int]int{0: 62, 1: 64, 2: 66, 3: 67, 4: 69, 5: 71, 6: 73, 7: 74, 14: 86, -1: 61, -3: 57, -7: 50} {
		if got := pitchOf(62, step); got != want {
			t.Errorf("step %d is %d, want %d", step, got, want)
		}
	}
}

func TestMIDIOfATune(t *testing.T) {
	// a part of four bars: quarter notes up the tonic chord, and then an eighth and a half note
	bar := "0 - - - 2 - - - 4 - - - 7 - - -"
	cells := cellsOf(bar + " " + bar + " " + bar + " 0 - 2 - 4 - - - - - - - - - - - ||")

	data, err := MIDI(cells, meter44, "D", "a tune", 100, true)
	if err != nil {
		t.Fatal(err)
	}

	d := decode(t, data)

	if d.format != 0 || d.tracks != 1 || d.division != 480 || d.name != "a tune" || d.meter != [2]int{4, 4} || d.key != 2 || d.program != 40 {
		t.Errorf("the header: %+v", d)
	}

	if d.tempo != 600000 { // a quarter at 100 a minute
		t.Errorf("the tempo is %d µs a quarter", d.tempo)
	}

	// the part is played twice
	if len(d.notes) != 2*(3*4+3) {
		t.Fatalf("%d notes, want %d", len(d.notes), 2*(3*4+3))
	}

	first := d.notes[:15]
	for i, want := range []midiNote{
		{0, 62, 480, 92}, {480, 66, 480, 84}, {960, 69, 480, 84}, {1440, 74, 480, 84}, // D F# A d
		{1920, 62, 480, 92},
	} {
		if first[i] != want {
			t.Errorf("note %d: %+v, want %+v", i, first[i], want)
		}
	}

	// the last bar: an eighth, an eighth, a dotted half
	if got := first[12:]; !reflect.DeepEqual(got, []midiNote{{5760, 62, 240, 92}, {6000, 66, 240, 76}, {6240, 69, 1440, 84}}) {
		t.Errorf("the last bar: %+v", got)
	}

	// and again from the start of the part, four bars later
	for i := range first {
		if again := d.notes[15+i]; again.at != first[i].at+7680 || again.pitch != first[i].pitch || again.length != first[i].length {
			t.Errorf("the repeat of note %d: %+v", i, again)
		}
	}

	if d.end != 15360 { // 8 bars of 1920
		t.Errorf("the track ends at %d", d.end)
	}

	// without the repeat, once
	data, _ = MIDI(cells, meter44, "D", "a tune", 100, false)

	if d := decode(t, data); len(d.notes) != 15 || d.end != 7680 {
		t.Errorf("without repeats: %d notes, end %d", len(d.notes), d.end)
	}
}

func TestMIDIPlaysASecondEndingOnce(t *testing.T) {
	a, b, c := "0 - - - - - - - - - - - - - - -", "2 - - - - - - - - - - - - - - -", "4 - - - - - - - - - - - - - - -"

	// four bars, of which the last is the first ending, and a second ending: A A A B | C
	cells := cellsOf(a + " " + a + " " + a + " " + b + " [2 " + c + " ||")

	data, err := MIDI(cells, meter44, "C", "endings", 110, true)
	if err != nil {
		t.Fatal(err)
	}

	var pitches []int
	for _, n := range decode(t, data).notes {
		pitches = append(pitches, n.pitch)
	}

	// A A A B, A A A, C
	if want := []int{60, 60, 60, 64, 60, 60, 60, 67}; !reflect.DeepEqual(pitches, want) {
		t.Errorf("played %v, want %v", pitches, want)
	}
}

func TestMIDIRestsAndTies(t *testing.T) {
	// a note, a rest, a note that goes on over the bar line, a rest at the end
	cells := cellsOf(`0 - - - z - - - 2 - - - - - - -  - - - - 4 - - - - - - - z - - - ||`)

	data, err := MIDI(cells, meter44, "C", "rests", 110, false)
	if err != nil {
		t.Fatal(err)
	}

	d := decode(t, data)

	if want := []midiNote{{0, 60, 480, 92}, {960, 64, 1440, 84}, {2400, 67, 960, 84}}; !reflect.DeepEqual(d.notes, want) {
		t.Errorf("got  %+v\nwant %+v", d.notes, want)
	}

	if d.end != 32*slotMIDI { // the rest at the end is part of the tune
		t.Errorf("the track ends at %d", d.end)
	}
}

func TestMIDITempoAndMeter(t *testing.T) {
	cells := cellsOf("0 - - - - - - - - - - - ||")

	data, err := MIDI(cells, abc.Meter{Num: 6, Den: 8}, "G", "jig", 120, false)
	if err != nil {
		t.Fatal(err)
	}

	// a beat is a dotted quarter, a quarter and a half: 120 of them a minute is 180 quarters a minute, 333333 µs each
	if d := decode(t, data); d.tempo != 333333 || d.meter != [2]int{6, 8} || d.key != 1 {
		t.Errorf("%+v", d)
	}

	for name, f := range map[string]func() ([]byte, error){
		"a tempo of 0":   func() ([]byte, error) { return MIDI(cells, meter44, "D", "x", 0, true) },
		"a tempo of 999": func() ([]byte, error) { return MIDI(cells, meter44, "D", "x", 999, true) },
		"a key of H":     func() ([]byte, error) { return MIDI(cells, meter44, "H", "x", 100, true) },
		"a meter of 4/3": func() ([]byte, error) { return MIDI(cells, abc.Meter{Num: 4, Den: 3}, "D", "x", 100, true) },
	} {
		if _, err := f(); err == nil {
			t.Errorf("%s was taken", name)
		}
	}
}

// Whatever is generated can be written, and the file says what the tune says: the notes of a tune follow each other and the
// tune takes as long as its cells.
func TestMIDIOfGeneratedTunes(t *testing.T) {
	for _, meter := range []abc.Meter{meter44, {Num: 6, Den: 8}, {Num: 3, Den: 4}} {
		made := aModel(meter)

		for s := range made.Rhythm {
			if s == 0 {
				for r := range made.Rhythm[s] {
					made.Rhythm[s][r] = [3]float64{1, 3, 1} // ties over the bar line, and rests
				}
			}
		}

		for _, p := range piecesOf(t, made, 40) {
			data, err := MIDI(p.Cells, meter, "D", "made", 110, true)
			if err != nil {
				t.Fatalf("%v seed %d: %v", meter, p.Ref, err)
			}

			d := decode(t, data)

			played := 0

			for _, c := range playOrder(p.Cells, meter) {
				if c.Kind <= Rest {
					played++
				}
			}

			if d.end != played*slotMIDI {
				t.Fatalf("%v seed %d: %d cells played, the track ends at tick %d", meter, p.Ref, played, d.end)
			}

			last := 0

			for _, n := range d.notes {
				if n.at < last || n.length <= 0 || n.pitch < 36 || n.pitch > 100 {
					t.Fatalf("%v seed %d: %+v", meter, p.Ref, n)
				}

				last = n.at + n.length
			}
		}
	}

	_ = fmt.Sprint
}
