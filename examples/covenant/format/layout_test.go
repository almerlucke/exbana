package format

import (
	"math/rand"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/almerlucke/exbana/v3/examples/covenant/token"
)

// noisy writes the tokens of a program again with other spaces, with line ends where the grammar allows them (after an opening
// bracket, a comma or a binary operator, and before a closing bracket), and with comments, on lines of their own and at the end of
// the lines. It is the same program, with a layout that no one writes, which is what a formatter is for. It says how many comments
// it made.
func noisy(src string, toks []token.Token, rng *rand.Rand) (string, int) {
	var (
		sb       strings.Builder
		comments int
	)

	comment := func() string {
		comments++
		return "// c" + string(rune('a'+rng.Intn(26))) + "  "[:0]
	}

	operand := func(k token.Kind) bool {
		switch k {
		case token.Ident, token.Int, token.Float, token.Text, token.RParen, token.RBracket, token.RBrace, token.True, token.False, token.Self:
			return true
		}

		return false
	}

	isWord := func(c byte) bool {
		return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
	}

	// the patterns of a for are separated by commas, and a line may not end after them
	inFor := false

	// the bounds in the generics of a declaration are separated by plus signs, and a line may not end after them
	inGenerics := false

	for i, t := range toks {
		text := src[t.Begin:t.End]

		switch t.Kind {
		case token.For:
			inFor = true
		case token.In:
			inFor = false
		case token.Lt:
			if i >= 2 && toks[i-1].Kind == token.Ident {
				switch toks[i-2].Kind {
				case token.Fn, token.Struct, token.Enum, token.Trait, token.Type:
					inGenerics = true
				}
			}
		case token.Gt:
			inGenerics = false
		}

		if t.Kind == token.NL {
			if rng.Intn(3) == 0 {
				sb.WriteString(" " + comment())
			}

			sb.WriteString("\n")

			for rng.Intn(5) == 0 {
				sb.WriteString(strings.Repeat(" ", rng.Intn(4)) + comment() + "\n")
			}

			continue
		}

		// the space in front of the token
		if sb.Len() > 0 {
			last := sb.String()[sb.Len()-1]
			first := text[0]
			unsafe := isWord(last) && isWord(first) ||
				strings.IndexByte("-=<>!", last) >= 0 && strings.IndexByte("=>", first) >= 0 ||
				last == ':' && first == ':' || first == ':' && isWord(last) ||
				last == '/' && first == '/' || last == '"' && first == '"'

			switch {
			case last == '\n':
				sb.WriteString(strings.Repeat(" ", rng.Intn(9)))
			case unsafe || rng.Intn(2) == 0:
				sb.WriteString(strings.Repeat(" ", 1+rng.Intn(3)))
			}
		}

		// before a closing bracket, a line may end
		if (t.Kind == token.RParen || t.Kind == token.RBracket || t.Kind == token.RBrace) && i > 0 && toks[i-1].Kind != token.NL && rng.Intn(4) == 0 {
			if rng.Intn(2) == 0 {
				sb.WriteString(" " + comment())
			}

			sb.WriteString("\n")
		}

		sb.WriteString(text)

		// after an opening bracket, a comma or a binary operator, a line may end
		after := false

		switch t.Kind {
		case token.Comma:
			after = !inFor
		case token.Plus:
			after = !inGenerics
		case token.LParen, token.LBracket, token.LBrace, token.Star, token.Slash, token.Percent,
			token.Eq, token.Ne, token.Le, token.Ge, token.Lt, token.And, token.Or:
			after = true
		case token.Minus:
			after = i > 0 && operand(toks[i-1].Kind)
		}

		if after && i+1 < len(toks) && toks[i+1].Kind != token.NL && rng.Intn(4) == 0 {
			if rng.Intn(2) == 0 {
				sb.WriteString(" " + comment())
			}

			sb.WriteString("\n")
		}
	}

	return sb.String(), comments
}

func TestAnyLayoutOfAProgram(t *testing.T) {
	// LAYOUTS and SEED are for looking harder than a test should: LAYOUTS=3000 SEED=5 go test -run AnyLayout
	layouts, seed := 40, int64(11)

	if n, err := strconv.Atoi(os.Getenv("LAYOUTS")); err == nil {
		layouts = n
	}

	if n, err := strconv.ParseInt(os.Getenv("SEED"), 10, 64); err == nil {
		seed = n
	}

	rng := rand.New(rand.NewSource(seed))
	programs, runs := 0, 0

	for name, src := range corpus(t) {
		toks, _, err := token.LexComments(src)
		if err != nil {
			continue
		}

		programs++

		for range layouts {
			text, comments := noisy(src, toks, rng)

			_, _, _, err := write(text)
			if err != nil {
				t.Fatalf("%s: the test wrote a program that does not parse (%v):\n%s", name, err, text)
			}

			out, err := Source(text)
			if err != nil {
				t.Fatalf("%s: %v\n%s", name, err, text)
			}

			_, found, _ := token.LexComments(out)
			if got := len(found); got != comments {
				t.Fatalf("%s: %d comments in, %d out:\n%s\n%s", name, comments, got, text, out)
			}

			again, err := Source(out)
			if err != nil || again != out {
				t.Fatalf("%s: not stable (%v):\n%s\n-- and then:\n%s\n-- from:\n%s", name, err, out, again, text)
			}

			runs++
		}
	}

	t.Logf("%d programs, %d layouts", programs, runs)
}
