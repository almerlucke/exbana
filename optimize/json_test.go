package optimize_test

import (
	"fmt"
	"strings"
	"testing"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/optimize"
	"github.com/almerlucke/exbana/v3/patterns/entity"
	"github.com/almerlucke/exbana/v3/patterns/rule"
	"github.com/almerlucke/exbana/v3/readers/runes"
)

type rng = entity.Range[rune]

// jsonGrammar is JSON written the way the grammar reads, with the default (backtracking) repetitions and alternations.
// Whitespace comes after each token, so that every choice can be made by looking at the next character.
func jsonGrammar() pat {
	digit := between('0', '9')
	oneToNine := between('1', '9')

	ws := many(entity.OneOf[rune, runes.Pos](' ', '\t', '\n', '\r'))

	integer := alt(text("0"), conc(oneToNine, many(digit)))
	fraction := conc(text("."), some(digit))
	exponent := conc(entity.OneOf[rune, runes.Pos]('e', 'E'), maybe(entity.OneOf[rune, runes.Pos]('+', '-')), some(digit))
	number := rule.New[rune, runes.Pos]("number", conc(maybe(text("-")), integer, maybe(fraction), maybe(exponent), ws))

	escape := conc(text(`\`), alt(entity.OneOf[rune, runes.Pos]('"', '\\', '/', 'b', 'f', 'n', 'r', 't'),
		conc(text("u"), between('0', '9'), between('0', '9'), between('0', '9'), between('0', '9'))))
	str := rule.New[rune, runes.Pos]("string", conc(text(`"`), many(alt(entity.NotIn[rune, runes.Pos](rng{Lo: '"', Hi: '"'}, rng{Lo: '\\', Hi: '\\'}), escape)), text(`"`), ws))

	literal := rule.New[rune, runes.Pos]("literal", conc(alt(text("true"), text("false"), text("null")), ws))

	value := rule.Declare[rune, runes.Pos]("value")

	comma := conc(text(","), ws)
	array := rule.New[rune, runes.Pos]("array", conc(text("["), ws, maybe(conc(value, many(conc(comma, value)))), text("]"), ws))

	member := conc(str, text(":"), ws, value)
	object := rule.New[rune, runes.Pos]("object", conc(text("{"), ws, maybe(conc(member, many(conc(comma, member)))), text("}"), ws))

	value.Define(alt(str, number, array, object, literal))

	return conc(ws, value, endP())
}

func endP() pat { return endPattern() }

// jsonDocument makes a document with many small objects in an array
func jsonDocument(n int) string {
	var sb strings.Builder

	sb.WriteString("[")

	for i := range n {
		if i > 0 {
			sb.WriteString(", ")
		}

		fmt.Fprintf(&sb, `{"id": %d, "name": "item %d", "tags": ["a", "b", "c"], "price": %d.5, "ok": true, "none": null}`, i, i, i)
	}

	sb.WriteString("]")

	return sb.String()
}

func parseAll(t testing.TB, g pat, doc string) (bool, int) {
	rd, err := runes.New(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}

	ok, m, err := ebnf.Parse(g, rd)
	if err != nil {
		t.Fatal(err)
	}

	if !ok {
		return false, 0
	}

	return true, m.End.Index
}

func TestJSONGrammarKeepsMeaningAndLosesItsBacktracking(t *testing.T) {
	original, optimized := jsonGrammar(), jsonGrammar()

	res, err := optimize.Optimize[rune, runes.Pos](optimized, optimize.Options{})
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("\n%s", res)

	if blocked := res.Blocked(); len(blocked) != 0 {
		t.Errorf("%d places were not optimized:\n%v", len(blocked), res)
	}

	for _, doc := range []string{
		jsonDocument(50), `[]`, `{}`, ` [ 1 , 2 ] `, `{"a": [1, {"b": null}], "c": "x\"yé"}`, `-0.5e+10`, `"text"`,
		// not JSON
		``, `[1,]`, `{"a" 1}`, `[1 2]`, `tru`, `"open`, `01`, `[1] x`, `{"a":}`,
	} {
		okA, endA := parseAll(t, original, doc)
		okB, endB := parseAll(t, optimized, doc)

		if okA != okB || endA != endB {
			t.Errorf("%.40q: original (%v, %d), optimized (%v, %d)", doc, okA, endA, okB, endB)
		}
	}
}

func BenchmarkJSON(b *testing.B) {
	doc := jsonDocument(2000)

	for _, optimized := range []bool{false, true} {
		g := jsonGrammar()
		name := "backtracking"

		if optimized {
			if _, err := optimize.Optimize[rune, runes.Pos](g, optimize.Options{}); err != nil {
				b.Fatal(err)
			}

			name = "optimized"
		}

		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(doc)))

			for b.Loop() {
				if ok, _ := parseAll(b, g, doc); !ok {
					b.Fatal("no match")
				}
			}
		})
	}
}
