// Command abcstat reads ABC files (the Nottingham Music Database, for one) and says how many of the tunes were read, and
// what they are like: meters, keys, and the lengths of notes.
//
//	abcstat ABC_cleaned/*.abc
package main

import (
	"fmt"
	"os"
	"sort"

	"github.com/almerlucke/exbana/v3/examples/melody/abc"
)

func top(counts map[string]int, n int) string {
	type kv struct {
		k string
		v int
	}

	var all []kv
	for k, v := range counts {
		all = append(all, kv{k, v})
	}

	sort.Slice(all, func(i, j int) bool { return all[i].v > all[j].v || all[i].v == all[j].v && all[i].k < all[j].k })

	s := ""
	for i, e := range all {
		if i == n {
			break
		}

		s += fmt.Sprintf("  %s %d", e.k, e.v)
	}

	return s
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: abcstat file.abc ...")
		os.Exit(2)
	}

	var (
		tunes     []*abc.Tune
		failed    []error
		meters    = map[string]int{}
		keys      = map[string]int{}
		lengths   = map[string]int{}
		irregular int
		notes     int
	)

	for _, name := range os.Args[1:] {
		f, err := os.Open(name)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}

		ts, errs := abc.Read(f)
		f.Close()

		tunes = append(tunes, ts...)
		failed = append(failed, errs...)
	}

	for _, t := range tunes {
		if len(t.Irregular()) > 0 {
			irregular++
		}

		meters[fmt.Sprintf("%d/%d", t.Bars[0].Meter.Num, t.Bars[0].Meter.Den)]++
		keys[t.Bars[0].Key.String()]++

		for _, e := range t.Melody() {
			if !e.Rest {
				notes++
			}

			lengths[fmt.Sprintf("%d", e.Ticks)]++
		}
	}

	fmt.Printf("%d tunes read, %d not\n", len(tunes), len(failed))
	fmt.Printf("%d notes when played through\n", notes)
	fmt.Printf("%d tunes have bars that don't fill their meter\n", irregular)
	fmt.Println("meters", top(meters, 6))
	fmt.Println("keys", top(keys, 8))
	fmt.Println("note lengths in ticks (24 is a quarter)", top(lengths, 8))

	for i, err := range failed {
		if i == 10 {
			fmt.Printf("... and %d more\n", len(failed)-i)
			break
		}

		fmt.Println(err)
	}
}
