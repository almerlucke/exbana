package optimize

import (
	"math"
	"math/rand"
	"testing"
)

// The sets are checked against the obvious model: membership of single elements, at every element of a small window and at
// both ends of the range, where the intervals that reach the ends live.

const window = 10

func randomSet(rng *rand.Rand) set {
	var iv []interval

	for range rng.Intn(5) {
		lo := int64(rng.Intn(2*window+1) - window)
		hi := lo + int64(rng.Intn(5))

		switch rng.Intn(6) {
		case 0:
			lo = math.MinInt64
		case 1:
			hi = math.MaxInt64
		}

		iv = append(iv, interval{lo, hi})
	}

	return normalize(iv)
}

func contains(s set, v int64) bool {
	for _, iv := range s {
		if v >= iv.lo && v <= iv.hi {
			return true
		}
	}

	return false
}

// check compares a set with a model at every element of the window and at both ends of the range
func check(t *testing.T, what string, s set, want func(int64) bool) {
	t.Helper()

	probes := []int64{math.MinInt64, math.MinInt64 + 1, -window - 1, -window, -1, 0, 1, window, window + 1, math.MaxInt64 - 1, math.MaxInt64}
	for v := int64(-window); v <= window; v++ {
		probes = append(probes, v)
	}

	for _, v := range probes {
		if contains(s, v) != want(v) {
			t.Fatalf("%s: element %d is %v, want %v; set %v", what, v, contains(s, v), want(v), s)
		}
	}

	// normalized: sorted, disjoint, and not touching
	for i := range s {
		if s[i].lo > s[i].hi || (i > 0 && s[i].lo <= s[i-1].hi+1 && s[i-1].hi != math.MaxInt64) {
			t.Fatalf("%s: %v is not normalized", what, s)
		}
	}
}

func TestSetOperationsAgreeWithTheModel(t *testing.T) {
	rng := rand.New(rand.NewSource(1))

	for range 3000 {
		a, b := randomSet(rng), randomSet(rng)

		check(t, "union", union(a, b), func(v int64) bool { return contains(a, v) || contains(b, v) })
		check(t, "intersect", intersect(a, b), func(v int64) bool { return contains(a, v) && contains(b, v) })
		check(t, "complement", complement(a), func(v int64) bool { return !contains(a, v) })

		// laws
		if !equal(complement(complement(a)), a) {
			t.Fatalf("complement twice: %v -> %v", a, complement(complement(a)))
		}
		if !intersect(a, complement(a)).isEmpty() {
			t.Fatalf("a set and its complement overlap: %v", a)
		}
		if !union(a, complement(a)).isAll() {
			t.Fatalf("a set and its complement are not everything: %v", a)
		}
		if !equal(union(a, b), union(b, a)) || !equal(intersect(a, b), intersect(b, a)) {
			t.Fatalf("not commutative: %v %v", a, b)
		}
	}
}

func TestNormalizeMergesTouchingAndOverlappingIntervals(t *testing.T) {
	got := normalize([]interval{{5, 9}, {1, 3}, {4, 4}, {20, 30}, {25, 26}, {7, 2}})
	want := set{{1, 9}, {20, 30}}

	if !equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	// the largest value must not overflow when the next interval is checked against it
	if got := normalize([]interval{{0, math.MaxInt64}, {5, 6}}); !equal(got, set{{0, math.MaxInt64}}) {
		t.Errorf("got %v", got)
	}
}

func TestFormat(t *testing.T) {
	for _, c := range []struct {
		s     set
		runes bool
		want  string
	}{
		{nil, true, "nothing"},
		{all, true, "anything"},
		{set{{'a', 'z'}}, true, "'a' to 'z'"},
		{set{{'a', 'a'}, {'c', 'c'}}, true, "'a', 'c'"},
		{set{{1, 5}}, false, "1 to 5"},
		{set{{math.MinInt64, 4}}, false, "the smallest value to 4"},
	} {
		if got := c.s.format(c.runes); got != c.want {
			t.Errorf("format(%v) = %q, want %q", c.s, got, c.want)
		}
	}
}
