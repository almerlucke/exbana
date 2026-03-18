package main

import (
	"encoding/json"
	"fmt"
	"log"
	"reflect"
	"strconv"
	"strings"
	"unicode"

	ebnf "github.com/almerlucke/exbana/v2"
	"github.com/almerlucke/exbana/v2/patterns/alternation"
	"github.com/almerlucke/exbana/v2/patterns/concatenation"
	ent "github.com/almerlucke/exbana/v2/patterns/entity"
	"github.com/almerlucke/exbana/v2/patterns/except"
	"github.com/almerlucke/exbana/v2/patterns/repetition"
	vec "github.com/almerlucke/exbana/v2/patterns/vector"
	"github.com/almerlucke/exbana/v2/readers/runes"
)

func objToPattern(obj any) ebnf.Pattern[rune, runes.Pos] {
	if r, ok := obj.(rune); ok {
		return rm(r)
	}
	if p, ok := obj.(ebnf.Pattern[rune, runes.Pos]); ok {
		return p
	}
	panic(fmt.Sprintf("invalid object type %v", reflect.TypeOf(obj)))
	return nil
}

func objsToPatterns(objs ...any) []ebnf.Pattern[rune, runes.Pos] {
	patterns := make([]ebnf.Pattern[rune, runes.Pos], len(objs))
	for i, obj := range objs {
		patterns[i] = objToPattern(obj)
	}
	return patterns
}

func req(o1 rune, o2 rune) bool {
	return o1 == o2
}

func ra() *ent.Entity[rune, runes.Pos] {
	return ent.New[rune, runes.Pos](func(obj rune) bool { return true })
}

func stringContains(s string) *ent.Entity[rune, runes.Pos] {
	return ent.New[rune, runes.Pos](func(obj rune) bool {
		return strings.ContainsRune(s, obj)
	})
}

func rm(r rune) *ent.Entity[rune, runes.Pos] {
	e := ent.New[rune, runes.Pos](func(obj rune) bool {
		return obj == r
	})
	e.SetGenerateFunc(func() rune { return r })
	return e
}

func rmf(rf func(rune) bool) *ent.Entity[rune, runes.Pos] {
	return ent.New[rune, runes.Pos](func(obj rune) bool {
		return rf(obj)
	})
}

func rm2(r1 rune, r2 rune) *ent.Entity[rune, runes.Pos] {
	return ent.New[rune, runes.Pos](func(obj rune) bool {
		return obj == r1 || obj == r2
	})
}

func rb(r1 rune, r2 rune) *ent.Entity[rune, runes.Pos] {
	return ent.New[rune, runes.Pos](func(obj rune) bool {
		return obj >= r1 && obj <= r2
	})
}

func rv(v []rune) *vec.Vector[rune, runes.Pos] {
	return vec.New[rune, runes.Pos](req, v...)
}

func conc(objs ...any) *concatenation.Concatenation[rune, runes.Pos] {
	return concatenation.New[rune, runes.Pos](objsToPatterns(objs...)...)
}

func rep(obj any) *repetition.Repetition[rune, runes.Pos] {
	return repetition.New[rune, runes.Pos](objToPattern(obj), 0, 0).SetPossessive(true)
}

func n(obj any, n int) *repetition.Repetition[rune, runes.Pos] {
	return repetition.New[rune, runes.Pos](objToPattern(obj), n, n)
}

func atLeast1(obj any) *repetition.Repetition[rune, runes.Pos] {
	return repetition.New[rune, runes.Pos](objToPattern(obj), 1, 0).SetPossessive(true)
}

func alt(objs ...any) *alternation.Alternation[rune, runes.Pos] {
	return alternation.New[rune, runes.Pos](objsToPatterns(objs...)...)
}

func opt(obj any) *repetition.Repetition[rune, runes.Pos] {
	return repetition.New[rune, runes.Pos](objToPattern(obj), 0, 1).SetPossessive(true)
}

func ex(must any, ex any) *except.Except[rune, runes.Pos] {
	return except.New[rune, runes.Pos](objToPattern(must), objToPattern(ex))
}

func evalNumber(m *ebnf.Match[rune, runes.Pos], r ebnf.Reader[rune, runes.Pos]) (any, error) {
	rs, err := r.Range(m.Begin, m.End)
	if err != nil {
		return nil, err
	}

	fl, err := strconv.ParseFloat(string(rs), 64)
	if err != nil {
		return nil, err
	}

	return fl, nil
}

func evalString(m *ebnf.Match[rune, runes.Pos], r ebnf.Reader[rune, runes.Pos]) (any, error) {
	var strBuilder strings.Builder

	var escapedRuneMap = map[rune]rune{
		'"': '"', '\\': '\\', '/': '/', 'b': '\b', 'f': '\f', 'n': '\n', 'r': '\r', 't': '\t',
	}

	charMatches := m.Components[1].Components
	for _, charMatch := range charMatches {
		charMatch = charMatch.Unpack()
		if charMatch.ID() == "escape" {
			charMatch = charMatch.Components[1].Unpack()
			charRange, err := r.Range(charMatch.Begin, charMatch.End)
			if err != nil {
				return nil, err
			}
			if charMatch.ID() == "unicodeLit" {
				char, err := strconv.ParseInt(string(charRange[1:]), 16, 32)
				if err != nil {
					return nil, err
				}
				strBuilder.WriteRune(rune(char))
			} else {
				strBuilder.WriteRune(escapedRuneMap[charRange[0]])
			}
		} else {
			charRange, err := r.Range(charMatch.Begin, charMatch.End)
			if err != nil {
				return nil, err
			}
			strBuilder.WriteRune(charRange[0])
		}
	}

	return strBuilder.String(), nil
}

func evalTrue(m *ebnf.Match[rune, runes.Pos], r ebnf.Reader[rune, runes.Pos]) (any, error) {
	return true, nil
}

func evalFalse(m *ebnf.Match[rune, runes.Pos], r ebnf.Reader[rune, runes.Pos]) (any, error) {
	return false, nil
}

func evalNull(m *ebnf.Match[rune, runes.Pos], r ebnf.Reader[rune, runes.Pos]) (any, error) {
	return nil, nil
}

func evalValue(m *ebnf.Match[rune, runes.Pos], r ebnf.Reader[rune, runes.Pos]) (any, error) {
	return m.Components[1].Unpack().Eval(r)
}

func evalArray(m *ebnf.Match[rune, runes.Pos], r ebnf.Reader[rune, runes.Pos]) (any, error) {
	var array []any

	// check for whitespace
	comp := m.Components[1].Unpack()
	if comp.ID() == "whitespace" {
		return array, nil
	}

	// otherwise get first value from conc(value, rep(conc(',', value)))
	value, err := comp.Components[0].Eval(r)
	if err != nil {
		return nil, err
	}

	array = append(array, value)

	// get remaining values from rep(conc(',' value))
	for _, concComp := range comp.Components[1].Components {
		value, err = concComp.Components[1].Eval(r)
		if err != nil {
			return nil, err
		}

		array = append(array, value)
	}

	return array, nil
}

func evalObject(m *ebnf.Match[rune, runes.Pos], r ebnf.Reader[rune, runes.Pos]) (any, error) {
	obj := map[string]any{}

	// check for whitespace
	comp := m.Components[1].Unpack()
	if comp.ID() == "whitespace" {
		return obj, nil
	}

	str, err := comp.Components[1].Eval(r)
	if err != nil {
		return nil, err
	}

	value, err := comp.Components[4].Eval(r)
	if err != nil {
		return nil, err
	}

	obj[str.(string)] = value

	for _, concComp := range comp.Components[5].Components {
		str, err = concComp.Components[2].Eval(r)
		if err != nil {
			return nil, err
		}

		value, err = concComp.Components[5].Eval(r)
		if err != nil {
			return nil, err
		}

		obj[str.(string)] = value
	}

	return obj, nil
}

func main() {
	jsonStr := `{ "foo": "bar\u231834", "test": 2.3, "hallo": [1, 2, 3, "test", null, true, false, [ ]] }`
	rd, _ := runes.New(strings.NewReader(jsonStr))

	logger := ebnf.NewStackLog[rune, runes.Pos]()

	ws := rep(stringContains(" \t\n\r")).SetID("whitespace")

	digit := rb('0', '9')
	fraction := conc('.', atLeast1(digit))
	exponent := conc(rm2('e', 'E'), opt(rm2('+', '-')), atLeast1(digit))
	number := conc(
		opt('-'),
		alt('0', conc(rb('1', '9'), rep(digit))),
		opt(fraction),
		opt(exponent),
	).SetID("number").SetLogger(logger).SetEvalFunc(evalNumber)

	hexDigit := ent.New[rune, runes.Pos](func(r rune) bool {
		return r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F'
	})
	uniLit := conc('u', n(hexDigit, 4)).SetID("unicodeLit")
	escape := conc('\\', alt('"', '\\', '/', 'b', 'f', 'n', 'r', 't', uniLit)).SetID("escape")
	anyChar := ex(ra(), alt('"', '\\', rmf(unicode.IsControl)))
	str := conc('"', rep(alt(anyChar, escape)), '"').SetID("string").SetLogger(logger).SetEvalFunc(evalString)

	trueVal := rv([]rune{'t', 'r', 'u', 'e'}).SetID("true").SetLogger(logger).SetEvalFunc(evalTrue)
	falseVal := rv([]rune{'f', 'a', 'l', 's', 'e'}).SetID("false").SetLogger(logger).SetEvalFunc(evalFalse)
	nullVal := rv([]rune{'n', 'u', 'l', 'l'}).SetID("null").SetLogger(logger).SetEvalFunc(evalNull)

	// forward value declaration
	value := conc()

	array := conc(
		'[',
		alt(conc(value, rep(conc(',', value))), ws),
		']',
	).SetID("array").SetLogger(logger).SetEvalFunc(evalArray)

	obj := conc(
		'{',
		alt(conc(ws, str, ws, ':', value, rep(conc(',', ws, str, ws, ':', value))), ws),
		'}',
	).SetID("object").SetLogger(logger).SetEvalFunc(evalObject)

	value.SetPatterns([]ebnf.Pattern[rune, runes.Pos]{ws, alt(str, number, obj, array, trueVal, falseVal, nullVal).SetOrthogonal(true), ws})
	value.SetID("value").SetEvalFunc(evalValue)

	if ok, match, err := value.Match(rd); ok && match != nil {
		val, err := match.Eval(rd)
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("value: %v", val)
		//log.Printf("match %v: %v", match.Pattern.ID(), val)
		//realValue := match.Components[1].Unpack()
		//rs, _ := rd.Range(realValue.Begin, realValue.End)
		//log.Printf("match %v: %v", realValue.ID(), string(rs))
	} else {
		if err != nil {
			log.Fatal(err)
		}
		longestMatch := logger.LongestMatch()
		if longestMatch != nil {
			rng, _ := rd.Range(longestMatch.Begin, longestMatch.End)
			if len(rng) > 10 {
				rng = rng[len(rng)-10:]
			}
			log.Printf("failed to parse %s -> pos %d, line %d, failed at %s",
				longestMatch.Pattern.ID(),
				longestMatch.End.Col+1,
				longestMatch.End.Line+1,
				string(rng))
		}
		log.Printf("no match")
	}

	var jsonObj map[string]any

	err := json.Unmarshal([]byte(jsonStr), &jsonObj)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("jsonObj: %v", jsonObj)
}
