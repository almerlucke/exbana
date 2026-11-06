package token

import (
	"math/rand"
	"slices"
	"strings"
	"testing"
)

func kinds(t *testing.T, src string) []Kind {
	t.Helper()

	tokens, err := Lex(src)
	if err != nil {
		t.Fatalf("%q: %v", src, err)
	}

	out := make([]Kind, len(tokens))
	for i, tk := range tokens {
		out[i] = tk.Kind
	}

	return out
}

func TestKinds(t *testing.T) {
	for src, want := range map[string][]Kind{
		"let x: Int = 0xff\n":       {Let, Label, Ident, Assign, Int, NL},
		"x = 1.5e3":                 {Ident, Assign, Float, NL},
		"a->b => c == d != e":       {Ident, Arrow, Ident, FatArrow, Ident, Eq, Ident, Ne, Ident, NL},
		"a <= b >= c < d > e":       {Ident, Le, Ident, Ge, Ident, Lt, Ident, Gt, Ident, NL},
		"List<List<Int>>":           {Ident, Lt, Ident, Lt, Ident, Gt, Gt, NL}, // > > is two tokens: there is no shift
		"a::b::c":                   {Ident, ColonColon, Ident, ColonColon, Ident, NL},
		"(){}[],:.":                 {LParen, RParen, LBrace, RBrace, LBracket, RBracket, Comma, Colon, Dot, NL},
		"+ - * / %":                 {Plus, Minus, Star, Slash, Percent, NL},
		"fn Fn fnx _ _x":            {Fn, FnType, Ident, Underscore, Ident, NL},
		`"a" "b\"c\\d\n\t\r"`:       {Text, Text, NL},
		"a // comment\nb":           {Ident, NL, Ident, NL},
		"a\r\nb\r\n":                {Ident, NL, Ident, NL},
		"\n\n  a  \n\n\n  b \n\n\n": {Ident, NL, Ident, NL},
		"// only a comment\n":       nil,
		"":                          nil,
		"\n\n":                      nil,
		"1.max(2)":                  {Int, Dot, Ident, LParen, Int, RParen, NL},
		"1.e3":                      {Int, Dot, Ident, NL}, // a member of 1
	} {
		if got := kinds(t, src); !slices.Equal(got, want) {
			t.Errorf("%q\n got %v\nwant %v", src, got, want)
		}
	}
}

func TestLabels(t *testing.T) {
	for src, want := range map[string][]Kind{
		"x: 1":              {Label, Int, NL},
		"x : 1":             {Ident, Colon, Int, NL}, // a space in front of the colon: no label
		"x:1":               {Label, Int, NL},
		"a::b":              {Ident, ColonColon, Ident, NL},
		"a.b: 1":            {Ident, Dot, Ident, Colon, Int, NL}, // a member in front of a colon is an expression
		"Color::Red: 1":     {Ident, ColonColon, Ident, Colon, Int, NL},
		"self: 1":           {Self, Colon, Int, NL}, // a keyword is not a name
		`"a": 1`:            {Text, Colon, Int, NL},
		"(x): 1":            {LParen, Ident, RParen, Colon, Int, NL},
		"f(x: 1, y: 2)":     {Ident, LParen, Label, Int, Comma, Label, Int, RParen, NL},
		"{x: 1}":            {LBrace, Label, Int, RBrace, NL},
		"fn f(a: Int) -> T": {Fn, Ident, LParen, Label, Ident, RParen, Arrow, Ident, NL},
	} {
		if got := kinds(t, src); !slices.Equal(got, want) {
			t.Errorf("%q\n got %v\nwant %v", src, got, want)
		}
	}
}

func TestKeywordsAreTheirOwnKinds(t *testing.T) {
	seen := map[Kind]string{}

	for _, word := range Keywords() {
		got := kinds(t, word)
		if len(got) != 2 || got[0] == Ident || got[1] != NL {
			t.Errorf("%q lexes as %v", word, got)
			continue
		}

		if other, dup := seen[got[0]]; dup {
			t.Errorf("%q and %q are the same kind", word, other)
		}

		seen[got[0]] = word

		if got[0].String() != word {
			t.Errorf("%q is called %q", word, got[0])
		}

		// a word that goes on is a name
		if got := kinds(t, word+"x"); got[0] != Ident {
			t.Errorf("%qx lexes as %v", word, got)
		}
	}
}

func TestNumbers(t *testing.T) {
	for src, kind := range map[string]Kind{
		"0": Int, "7": Int, "42": Int, "1_000_000": Int, "0xff": Int, "0xde_ad_be_ef": Int, "0b1010": Int, "0b1010_0101": Int,
		"0x0": Int, "0b0": Int, "1.5": Float, "0.5": Float, "1_000.000_1": Float, "1e9": Float, "2e0": Float, "2.5e-3": Float,
		"6.02e+23": Float, "10.0e10": Float, "0e1": Float,
	} {
		tokens, err := Lex(src)
		if err != nil || len(tokens) != 2 || tokens[0].Kind != kind || tokens[0].End-tokens[0].Begin != len(src) {
			t.Errorf("%s: %v, %v", src, tokens, err)
		}
	}

	for _, src := range []string{
		"007", "00", "1_", "1__0", "0x", "0xFF", "0x_ff", "0b", "0b102", "0b2", "1e", "1e+", "1E9", "1_e3", "1e_3", "12abc",
		"0xfg", "1.5x", "0_1", "1.5e3x",
	} {
		if tokens, err := Lex(src); err == nil {
			t.Errorf("%s lexes, as %v", src, tokens)
		}
	}

	// not numbers, but not wrong either: the grammar has no place for them
	for src, want := range map[string][]Kind{
		"1.":    {Int, Dot, NL},
		".5":    {Dot, Int, NL},
		"1.5.2": {Float, Dot, Int, NL},
		"1 2":   {Int, Int, NL},
	} {
		if got := kinds(t, src); !slices.Equal(got, want) {
			t.Errorf("%q\n got %v\nwant %v", src, got, want)
		}
	}
}

func TestErrors(t *testing.T) {
	for src, want := range map[string]string{
		"a;":           "semicolons",
		"!a":           "!=",
		"a ! b":        "!=",
		"é":            "ASCII",
		"a @ b":        "unexpected",
		`"abc`:         "not closed",
		"\"abc\ndef\"": "ends on the line",
		`"a\qb"`:       "escape",
		`"a\`:          "escape",
		"a\rb":         "unexpected",
	} {
		_, err := Lex(src)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want something about %q", src, err, want)
		}
	}
}

func TestTokensCoverTheSource(t *testing.T) {
	src := "fn f(a: Int) -> Int {\r\n    return a // done\r\n}\r\n"

	tokens, err := Lex(src)
	if err != nil {
		t.Fatal(err)
	}

	var parts []string
	for _, tk := range tokens {
		parts = append(parts, strings.ReplaceAll(src[tk.Begin:tk.End], "\r\n", "\\n"))
	}

	// the line end after a comment is only the \n: the comment took the \r
	want := []string{"fn", "f", "(", "a:", "Int", ")", "->", "Int", "{", "\\n", "return", "a", "\n", "}", "\\n"}
	if !slices.Equal(parts, want) {
		t.Errorf("got %q\nwant %q", parts, want)
	}
}

func TestPosition(t *testing.T) {
	src := "ab\ncd\n\nef"

	for offset, want := range map[int][2]int{0: {1, 1}, 1: {1, 2}, 2: {1, 3}, 3: {2, 1}, 4: {2, 2}, 6: {3, 1}, 7: {4, 1}, 9: {4, 3}, 100: {4, 3}, -5: {1, 1}} {
		if line, col := Position(src, offset); [2]int{line, col} != want {
			t.Errorf("offset %d is %d:%d, want %v", offset, line, col, want)
		}
	}

	// columns count characters
	if _, col := Position("\"é\" x", 5); col != 5 {
		t.Errorf("column %d", col)
	}
}

// Whatever the source is, the tokens are in order, do not overlap, and end with a line end.
func checkTokens(t *testing.T, src string) {
	t.Helper()

	tokens, err := Lex(src)
	if err != nil {
		return
	}

	prev := 0

	for i, tk := range tokens {
		if tk.Kind <= 0 || tk.Kind >= numKinds || tk.Begin < prev || tk.End < tk.Begin || tk.End > len(src) {
			t.Fatalf("%q: token %d is %+v after %d", src, i, tk, prev)
		}

		if tk.Kind == NL && i > 0 && tokens[i-1].Kind == NL {
			t.Fatalf("%q: two line ends in a row", src)
		}

		prev = tk.End
	}

	if n := len(tokens); n > 0 && (tokens[n-1].Kind != NL || tokens[0].Kind == NL) {
		t.Fatalf("%q: starts or ends wrong: %v", src, tokens)
	}
}

func FuzzLex(f *testing.F) {
	for _, s := range []string{"let x: Int = 0xff\n", "fn f() {\r\n}\r\n", "\"a\\n\"", "1_000.5e-3", "a::b.c: d", "// x\n\n"} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, src string) { checkTokens(t, src) })
}

func TestRandomSources(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	alphabet := []string{"a", "x", "fn", "let", "0", "1", "_", ".", ":", "::", "\"", "\\", "\n", "\r\n", " ", "//", "e", "0x", "0b", "-", ">", "=", "é", ";", "{", "}"}

	for range 5000 {
		var sb strings.Builder
		for range rng.Intn(12) {
			sb.WriteString(alphabet[rng.Intn(len(alphabet))])
		}

		checkTokens(t, sb.String())
	}
}

func TestCommentsAreReportedAndTokensAreTheSame(t *testing.T) {
	src := "// a  \nlet x = 1 // b\t\r\n\n  //c\n// \"not text\"\nlet s = \"// not a comment\"\n"

	tokens, comments, err := LexComments(src)
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, c := range comments {
		got = append(got, src[c.Begin:c.End])
	}

	want := []string{"// a", "// b", "//c", `// "not text"`}
	if !slices.Equal(got, want) {
		t.Errorf("comments %q, want %q", got, want)
	}

	plain, err := Lex(src)
	if err != nil || !slices.Equal(plain, tokens) {
		t.Errorf("Lex gives other tokens than LexComments: %v", err)
	}
}
