package abc

// Playthrough is the bars in the order they are played: repeats played twice, and of a first and second ending only the
// one that belongs to the time through.
func (t *Tune) Playthrough() []Bar {
	var (
		out   []Bar
		bars  = t.Bars
		start = 0
	)

	for i := 0; i < len(bars); {
		// the section ends at the first bar that ends a repeat
		end := -1
		for j := i; j < len(bars); j++ {
			if bars[j].RepeatEnd {
				end = j
				break
			}
		}

		if end < 0 {
			out = append(out, bars[i:]...)
			break
		}

		start = i
		for j := i; j <= end; j++ {
			if bars[j].RepeatStart {
				start = j
			}
		}

		out = append(out, bars[i:start]...)

		// the first time through, all of it
		out = append(out, bars[start:end+1]...)

		// the second time, without the first ending
		for j := start; j <= end; j++ {
			if bars[j].Ending != 1 {
				out = append(out, bars[j])
			}
		}

		i = end + 1

		// the second ending follows the repeat, and is played once
		for i < len(bars) && bars[i].Ending == 2 && !bars[i].RepeatStart {
			out = append(out, bars[i])

			if bars[i].RepeatEnd {
				i++
				break
			}

			i++
		}
	}

	return out
}

// Melody is the notes of the tune as they are played, with the repeats played out and ties made into one longer note.
func (t *Tune) Melody() []Event {
	var out []Event

	tied := false

	for _, b := range t.Playthrough() {
		for _, e := range b.Events {
			if tied && len(out) > 0 && !e.Rest && out[len(out)-1].MIDI == e.MIDI {
				out[len(out)-1].Ticks += e.Ticks
				out[len(out)-1].Tie = e.Tie
			} else {
				out = append(out, e)
			}

			tied = e.Tie && !e.Rest
		}
	}

	for i := range out {
		out[i].Tie = false
	}

	return out
}

// Irregular is the bars that don't fill their meter. Not counted are a short bar at the start of the tune or the end of
// it (a pickup, and what is left of its bar), and a short bar that the short bar after it fills up, which is a part of a
// tune that ends and the next that starts with a pickup. A tune with no irregular bars was read whole.
func (t *Tune) Irregular() []int {
	var bad []int

	for i := 0; i < len(t.Bars); i++ {
		b := t.Bars[i]
		full := b.Meter.Ticks()

		switch {
		case b.Ticks == full:
		case b.Ticks > full:
			bad = append(bad, i)
		case i == 0 || i == len(t.Bars)-1:
		case t.Bars[i+1].Meter == b.Meter && b.Ticks+t.Bars[i+1].Ticks == full:
			i++
		default:
			bad = append(bad, i)
		}
	}

	return bad
}
