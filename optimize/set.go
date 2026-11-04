package optimize

import (
	"math"
	"sort"
	"strconv"
	"strings"
)

// interval is an inclusive range of elements, as int64 so that every integer type up to 64 bits fits.
type interval struct{ lo, hi int64 }

// set is a set of elements as sorted, disjoint intervals that do not touch.
type set []interval

var (
	empty set
	all   = set{{math.MinInt64, math.MaxInt64}}
)

func (s set) isEmpty() bool { return len(s) == 0 }

func (s set) isAll() bool { return len(s) == 1 && s[0].lo == math.MinInt64 && s[0].hi == math.MaxInt64 }

// normalize sorts the intervals and merges those that overlap or touch.
func normalize(in []interval) set {
	if len(in) == 0 {
		return nil
	}

	cp := make([]interval, 0, len(in))
	for _, iv := range in {
		if iv.lo <= iv.hi {
			cp = append(cp, iv)
		}
	}

	sort.Slice(cp, func(i, j int) bool { return cp[i].lo < cp[j].lo })

	out := set{}

	for _, iv := range cp {
		if n := len(out); n > 0 && (out[n-1].hi == math.MaxInt64 || iv.lo <= out[n-1].hi+1) {
			out[n-1].hi = max(out[n-1].hi, iv.hi)
			continue
		}

		out = append(out, iv)
	}

	if len(out) == 0 {
		return nil
	}

	return out
}

func union(a, b set) set {
	if len(b) == 0 {
		return a
	}

	if len(a) == 0 {
		return b
	}

	return normalize(append(append([]interval{}, a...), b...))
}

func intersect(a, b set) set {
	var out []interval

	for i, j := 0, 0; i < len(a) && j < len(b); {
		lo, hi := max(a[i].lo, b[j].lo), min(a[i].hi, b[j].hi)
		if lo <= hi {
			out = append(out, interval{lo, hi})
		}

		if a[i].hi < b[j].hi {
			i++
		} else {
			j++
		}
	}

	return normalize(out)
}

// complement is every element that is not in the set.
func complement(s set) set {
	var out []interval

	next := int64(math.MinInt64)
	done := false

	for _, iv := range s {
		if iv.lo > next {
			out = append(out, interval{next, iv.lo - 1})
		}

		if iv.hi == math.MaxInt64 {
			done = true
			break
		}

		next = iv.hi + 1
	}

	if !done {
		out = append(out, interval{next, math.MaxInt64})
	}

	return normalize(out)
}

func equal(a, b set) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

// format writes the set for a message: runes as quoted characters when the elements are runes.
func (s set) format(runes bool) string {
	if s.isEmpty() {
		return "nothing"
	}

	if s.isAll() {
		return "anything"
	}

	el := func(v int64) string {
		switch {
		case v == math.MinInt64:
			return "the smallest value"
		case v == math.MaxInt64:
			return "the largest value"
		case runes && v >= 0 && v <= 0x10FFFF:
			q := strconv.QuoteRune(rune(v))
			return q
		}

		return strconv.FormatInt(v, 10)
	}

	parts := make([]string, 0, len(s))

	for i, iv := range s {
		if i == 6 {
			parts = append(parts, "...")
			break
		}

		if iv.lo == iv.hi {
			parts = append(parts, el(iv.lo))
		} else {
			parts = append(parts, el(iv.lo)+" to "+el(iv.hi))
		}
	}

	return strings.Join(parts, ", ")
}
