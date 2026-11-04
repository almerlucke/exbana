package tests

import (
	"bytes"
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"testing"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/patterns/entity"
	"github.com/almerlucke/exbana/v3/patterns/vector"
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
		p: ebnf.Text(entity.Equals[rune, runes.Pos](c), string(c)),
		ends: func(in []rune, pos int) map[int]bool {
			if pos < len(in) && in[pos] == c {
				return setOf(pos + 1)
			}
			return setOf()
		},
	}
}

// classNode is one element from lo to hi, or any other one if negated
func classNode(lo, hi rune, negated bool) gnode {
	var p ebnf.Pattern[rune, runes.Pos]
	if negated {
		p = ebnf.Text(entity.NotIn[rune, runes.Pos](entity.Range[rune]{Lo: lo, Hi: hi}), "[^"+string(lo)+"-"+string(hi)+"]")
	} else {
		p = ebnf.Text(entity.Between[rune, runes.Pos](lo, hi), "["+string(lo)+"-"+string(hi)+"]")
	}

	return gnode{
		p: p,
		ends: func(in []rune, pos int) map[int]bool {
			if pos < len(in) && (in[pos] >= lo && in[pos] <= hi) != negated {
				return setOf(pos + 1)
			}
			return setOf()
		},
	}
}

func vecNode(s string) gnode {
	rs := []rune(s)
	return gnode{
		p: vector.Text[runes.Pos](s),
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
	return randomNodeFor(rng, depth, false)
}

// randomNodeFor makes a random grammar. A grammar that is only used to generate from can not have an end pattern or an
// except: what follows the generated text decides if they match, and generating does not know what follows.
func randomNodeFor(rng *rand.Rand, depth int, generating bool) gnode {
	leaf := func() gnode {
		switch rng.Intn(8) {
		case 6, 7:
			if generating {
				return leafNode('b')
			}
			lo := rune('a' + rng.Intn(3))
			return classNode(lo, lo+rune(rng.Intn(int('c'-lo)+1)), rng.Intn(3) == 0)
		case 0:
			return vecNode([]string{"ab", "ba", "aa", "abc"}[rng.Intn(4)])
		case 1:
			if generating {
				return leafNode('c')
			}
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
			out[i] = randomNodeFor(rng, depth-1, generating)
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
		child := randomNodeFor(rng, depth-1, generating)
		for tries := 0; child.nullable && tries < 5; tries++ {
			child = randomNodeFor(rng, depth-1, generating)
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
		if generating {
			return leaf()
		}
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

// checkAgainstOracle matches the grammar against the input in every way and compares with the oracle, it reports whether
// the grammar matched.
func checkAgainstOracle(t testing.TB, g gnode, input []rune) bool {
	t.Helper()

	var desc bytes.Buffer
	_ = g.p.Print(&desc)

	memoIdx := func(p runes.Pos) int { return p.Index }

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

	// the grammar described as a tree must mean the same as the grammar itself
	if described, err := ebnf.Analyze[rune, runes.Pos](g.p); err != nil {
		t.Fatalf("grammar %s: Analyze: %v", desc.String(), err)
	} else if got := irEnds(described, described.Root, input, 0); !equalSets(got, want) {
		fail("the analysed grammar %s ends at %v, the oracle says %v", described, got, want)
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
	full := concR(g.p, endR())
	wantFull := want[len(input)]
	plainFull, memoFull := parse(full, false), parse(full, true)

	if plainFull.ok != wantFull {
		fail("followed by end of input: matched=%v, the oracle says %v", plainFull.ok, wantFull)
	}
	if plainFull.ok != memoFull.ok || (plainFull.ok && fingerprint(plainFull.m) != fingerprint(memoFull.m)) {
		fail("followed by end of input: memo differs: plain %v, memo %v", plainFull.ok, memoFull.ok)
	}

	return plain.ok
}

// abcInput maps arbitrary bytes onto the alphabet the random grammars use
func abcInput(raw string, limit int) []rune {
	out := make([]rune, 0, limit)
	for i := 0; i < len(raw) && i < limit; i++ {
		out = append(out, rune('a'+raw[i]%3))
	}

	return out
}

func TestDifferentialAgainstOracle(t *testing.T) {
	rng := rand.New(rand.NewSource(20240607))

	grammars, checks, matchedCount := 0, 0, 0

	for range 600 {
		g := randomNode(rng, 3)
		grammars++

		for range 12 {
			input := make([]rune, rng.Intn(7))
			for i := range input {
				input[i] = rune('a' + rng.Intn(3))
			}

			checks++
			if checkAgainstOracle(t, g, input) {
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

// FuzzGrammarsAgainstOracle lets the fuzzer pick the grammar (through the seed of the random grammar generator) and the
// input. Run it with: go test ./tests -run '^$' -fuzz FuzzGrammarsAgainstOracle
func FuzzGrammarsAgainstOracle(f *testing.F) {
	f.Add(int64(1), "abcab")
	f.Add(int64(20240607), "")
	f.Add(int64(42), "aaaaaaa")
	f.Add(int64(7), "cba")

	f.Fuzz(func(t *testing.T, seed int64, raw string) {
		rng := rand.New(rand.NewSource(seed))
		checkAgainstOracle(t, randomNode(rng, 3), abcInput(raw, 9))
	})
}

func equalSets(a, b map[int]bool) bool {
	if len(a) != len(b) {
		return false
	}

	for k := range a {
		if !b[k] {
			return false
		}
	}

	return true
}

// irEnds is a second, independent implementation of the oracle that works on the analysed tree, with nothing but
// the information in the nodes: if it agrees, the shapes of the patterns describe them completely.
func irEnds(g *ebnf.Grammar[rune, runes.Pos], n *ebnf.Node[rune, runes.Pos], in []rune, pos int) map[int]bool {
	step := func(set map[int]bool, child *ebnf.Node[rune, runes.Pos]) map[int]bool {
		next := map[int]bool{}
		for at := range set {
			next = union(next, irEnds(g, child, in, at))
		}
		return next
	}

	switch n.Kind {
	case ebnf.KindRef:
		return irEnds(g, g.Rules[n.ID].Children[0], in, pos)
	case ebnf.KindEnd:
		if pos == len(in) {
			return setOf(pos)
		}
		return setOf()
	case ebnf.KindLiteral:
		if !n.Exact {
			panic("literal with an equality function nobody can see")
		}
		if pos+len(n.Values) <= len(in) && slices.Equal(in[pos:pos+len(n.Values)], n.Values) {
			return setOf(pos + len(n.Values))
		}
		return setOf()
	case ebnf.KindClass:
		if !n.Exact {
			panic("class decided by a function nobody can see")
		}
		if pos >= len(in) {
			return setOf()
		}
		inside := false
		for _, r := range n.Ranges {
			if in[pos] >= r[0] && in[pos] <= r[1] {
				inside = true
			}
		}
		if inside != n.Negated {
			return setOf(pos + 1)
		}
		return setOf()
	case ebnf.KindSeq:
		cur := setOf(pos)
		for _, c := range n.Children {
			cur = step(cur, c)
		}
		return cur
	case ebnf.KindAlt:
		out := map[int]bool{}
		for _, c := range n.Children {
			out = union(out, irEnds(g, c, in, pos))
		}
		return out
	case ebnf.KindExcept:
		if len(irEnds(g, n.Children[1], in, pos)) > 0 {
			return setOf()
		}
		return irEnds(g, n.Children[0], in, pos)
	case ebnf.KindRepeat:
		child := n.Children[0]
		cur := setOf(pos)
		for range n.Min {
			cur = step(cur, child)
		}
		res := union(cur)
		if n.Max == 0 {
			for frontier := cur; len(frontier) > 0; {
				next := map[int]bool{}
				for at := range step(frontier, child) {
					if !res[at] {
						next[at] = true
						res[at] = true
					}
				}
				frontier = next
			}
			return res
		}
		for range n.Max - n.Min {
			cur = step(cur, child)
			res = union(res, cur)
		}
		return res
	}

	panic("cannot interpret " + n.Kind.String())
}
