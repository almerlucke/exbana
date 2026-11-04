package slice_test

import (
	"errors"
	"io"
	"math/rand"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/readers/runes"
	"github.com/almerlucke/exbana/v3/readers/slice"
)

// The runes reader is the reference: for data without carriage returns the slice reader must do exactly the same for
// every sequence of operations.
func TestBehavesLikeTheRunesReader(t *testing.T) {
	rng := rand.New(rand.NewSource(42))

	for range 2000 {
		data := make([]rune, rng.Intn(12))
		for i := range data {
			data[i] = rune('a' + rng.Intn(5))
		}

		ref, err := runes.New(strings.NewReader(string(data)))
		if err != nil {
			t.Fatal(err)
		}

		got := slice.New(data)

		for step := range 40 {
			op := rng.Intn(9)
			where := func() string { return "data " + string(data) + " step " + string(rune('0'+step%10)) }

			switch op {
			case 0:
				a, ea := ref.Peek1()
				b, eb := got.Peek1()
				if a != b || ea != eb {
					t.Fatalf("%s Peek1: (%q, %v) vs (%q, %v)", where(), a, ea, b, eb)
				}
			case 1:
				a, ea := ref.Read1()
				b, eb := got.Read1()
				if a != b || ea != eb {
					t.Fatalf("%s Read1: (%q, %v) vs (%q, %v)", where(), a, ea, b, eb)
				}
			case 2, 3:
				n := rng.Intn(6)
				ba, bb := make([]rune, n), make([]rune, n)
				var na, nb int
				var ea, eb error
				if op == 2 {
					na, ea = ref.Peek(n, ba)
					nb, eb = got.Peek(n, bb)
				} else {
					na, ea = ref.Read(n, ba)
					nb, eb = got.Read(n, bb)
				}
				if na != nb || ea != eb || !slices.Equal(ba[:na], bb[:nb]) {
					t.Fatalf("%s Peek/Read %d: (%d, %v) vs (%d, %v)", where(), n, na, ea, nb, eb)
				}
			case 4:
				n := rng.Intn(6)
				na, ea := ref.Skip(n)
				nb, eb := got.Skip(n)
				if na != nb || ea != eb {
					t.Fatalf("%s Skip %d: (%d, %v) vs (%d, %v)", where(), n, na, ea, nb, eb)
				}
			case 5:
				p := rng.Intn(len(data)+3) - 1
				ea := ref.SetPosition(runes.Pos{Index: p})
				eb := got.SetPosition(p)
				if (ea == nil) != (eb == nil) {
					t.Fatalf("%s SetPosition %d: %v vs %v", where(), p, ea, eb)
				}
				// the runes reader keeps its line and column from the position it is given; only the index matters here
				if ea == nil {
					_ = ref.SetPosition(runes.Pos{Index: p})
				}
			case 6:
				a, b := rng.Intn(len(data)+1), rng.Intn(len(data)+1)
				if a > b {
					a, b = b, a
				}
				ra, ea := ref.Range(runes.Pos{Index: a}, runes.Pos{Index: b})
				rb, eb := got.Range(a, b)
				if (ea == nil) != (eb == nil) || !slices.Equal(ra, rb) {
					t.Fatalf("%s Range %d..%d: (%q, %v) vs (%q, %v)", where(), a, b, string(ra), ea, string(rb), eb)
				}
			case 7:
				a, b := rng.Intn(len(data)+1), rng.Intn(len(data)+1)
				if x, y := ref.Length(runes.Pos{Index: a}, runes.Pos{Index: b}), got.Length(a, b); x != y {
					t.Fatalf("%s Length %d..%d: %d vs %d", where(), a, b, x, y)
				}
			case 8:
				if ref.Finished() != got.Finished() {
					t.Fatalf("%s Finished: %v vs %v", where(), ref.Finished(), got.Finished())
				}
			}

			pa, _ := ref.Position()
			pb, _ := got.Position()
			if pa.Index != pb {
				t.Fatalf("data %q step %d op %d: position %d vs %d", string(data), step, op, pa.Index, pb)
			}
		}
	}
}

func TestOutOfBoundsRange(t *testing.T) {
	r := slice.New([]int{1, 2, 3})

	for _, c := range [][2]int{{-1, 2}, {0, 4}, {2, 1}, {4, 4}} {
		if _, err := r.Range(c[0], c[1]); err == nil {
			t.Errorf("Range%v should fail", c)
		}
	}

	if v, err := r.Range(1, 3); err != nil || !slices.Equal(v, []int{2, 3}) {
		t.Errorf("Range(1, 3) = %v, %v", v, err)
	}
}

func TestEmptyData(t *testing.T) {
	r := slice.New[string](nil)

	if _, err := r.Peek1(); !errors.Is(err, io.EOF) || !r.Finished() {
		t.Errorf("empty reader: %v finished=%v", err, r.Finished())
	}
	if n, err := r.Skip(2); n != 0 || !errors.Is(err, io.EOF) {
		t.Errorf("Skip = %d, %v", n, err)
	}
}

func TestIndexIsTheIdentity(t *testing.T) {
	// the position is the memo index, so a memoizing parse works without a function of your own
	g := newTwoStepGrammar()
	ok, _, err := ebnf.Parse[string, int](g, ebnf.WithMemo[string, int](slice.New([]string{"a", "b"}), slice.Index))
	if err != nil || !ok {
		t.Errorf("ok=%v err=%v", ok, err)
	}
}

// FuzzReaderOperations drives both readers with the operations the fuzzer encodes in bytes (one byte for the operation,
// the next for its argument), and requires the same results from both.
func FuzzReaderOperations(f *testing.F) {
	f.Add("abcab", []byte{0, 0, 1, 0, 2, 3, 4, 2, 5, 1, 6, 2})
	f.Add("", []byte{0, 0, 1, 0, 4, 1})
	f.Add("zzzzzzzzzz", []byte{3, 5, 3, 9, 5, 0, 6, 7, 8, 1})

	f.Fuzz(func(t *testing.T, raw string, ops []byte) {
		// the runes reader turns a carriage return into a line feed; the slice reader keeps what it is given
		data := []rune(strings.Map(func(r rune) rune {
			if r == '\r' || r == utf8.RuneError {
				return 'x'
			}
			return r
		}, raw))

		ref, err := runes.New(strings.NewReader(string(data)))
		if err != nil {
			t.Skip()
		}

		got := slice.New(data)
		n := len(data)

		for i := 0; i+1 < len(ops); i += 2 {
			op, arg := ops[i]%9, int(ops[i+1])

			switch op {
			case 0:
				a, ea := ref.Peek1()
				b, eb := got.Peek1()
				if a != b || ea != eb {
					t.Fatalf("Peek1: (%q, %v) vs (%q, %v)", a, ea, b, eb)
				}
			case 1:
				a, ea := ref.Read1()
				b, eb := got.Read1()
				if a != b || ea != eb {
					t.Fatalf("Read1: (%q, %v) vs (%q, %v)", a, ea, b, eb)
				}
			case 2, 3:
				k := arg % 8
				ba, bb := make([]rune, k), make([]rune, k)
				var na, nb int
				var ea, eb error
				if op == 2 {
					na, ea = ref.Peek(k, ba)
					nb, eb = got.Peek(k, bb)
				} else {
					na, ea = ref.Read(k, ba)
					nb, eb = got.Read(k, bb)
				}
				if na != nb || ea != eb || !slices.Equal(ba[:na], bb[:nb]) {
					t.Fatalf("Peek/Read %d: (%d, %v) vs (%d, %v)", k, na, ea, nb, eb)
				}
			case 4:
				k := arg % 8
				na, ea := ref.Skip(k)
				nb, eb := got.Skip(k)
				if na != nb || ea != eb {
					t.Fatalf("Skip %d: (%d, %v) vs (%d, %v)", k, na, ea, nb, eb)
				}
			case 5:
				p := arg%(n+3) - 1
				ea := ref.SetPosition(runes.Pos{Index: p})
				eb := got.SetPosition(p)
				if (ea == nil) != (eb == nil) {
					t.Fatalf("SetPosition %d: %v vs %v", p, ea, eb)
				}
			case 6, 7:
				a, b := arg%(n+1), (arg/7)%(n+1)
				if a > b {
					a, b = b, a
				}
				ra, ea := ref.Range(runes.Pos{Index: a}, runes.Pos{Index: b})
				rb, eb := got.Range(a, b)
				if (ea == nil) != (eb == nil) || !slices.Equal(ra, rb) {
					t.Fatalf("Range %d..%d: (%q, %v) vs (%q, %v)", a, b, string(ra), ea, string(rb), eb)
				}
				if x, y := ref.Length(runes.Pos{Index: a}, runes.Pos{Index: b}), got.Length(a, b); x != y {
					t.Fatalf("Length %d..%d: %d vs %d", a, b, x, y)
				}
			case 8:
				if ref.Finished() != got.Finished() {
					t.Fatalf("Finished: %v vs %v", ref.Finished(), got.Finished())
				}
			}

			pa, _ := ref.Position()
			pb, _ := got.Position()
			if pa.Index != pb {
				t.Fatalf("position %d vs %d after op %d", pa.Index, pb, op)
			}
		}
	})
}
