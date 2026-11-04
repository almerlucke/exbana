package main

import (
	"fmt"
	"strings"
	"testing"
)

func BenchmarkChain(b *testing.B) {
	for _, n := range []int{50, 100, 200, 400} {
		in := "1" + strings.Repeat(" + 1", n)
		calc := NewCalculator()
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			for b.Loop() {
				if _, err := calc.Eval(in); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
