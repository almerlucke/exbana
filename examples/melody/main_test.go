package main

import (
	"reflect"
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
