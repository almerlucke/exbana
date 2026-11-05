// Command melody generates melodies from a grammar, and can learn from known ones what to generate.
//
//	piece   = phrase , phrase
//	phrase  = bar , bar , bar , cadence
//	bar     = q q q q | h q q | q q h | h h | q h q      (every alternative adds up to 4 beats)
//	cadence = whole note on the tonic
//
// The grammar is a pattern over notes, and a pattern both parses and generates. So the same grammar that writes a
// melody also reads one. With -learn it reads known melodies (a few simple songs are built in, -corpus names a file
// with your own), counts which rhythms their bars have and how far their notes step, and generates new melodies with
// those odds instead of the hand-made ones. Everything is seeded: the same seed gives the same melody.
//
//	melody -n 3                 melodies with the hand-made odds
//	melody -learn -n 3          melodies that move like the built-in songs
//	melody -corpus songs.txt    melodies that move like the songs in the file, one "name: notes" a line
//
// With -nottingham it does the same with real folk tunes. It reads the ABC files of the Nottingham Music Database (not
// part of this repository: it is under the GPL, https://github.com/jukedeck/nottingham-dataset), turns the tunes into
// cells, one for every sixteenth note, reads them with a grammar of parts, phrases and bars (folk.go), learns from the
// parses how the rhythm goes, how far the notes step and where phrases come to rest, and writes new tunes as ABC:
//
//	melody -nottingham ABC_cleaned -meter 6/8 -key G -n 3 -midi jig.mid > jigs.abc
//
// -midi writes each tune as a MIDI file as well (-tempo sets the speed), with its repeats played.
//
// A tune is left out of the learning if it does not fit the grammar: it has triplets, is not in a major key, changes key,
// or has parts that are not made of phrases of four bars. The command says how many, and why.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/almerlucke/exbana/v3/examples/melody/abc"
)

// A corpus is known melodies, written as bars of notes. They are simple arrangements, in C major, that fit the grammar:
// quarter and half notes, phrases of three bars and a whole tonic. They are not the songs as they are written.
var corpus = []struct{ name, notes string }{
	{"Ode to Joy", `E4/q E4/q F4/q G4/q | G4/q F4/q E4/q D4/q | C4/q C4/q D4/q E4/q | C4/w
	                E4/q E4/q F4/q G4/q | G4/q F4/q E4/q D4/q | C4/q D4/q E4/h | C4/w`},
	{"Frere Jacques", `C4/q D4/q E4/q C4/q | C4/q D4/q E4/q C4/q | E4/q F4/q G4/h | C4/w
	                   G4/q A4/q G4/q F4/q | E4/h C4/h | D4/q E4/q C4/h | C4/w`},
	{"Mary Had a Little Lamb", `E4/q D4/q C4/q D4/q | E4/q E4/q E4/h | D4/q D4/q D4/h | C4/w
	                            E4/q G4/q G4/h | E4/q D4/q C4/q D4/q | E4/q E4/q E4/q E4/q | C4/w`},
	{"Twinkle Twinkle", `C4/q C4/q G4/q G4/q | A4/q A4/q G4/h | F4/q F4/q E4/q E4/q | C4/w
	                     G4/q G4/q F4/q F4/q | E4/q E4/q D4/h | G4/q G4/q F4/q F4/q | C4/w`},
}

// readCorpus reads melodies from a file with a "name: notes" on every line, lines with # are comments.
func readCorpus(path string) ([][]Note, []string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	var (
		melodies [][]Note
		titles   []string
		sc       = bufio.NewScanner(f)
	)

	for line := 1; sc.Scan(); line++ {
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}

		title, notes, ok := strings.Cut(text, ":")
		if !ok {
			return nil, nil, fmt.Errorf("%s:%d: expected name: notes", path, line)
		}

		parsed, err := ParseNotes(notes)
		if err != nil {
			return nil, nil, fmt.Errorf("%s:%d: %w", path, line, err)
		}

		melodies = append(melodies, parsed)
		titles = append(titles, strings.TrimSpace(title))
	}

	return melodies, titles, sc.Err()
}

func builtin() ([][]Note, []string, error) {
	var (
		melodies [][]Note
		titles   []string
	)

	for _, c := range corpus {
		notes, err := ParseNotes(c.notes)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", c.name, err)
		}

		melodies = append(melodies, notes)
		titles = append(titles, c.name)
	}

	return melodies, titles, nil
}

func show(notes []Note) string {
	var parts []string
	beats := 0.0

	for _, n := range notes {
		parts = append(parts, n.String())
		beats += n.Beats

		if beats == float64(int(beats)) && int(beats)%4 == 0 {
			parts = append(parts, "|")
			beats = 0
		}
	}

	return strings.Join(parts, " ")
}

func percent(ws []float64) string {
	sum := 0.0
	for _, w := range ws {
		sum += w
	}

	parts := make([]string, len(ws))
	for i, w := range ws {
		parts[i] = fmt.Sprintf("%.0f%%", 100*w/sum)
	}

	return strings.Join(parts, " ")
}

// folk learns from the ABC files of a directory what folk tunes of a meter are like, and writes new ones.
func folk(dir, meterText string, key string, seed int64, count int, midiPath string, tempo int) error {
	var meter abc.Meter

	if _, err := fmt.Sscanf(meterText, "%d/%d", &meter.Num, &meter.Den); err != nil {
		return fmt.Errorf("-meter %q is not like 4/4", meterText)
	}

	corpus, err := LoadCorpus(dir)
	if err != nil {
		return err
	}

	learned, err := LearnFolk(corpus, meter)
	if err != nil {
		return err
	}

	fmt.Printf("%% %d tunes read, %d made into cells, %d of them in %s and read with the grammar\n",
		corpus.Tunes, len(corpus.Pieces), learned.Tunes, meterText)

	var left []string
	for reason, n := range corpus.Left {
		left = append(left, fmt.Sprintf("%d %s", n, reason))
	}

	sort.Strings(left)

	for _, l := range left {
		fmt.Println("%   left out:", l)
	}

	for _, f := range learned.Failed {
		fmt.Println("%   not read:", f)
	}

	for i := 0; i < count; i++ {
		cells, err := ComposeFolk(learned.Model, seed+int64(i))
		if err != nil {
			return err
		}

		text, err := ABC(cells, meter, key, fmt.Sprintf("Generated %d", seed+int64(i)), i+1)
		if err != nil {
			return err
		}

		fmt.Println()
		fmt.Print(text)

		if midiPath != "" {
			data, err := MIDI(cells, meter, key, fmt.Sprintf("Generated %d", seed+int64(i)), tempo, true)
			if err != nil {
				return err
			}

			path := midiPath
			if count > 1 {
				ext := filepath.Ext(path)
				path = fmt.Sprintf("%s-%d%s", strings.TrimSuffix(path, ext), i+1, ext)
			}

			if err := os.WriteFile(path, data, 0o644); err != nil {
				return err
			}

			fmt.Printf("%% written to %s\n", path)
		}
	}

	return nil
}

func main() {
	seed := flag.Int64("seed", 1, "random seed")
	count := flag.Int("n", 3, "number of melodies")
	learn := flag.Bool("learn", false, "learn the odds from the built-in songs instead of the hand-made ones")
	file := flag.String("corpus", "", "learn the odds from the melodies in this file (name: notes, one a line)")
	smoothing := flag.Float64("smoothing", 0.25, "added to every count when learning, so that what the songs never do is rare, not impossible")
	dir := flag.String("nottingham", "", "learn folk tunes from the ABC files in this directory (the Nottingham Music Database), and write new ones in ABC")
	meterFlag := flag.String("meter", "4/4", "the meter of the folk tunes")
	key := flag.String("key", "D", "the key the folk tunes are written in")
	midiPath := flag.String("midi", "", "also write the folk tunes as MIDI files: this file, or with the number of the tune before the extension for more than one")
	tempo := flag.Int("tempo", 110, "the beats a minute of the MIDI files, a beat is a quarter note, or a dotted quarter in 6/8")
	flag.Parse()

	if *dir != "" {
		if err := folk(*dir, *meterFlag, *key, *seed, *count, *midiPath, *tempo); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}

		return
	}

	model := DefaultModel()

	if *learn || *file != "" {
		var (
			melodies [][]Note
			titles   []string
			err      error
		)

		if *file != "" {
			melodies, titles, err = readCorpus(*file)
		} else {
			melodies, titles, err = builtin()
		}

		if err == nil {
			model, err = Learn(melodies, *smoothing)
		}

		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}

		fmt.Printf("learned from %d melodies (%s)\n", len(melodies), strings.Join(titles, ", "))
		fmt.Printf("  bars   %s: %s\n", strings.Join(barNames, " "), percent(model.Bars))
		fmt.Printf("  steps  %v: %s\n", deltas, percent(model.Deltas))
	}

	for i := 0; i < *count; i++ {
		notes, err := ComposeWith(model, *seed+int64(i))
		if err != nil {
			fmt.Println("error:", err)
			return
		}

		fmt.Printf("seed %d: %s\n", *seed+int64(i), show(notes))
	}
}
