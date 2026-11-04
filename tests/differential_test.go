package tests

import (
	"bytes"
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"testing"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/readers/runes"
)

// A differential test: random small grammars are matched against random inputs, and the library is compared with an
// oracle that knows nothing about continuations, memoization or preference order. The oracle computes the set of
// positions at which a grammar can end, straight from the definition of the EBNF constructs. The library must
//   - match exactly when that set is not empty, ending at a position in the set,
//   - match grammar followed by end of input exactly when the end of the input is in the set (that needs backtracking),
//   - give the same answer, and the same match tree, with and without the failure memo.

type gnode struct {
	p        ebnf.Pattern[rune, runes.Pos]
	ends     func(in []rune, pos int) map[int]bool
	nullable bool // can match the empty input; over approximated, only used to keep repetitions finite
}

func setOf(vals ...int) map[int]bool {
	s := make(map[int]bool, len(vals))
	for _, v := range vals {
		s[v] = true
	}
	return s
}

func union(sets ...map[int]bool) map[int]bool {
	out := map[int]bool{}
	for _, s := range sets {
		for k := range s {
			out[k] = true
		}
	}
	return out
}

func leafNode(c rune) gnode {
	return gnode{
		p: ebnf.Text(runeR(c), string(c)),
		ends: func(in []rune, pos int) map[int]bool {
			if pos < len(in) && in[pos] == c {
				return setOf(pos + 1)
			}
			return setOf()
		},
	}
}

func vecNode(s string) gnode {
	rs := []rune(s)
	return gnode{
		p: ebnf.Text(vecR(s), s),
		ends: func(in []rune, pos int) map[int]bool {
			if pos+len(rs) <= len(in) && slices.Equal(in[pos:pos+len(rs)], rs) {
				return setOf(pos + len(rs))
			}
			return setOf()
		},
	}
}

func endNode() gnode {
	return gnode{
		p: ebnf.Text(endR(), "$"),
		ends: func(in []rune, pos int) map[int]bool {
			if pos == len(in) {
				return setOf(pos)
			}
			return setOf()
		},
		nullable: true,
	}
}

func altNode(children ...gnode) gnode {
	ps := make([]ebnf.Pattern[rune, runes.Pos], len(children))
	nullable := false
	for i, c := range children {
		ps[i] = c.p
		nullable = nullable || c.nullable
	}
	return gnode{
		p: altR(ps...),
		ends: func(in []rune, pos int) map[int]bool {
			out := map[int]bool{}
			for _, c := range children {
				out = union(out, c.ends(in, pos))
			}
			return out
		},
		nullable: nullable,
	}
}

func concNode(children ...gnode) gnode {
	ps := make([]ebnf.Pattern[rune, runes.Pos], len(children))
	nullable := true
	for i, c := range children {
		ps[i] = c.p
		nullable = nullable && c.nullable
	}
	return gnode{
		p: concR(ps...),
		ends: func(in []rune, pos int) map[int]bool {
			cur := setOf(pos)
			for _, c := range children {
				next := map[int]bool{}
				for at := range cur {
					next = union(next, c.ends(in, at))
				}
				cur = next
			}
			return cur
		},
		nullable: nullable,
	}
}

// repNode requires a child that can not match the empty input
func repNode(child gnode, min, max int) gnode {
	return gnode{
		p: repR(child.p, min, max),
		ends: func(in []rune, pos int) map[int]bool {
			step := func(set map[int]bool) map[int]bool {
				next := map[int]bool{}
				for at := range set {
					next = union(next, child.ends(in, at))
				}
				return next
			}

			cur := setOf(pos)
			for range min {
				cur = step(cur)
			}

			res := union(cur)
			if max == 0 {
				for frontier := cur; len(frontier) > 0; {
					next := map[int]bool{}
					for at := range step(frontier) {
						if !res[at] {
							next[at] = true
							res[at] = true
						}
					}
					frontier = next
				}
				return res
			}

			for range max - min {
				cur = step(cur)
				res = union(res, cur)
			}
			return res
		},
		nullable: min == 0,
	}
}

func exceptNode(must, exception gnode) gnode {
	return gnode{
		p: exceptR(must.p, exception.p),
		ends: func(in []rune, pos int) map[int]bool {
			if len(exception.ends(in, pos)) > 0 {
				return setOf()
			}
			return must.ends(in, pos)
		},
		nullable: must.nullable,
	}
}

func randomNode(rng *rand.Rand, depth int) gnode {
	leaf := func() gnode {
		switch rng.Intn(6) {
		case 0:
			return vecNode([]string{"ab", "ba", "aa", "abc"}[rng.Intn(4)])
		case 1:
			return endNode()
		default:
			return leafNode(rune("abc"[rng.Intn(3)]))
		}
	}

	if depth == 0 {
		return leaf()
	}

	kids := func(n int) []gnode {
		out := make([]gnode, n)
		for i := range out {
			out[i] = randomNode(rng, depth-1)
		}
		return out
	}

	switch rng.Intn(7) {
	case 0:
		return leaf()
	case 1, 2:
		return altNode(kids(2 + rng.Intn(2))...)
	case 3, 4:
		return concNode(kids(2 + rng.Intn(2))...)
	case 5:
		child := randomNode(rng, depth-1)
		for tries := 0; child.nullable && tries < 5; tries++ {
			child = randomNode(rng, depth-1)
		}
		if child.nullable {
			child = leaf()
			for child.nullable {
				child = leaf()
			}
		}

		min := rng.Intn(3)
		max := 0
		if rng.Intn(3) > 0 {
			max = min + rng.Intn(3)
			if max == 0 {
				max = 1
			}
		}
		return repNode(child, min, max)
	default:
		return exceptNode(randomNode(rng, depth-1), randomNode(rng, depth-1))
	}
}

// fingerprint describes the shape of a match tree
func fingerprint(m *ebnf.Match[rune, runes.Pos]) string {
	var sb strings.Builder
	var walk func(m *ebnf.Match[rune, runes.Pos])
	walk = func(m *ebnf.Match[rune, runes.Pos]) {
		fmt.Fprintf(&sb, "%d-%d[", m.Begin.Index, m.End.Index)
		for _, c := range m.Components {
			walk(c)
		}
		sb.WriteString("]")
	}
	walk(m)
	return sb.String()
}

func TestDifferentialAgainstOracle(t *testing.T) {
	rng := rand.New(rand.NewSource(20240607))
	memoIdx := func(p runes.Pos) int { return p.Index }

	grammars, checks := 0, 0
	matchedCount := 0

	for range 600 {
		g := randomNode(rng, 3)
		grammars++

		var desc bytes.Buffer
		_ = g.p.Print(&desc)

		full := concR(g.p, endR())

		for range 12 {
			n := rng.Intn(7)
			input := make([]rune, n)
			for i := range input {
				input[i] = rune('a' + rng.Intn(3))
			}

			fail := func(format string, args ...any) {
				t.Helper()
				t.Errorf("grammar %s, input %q: %s", desc.String(), string(input), fmt.Sprintf(format, args...))
			}

			want := g.ends(input, 0)

			type result struct {
				ok bool
				m  *ebnf.Match[rune, runes.Pos]
			}
			parse := func(p ebnf.Pattern[rune, runes.Pos], memo bool) result {
				var rd ebnf.Reader[rune, runes.Pos] = newRunes(string(input))
				if memo {
					rd = ebnf.WithMemo[rune, runes.Pos](rd, memoIdx)
				}
				ok, m, err := ebnf.Parse(p, rd)
				if err != nil {
					t.Fatalf("grammar %s, input %q: %v", desc.String(), string(input), err)
				}
				return result{ok, m}
			}

			plain, memo := parse(g.p, false), parse(g.p, true)

			if plain.ok != (len(want) > 0) {
				fail("matched=%v but the oracle ends are %v", plain.ok, want)
			}
			if plain.ok && !want[plain.m.End.Index] {
				fail("ends at %d, the oracle says %v", plain.m.End.Index, want)
			}
			if plain.ok != memo.ok || (plain.ok && fingerprint(plain.m) != fingerprint(memo.m)) {
				fail("memo differs: plain %v, memo %v", plain.ok, memo.ok)
			}

			// grammar, end of input: only works if the grammar gives back until the ends line up
			wantFull := want[len(input)]
			plainFull, memoFull := parse(full, false), parse(full, true)

			if plainFull.ok != wantFull {
				fail("followed by end of input: matched=%v, the oracle says %v", plainFull.ok, wantFull)
			}
			if plainFull.ok != memoFull.ok || (plainFull.ok && fingerprint(plainFull.m) != fingerprint(memoFull.m)) {
				fail("followed by end of input: memo differs: plain %v, memo %v", plainFull.ok, memoFull.ok)
			}

			checks++
			if plain.ok {
				matchedCount++
			}
		}
	}

	// the test is only worth something if it sees both outcomes
	if matchedCount < checks/10 || matchedCount > checks*9/10 {
		t.Errorf("%d of %d cases matched, the random grammars are too one sided", matchedCount, checks)
	}
	t.Logf("%d grammars, %d cases, %d matched", grammars, checks, matchedCount)
}
