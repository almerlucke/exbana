package main

import (
	"bytes"
	"fmt"
	"math/bits"

	"github.com/almerlucke/exbana/v3/examples/melody/abc"
)

// A tune can be written as a standard MIDI file: one track, one voice, with the tempo, the meter and the key. The file has
// no dependencies, a MIDI file is a few headers and a list of notes.

const (
	midiDivision = 480 // ticks in a quarter note
	slotMIDI     = midiDivision / 4
	midiProgram  = 40 // violin: this is fiddle music
)

// keyOf reads a key like D or Bb: the pitch of its tonic in the octave of middle C, and the sharps (positive) and
// flats (negative) of its signature.
func keyOf(key string) (tonic, fifths int, err error) {
	letter, err := tonicIndex(key)
	if err != nil {
		return 0, 0, err
	}

	fifths = []int{0, 2, 4, -1, 1, 3, 5}[letter] // C D E F G A B
	semitone := []int{0, 2, 4, 5, 7, 9, 11}[letter]

	switch key[1:] {
	case "":
	case "#":
		fifths, semitone = fifths+7, semitone+1
	case "b":
		fifths, semitone = fifths-7, semitone-1
	default:
		return 0, 0, fmt.Errorf("%q is not a key like D or Bb", key)
	}

	if fifths < -7 || fifths > 7 {
		return 0, 0, fmt.Errorf("the key %s needs more than seven sharps or flats", key)
	}

	return 60 + semitone, fifths, nil
}

var majorScale = [7]int{0, 2, 4, 5, 7, 9, 11}

// pitchOf is the MIDI note of a step of the major scale on a tonic.
func pitchOf(tonic, step int) int {
	return tonic + 12*floorDiv(step, 7) + majorScale[(step%7+7)%7]
}

// playOrder is the cells of a tune as they are played: a part is played twice, and when it has a second ending, the
// bars of the part without its last one (the first ending) and then the second ending.
func playOrder(cells []Cell, meter abc.Meter) []Cell {
	n := slotsOf(meter)

	var out, part []Cell

	for _, c := range cells {
		if c.Kind != EndMark {
			part = append(part, c)
			continue
		}

		volta := -1

		for i, p := range part {
			if p.Kind == Volta {
				volta = i
			}
		}

		if volta < 0 {
			out = append(out, part...)
			out = append(out, part...)
		} else {
			body, ending := part[:volta], part[volta+1:]
			out = append(out, body...)
			out = append(out, body[:len(body)-n]...)
			out = append(out, ending...)
		}

		part = nil
	}

	return out
}

// MIDI writes a tune as a MIDI file. The tempo is the beats a minute, a beat being a quarter note, or a dotted quarter in
// 6/8. The parts are played with their repeats. The first beat of a bar is a little louder.
func MIDI(cells []Cell, meter abc.Meter, key, title string, tempo int, repeats bool) ([]byte, error) {
	tonic, fifths, err := keyOf(key)
	if err != nil {
		return nil, err
	}

	if tempo < 20 || tempo > 400 {
		return nil, fmt.Errorf("a tempo of %d beats a minute", tempo)
	}

	if bits.OnesCount(uint(meter.Den)) != 1 || meter.Num > 255 || meter.Den > 128 {
		return nil, fmt.Errorf("the meter %d/%d can't be written", meter.Num, meter.Den)
	}

	if repeats {
		cells = playOrder(cells, meter)
	} else {
		var plain []Cell

		for _, c := range cells {
			if c.Kind != EndMark && c.Kind != Volta {
				plain = append(plain, c)
			}
		}

		cells = plain
	}

	// the notes: when they start, what they are, how long, and how loud
	type note struct{ at, pitch, length, velocity int }

	var (
		notes []note
		n     = slotsOf(meter)
		slot  int
		last  = -1 // the note that holds go on, -1 if it is a rest or there is none
	)

	for _, c := range cells {
		if c.Kind == Volta || c.Kind == EndMark {
			continue
		}

		switch c.Kind {
		case Onset:
			velocity := 76
			if slot%n == 0 {
				velocity = 92
			} else if s := strength(meter, slot%n); s == 1 {
				velocity = 84
			}

			notes = append(notes, note{slot * slotMIDI, pitchOf(tonic, c.Degree), slotMIDI, velocity})
			last = len(notes) - 1
		case Rest:
			last = -1
		case Hold:
			if last >= 0 {
				notes[last].length += slotMIDI
			}
		}

		slot++
	}

	var track bytes.Buffer

	meta := func(delta int, kind byte, data ...byte) {
		track.Write(varLen(delta))
		track.WriteByte(0xFF)
		track.WriteByte(kind)
		track.Write(varLen(len(data)))
		track.Write(data)
	}

	// microseconds a quarter note: a beat of beatSlots slots is a minute over the tempo
	perQuarter := 60_000_000 * 4 / (tempo * beatSlots(meter))

	meta(0, 0x03, []byte(title)...)
	meta(0, 0x51, byte(perQuarter>>16), byte(perQuarter>>8), byte(perQuarter))
	meta(0, 0x58, byte(meter.Num), byte(bits.TrailingZeros(uint(meter.Den))), 24, 8)
	meta(0, 0x59, byte(int8(fifths)), 0)

	track.Write([]byte{0x00, 0xC0, midiProgram})

	clock := 0

	for i, nt := range notes {
		// a note ends where the next one begins, or later: this is one voice
		end := nt.at + nt.length
		if i+1 < len(notes) && end > notes[i+1].at {
			end = notes[i+1].at
		}

		track.Write(varLen(nt.at - clock))
		track.Write([]byte{0x90, byte(nt.pitch), byte(nt.velocity)})
		track.Write(varLen(end - nt.at))
		track.Write([]byte{0x80, byte(nt.pitch), 0})

		clock = end
	}

	// the track ends where the tune does, so that a rest at the end is heard
	meta(max(slot*slotMIDI-clock, 0), 0x2F)

	var out bytes.Buffer

	out.WriteString("MThd")
	out.Write([]byte{0, 0, 0, 6, 0, 0, 0, 1, byte(midiDivision >> 8), byte(midiDivision & 0xFF)})
	out.WriteString("MTrk")
	out.Write([]byte{byte(track.Len() >> 24), byte(track.Len() >> 16), byte(track.Len() >> 8), byte(track.Len())})
	out.Write(track.Bytes())

	return out.Bytes(), nil
}

// varLen is a number the way MIDI writes it: seven bits to a byte, the high bit set on all but the last.
func varLen(v int) []byte {
	out := []byte{byte(v & 0x7F)}

	for v >>= 7; v > 0; v >>= 7 {
		out = append([]byte{byte(v&0x7F) | 0x80}, out...)
	}

	return out
}
