package main

import (
	"log"
	"strconv"
	"strings"
	"unicode"

	ebnf "github.com/almerlucke/exbana/v2"
	"github.com/almerlucke/exbana/v2/patterns/alternation"
	"github.com/almerlucke/exbana/v2/patterns/concatenation"
	ent "github.com/almerlucke/exbana/v2/patterns/entity"
	"github.com/almerlucke/exbana/v2/patterns/repetition"
	vec "github.com/almerlucke/exbana/v2/patterns/vector"
	"github.com/almerlucke/exbana/v2/readers/runes"
)

type Writer struct {
	s strings.Builder
}

func (w *Writer) Write(rs ...rune) error {
	for _, r := range rs {
		w.s.WriteRune(r)
	}
	return nil
}

func (w *Writer) String() string {
	return w.s.String()
}

func (w *Writer) Finish() error {
	return nil
}

func runeEq(o1 rune, o2 rune) bool {
	return o1 == o2
}

func runeMatch(r rune) *ent.Entity[rune, runes.Pos] {
	e := ent.New[rune, runes.Pos](func(obj rune) bool {
		return obj == r
	})
	e.SetGenerateFunc(func() rune { return r })
	return e
}

func runeFuncMatch(rf func(rune) bool) *ent.Entity[rune, runes.Pos] {
	return ent.New[rune, runes.Pos](func(obj rune) bool {
		return rf(obj)
	})
}

func randomChoiceGen(choices []rune) func(*ebnf.GenContext) rune {
	return func(ctx *ebnf.GenContext) rune {
		return choices[ctx.Intn(len(choices))]
	}
}

func runeMatch2(r1 rune, r2 rune) *ent.Entity[rune, runes.Pos] {
	e := ent.New[rune, runes.Pos](func(obj rune) bool {
		return obj == r1 || obj == r2
	})
	e.SetGenerateFuncCtx(randomChoiceGen([]rune{r1, r2}))
	return e
}

func randomRuneBetween(ctx *ebnf.GenContext, r1 rune, r2 rune) rune {
	return rune(ctx.Intn(int(r2)+1-int(r1)) + int(r1))
}

func randomRuneBetweenGen(r1 rune, r2 rune) func(*ebnf.GenContext) rune {
	return func(ctx *ebnf.GenContext) rune {
		return randomRuneBetween(ctx, r1, r2)
	}
}

func runeBetween(r1 rune, r2 rune) *ent.Entity[rune, runes.Pos] {
	e := ent.New[rune, runes.Pos](func(obj rune) bool {
		return obj >= r1 && obj <= r2
	})
	e.SetGenerateFuncCtx(randomRuneBetweenGen(r1, r2))
	return e
}

func runeVector(v []rune) *vec.Vector[rune, runes.Pos] {
	return vec.New[rune, runes.Pos](runeEq, v...)
}

func conc(patterns ...ebnf.Pattern[rune, runes.Pos]) *concatenation.Concatenation[rune, runes.Pos] {
	return concatenation.New[rune, runes.Pos](patterns...)
}

func rep(pattern ebnf.Pattern[rune, runes.Pos]) *repetition.Repetition[rune, runes.Pos] {
	return repetition.New[rune, runes.Pos](pattern, 0, 0)
}

func repg(pattern ebnf.Pattern[rune, runes.Pos], maxGen int) *repetition.Repetition[rune, runes.Pos] {
	return repetition.New[rune, runes.Pos](pattern, 0, 0).SetMaxGen(maxGen)
}

func alt(patterns ...ebnf.Pattern[rune, runes.Pos]) *alternation.Alternation[rune, runes.Pos] {
	return alternation.New[rune, runes.Pos](patterns...)
}

func opt(pattern ebnf.Pattern[rune, runes.Pos]) *repetition.Repetition[rune, runes.Pos] {
	return repetition.New[rune, runes.Pos](pattern, 0, 1)
}

func randomHexDigit(ctx *ebnf.GenContext) rune {
	switch ctx.Intn(3) {
	case 0:
		return randomRuneBetween(ctx, '0', '9')
	case 1:
		return randomRuneBetween(ctx, 'A', 'F')
	case 2:
		return randomRuneBetween(ctx, 'a', 'f')
	}

	return 0
}

func randUnicode(ctx *ebnf.GenContext, table *unicode.RangeTable) rune {
	r16len := len(table.R16)
	r32len := len(table.R32)
	index := ctx.Intn(r16len + r32len)
	if index >= r16len {
		r := table.R32[index-r16len]
		return rune(r.Lo + r.Stride*uint32(ctx.Intn(int((r.Hi-r.Lo)/r.Stride+1))))
	}

	r := table.R16[index]
	return rune(r.Lo + r.Stride*uint16(ctx.Intn(int((r.Hi-r.Lo)/r.Stride+1))))
}

func randUnicodeDigit(ctx *ebnf.GenContext) rune {
	if ctx.Intn(10) > 7 {
		return randUnicode(ctx, unicode.Digit)
	}
	return randomRuneBetween(ctx, '0', '9')
}

func randetter(ctx *ebnf.GenContext) rune {
	if ctx.Intn(10) > 8 {
		return randUnicode(ctx, unicode.Letter)
	}
	if ctx.Intn(10) > 8 {
		return '_'
	}
	if ctx.Intn(2) == 1 {
		return randomRuneBetween(ctx, 'a', 'z')
	}

	return randomRuneBetween(ctx, 'A', 'Z')
}

func main() {
	rd, _ := runes.New(strings.NewReader("_identifier 123 0.23 2e3 tipie 0x7ff"))

	underscore := runeMatch('_')
	dot := runeMatch('.')
	zero := runeMatch('0')

	unicodeDigit := runeFuncMatch(unicode.IsDigit).SetGenerateFuncCtx(randUnicodeDigit)

	letter := runeFuncMatch(func(r rune) bool { return unicode.IsLetter(r) || r == '_' }).SetGenerateFuncCtx(randetter)

	decimalDigit := runeBetween('0', '9')
	binaryDigit := runeMatch2('0', '1')
	octalDigit := runeBetween('0', '7')
	hexDigit := runeFuncMatch(func(r rune) bool {
		return (r >= '0' && r <= '9') || (r >= 'A' && r <= 'F') || (r >= 'a' && r <= 'f')
	}).SetGenerateFuncCtx(randomHexDigit)

	identifier := ebnf.Name(conc(letter, repg(alt(letter, unicodeDigit), 12)), "identifier")

	hexDigits := conc(hexDigit, rep(conc(opt(underscore), hexDigit)))
	hexLit := ebnf.Name(conc(zero, runeMatch2('x', 'X'), opt(underscore), hexDigits), "hexLit")

	octalDigits := conc(octalDigit, rep(conc(opt(underscore), octalDigit)))
	octalLit := ebnf.Name(conc(zero, opt(runeMatch2('o', 'O')), opt(underscore), octalDigits), "octalLit")

	binaryDigits := conc(binaryDigit, rep(conc(opt(underscore), binaryDigit)))
	binaryLit := ebnf.Name(conc(zero, runeMatch2('b', 'B'), opt(underscore), binaryDigits), "binaryLit")

	decimalDigits := conc(decimalDigit, rep(conc(opt(underscore), decimalDigit)))
	decimalLit := ebnf.Name(alt(zero, conc(runeBetween('1', '9'), opt(conc(opt(underscore), decimalDigits)))), "decimalLit")

	intLit := ebnf.Name(alt(decimalLit, binaryLit, octalLit, hexLit), "intLit")

	decimalExponent := conc(runeMatch2('e', 'E'), opt(runeMatch2('+', '-')), decimalDigits)
	decimalFloatLit := ebnf.Name(alt(conc(decimalDigits, dot, opt(decimalDigits), opt(decimalExponent)), conc(decimalDigits, decimalExponent), conc(dot, decimalDigits, opt(decimalExponent))), "decimalFloatLit")

	hexMantissa := alt(conc(opt(underscore), hexDigits, dot, opt(hexDigits)), conc(opt(underscore), hexDigits), conc(dot, hexDigits))
	hexExponent := conc(runeMatch2('p', 'P'), opt(runeMatch2('+', '-')), decimalDigits)
	hexFloatLit := ebnf.Name(conc(zero, runeMatch2('x', 'X'), hexMantissa, hexExponent), "hexFloatLit")

	floatLit := ebnf.Name(alt(decimalFloatLit, hexFloatLit), "floatLit")

	token := alt(identifier, intLit, floatLit)

	results, err := ebnf.Scan[rune, runes.Pos](rd, token)
	if err != nil {
		log.Fatalf("err %v", err)
	}

	for _, result := range results {
		result = result.Unpack()
		s, _ := rd.Range(result.Begin, result.End)
		log.Printf("result %v: %v - pos %d", result.ID(), string(s), result.Begin)
	}

	// a fixed seed makes the generated output reproducible
	ctx := ebnf.NewGenContext(1)

	for range 100 {
		var wr Writer
		_ = floatLit.Generate(ctx, ebnf.Writer[rune](&wr))
		log.Printf("wr %v", wr.String())
		fv, err := strconv.ParseFloat(wr.String(), 64)
		if err != nil {
			log.Fatalf("err %v", err)
		}
		log.Printf("fv %v", fv)
	}

	for range 100 {
		var wr Writer
		_ = intLit.Generate(ctx, ebnf.Writer[rune](&wr))
		log.Printf("wr %v", wr.String())
		i, err := strconv.ParseInt(wr.String(), 0, 64)
		if err != nil {
			log.Fatalf("err %v", err)
		}
		log.Printf("fi %v", i)
	}

	for range 10 {
		var wr Writer
		_ = identifier.Generate(ctx, ebnf.Writer[rune](&wr))
		log.Printf("identifier: %v", wr.String())
	}
}
