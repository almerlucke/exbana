package main

import (
	"fmt"
	"log"
	"reflect"
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

// Helper functions for common patterns
func objToPattern(obj any) ebnf.Pattern[rune, runes.Pos] {
	if r, ok := obj.(rune); ok {
		return rm(r)
	}
	if p, ok := obj.(ebnf.Pattern[rune, runes.Pos]); ok {
		return p
	}
	panic(fmt.Sprintf("invalid object type %v", reflect.TypeOf(obj)))
}

func objsToPatterns(objs ...any) []ebnf.Pattern[rune, runes.Pos] {
	patterns := make([]ebnf.Pattern[rune, runes.Pos], len(objs))
	for i, obj := range objs {
		patterns[i] = objToPattern(obj)
	}
	return patterns
}

func req(o1 rune, o2 rune) bool { return o1 == o2 }

func ra() *ent.Entity[rune, runes.Pos] {
	e := ent.New[rune, runes.Pos](func(obj rune) bool { return true })
	e.SetEvalFunc(func(m *ebnf.Match[rune, runes.Pos], r ebnf.Reader[rune, runes.Pos]) (any, error) {
		return string(m.Value.([]rune)), nil
	})
	return e
}

func rm(r rune) *ent.Entity[rune, runes.Pos] {
	e := ent.New[rune, runes.Pos](func(obj rune) bool { return obj == r })
	e.SetGenerateFunc(func() rune { return r })
	e.SetEvalFunc(func(m *ebnf.Match[rune, runes.Pos], r ebnf.Reader[rune, runes.Pos]) (any, error) {
		return string(m.Value.([]rune)), nil
	})
	return e
}

func rb(r1 rune, r2 rune) *ent.Entity[rune, runes.Pos] {
	e := rbOriginal(r1, r2)
	e.SetEvalFunc(func(m *ebnf.Match[rune, runes.Pos], r ebnf.Reader[rune, runes.Pos]) (any, error) {
		return string(m.Value.([]rune)), nil
	})
	return e
}

func rbOriginal(r1 rune, r2 rune) *ent.Entity[rune, runes.Pos] {
	return ent.New[rune, runes.Pos](func(obj rune) bool { return obj >= r1 && obj <= r2 })
}

func rv(s string) *vec.Vector[rune, runes.Pos] {
	v := vec.New[rune, runes.Pos](req, []rune(s)...)
	v.SetEvalFunc(func(m *ebnf.Match[rune, runes.Pos], r ebnf.Reader[rune, runes.Pos]) (any, error) {
		return s, nil
	})
	return v
}

func conc(objs ...any) *concatenation.Concatenation[rune, runes.Pos] {
	return concatenation.New[rune, runes.Pos](objsToPatterns(objs...)...)
}

func rep(obj any) *repetition.Repetition[rune, runes.Pos] {
	r := repetition.New[rune, runes.Pos](objToPattern(obj), 0, 0).SetPossessive(true)
	r.SetEvalFunc(func(m *ebnf.Match[rune, runes.Pos], reader ebnf.Reader[rune, runes.Pos]) (any, error) {
		var sb strings.Builder
		for _, c := range m.Components {
			val, err := c.Eval(reader)
			if err != nil {
				return nil, err
			}
			if s, ok := val.(string); ok {
				sb.WriteString(s)
			}
		}
		return sb.String(), nil
	})
	return r
}

func atLeast1(obj any) *repetition.Repetition[rune, runes.Pos] {
	r := repetition.New[rune, runes.Pos](objToPattern(obj), 1, 0).SetPossessive(true)
	r.SetEvalFunc(func(m *ebnf.Match[rune, runes.Pos], reader ebnf.Reader[rune, runes.Pos]) (any, error) {
		var sb strings.Builder
		for _, c := range m.Components {
			val, err := c.Eval(reader)
			if err != nil {
				return nil, err
			}
			if s, ok := val.(string); ok {
				sb.WriteString(s)
			}
		}
		return sb.String(), nil
	})
	return r
}

func alt(objs ...any) *alternation.Alternation[rune, runes.Pos] {
	return alternation.New[rune, runes.Pos](objsToPatterns(objs...)...)
}

func opt(obj any) *repetition.Repetition[rune, runes.Pos] {
	return repetition.New[rune, runes.Pos](objToPattern(obj), 0, 1).SetPossessive(true)
}

func ex(must any, exception any) *except.Except[rune, runes.Pos] {
	return except.New[rune, runes.Pos](objToPattern(must), objToPattern(exception))
}

// XML data structures
type Attr struct {
	Name  string
	Value string
}

type Node struct {
	Name     string
	Attrs    []Attr
	Children []*Node
	Content  string
}

// Evaluators
func evalName(m *ebnf.Match[rune, runes.Pos], r ebnf.Reader[rune, runes.Pos]) (any, error) {
	rs, err := r.Range(m.Begin, m.End)
	if err != nil {
		return nil, err
	}
	return string(rs), nil
}

func evalAttrValue(m *ebnf.Match[rune, runes.Pos], r ebnf.Reader[rune, runes.Pos]) (any, error) {
	rs, err := r.Range(m.Begin, m.End)
	if err != nil {
		return nil, err
	}
	return string(rs), nil
}

func evalAttr(m *ebnf.Match[rune, runes.Pos], r ebnf.Reader[rune, runes.Pos]) (any, error) {
	name, err := m.Components[0].Eval(r)
	if err != nil {
		return nil, err
	}
	value, err := m.Components[3].Eval(r)
	if err != nil {
		return nil, err
	}
	return Attr{Name: name.(string), Value: value.(string)}, nil
}

func evalTag(m *ebnf.Match[rune, runes.Pos], r ebnf.Reader[rune, runes.Pos]) (any, error) {
	// m is an Alternation match, so it has one component which is the selected choice match
	choiceMatch := m.Components[0]

	nameVal, err := choiceMatch.Components[1].Eval(r)
	if err != nil {
		return nil, err
	}
	name := nameVal.(string)

	var attrs []Attr
	// choiceMatch.Components[2] is rep(conc(ws, attr))
	for _, c := range choiceMatch.Components[2].Components {
		// c is conc(ws, attr)
		attrVal, err := c.Components[1].Eval(r)
		if err != nil {
			return nil, err
		}
		attrs = append(attrs, attrVal.(Attr))
	}

	// Check if it's a self-closing tag or has content
	// Choice 0: conc('<', name, attrs, ws, rv("/>"))
	// Choice 1: conc('<', name, attrs, ws, '>', content, rv("</"), name, ws, '>')
	if len(choiceMatch.Components) < 6 {
		return &Node{Name: name, Attrs: attrs}, nil
	}

	contentMatch := choiceMatch.Components[5]
	var children []*Node
	var sb strings.Builder

	contentComponents, _ := contentMatch.Eval(r)
	for _, c := range contentComponents.([]*ebnf.Match[rune, runes.Pos]) {
		// c is alt(tag, text)
		// it has one component
		innerMatch := c.Components[0]
		val, err := innerMatch.Eval(r)
		if err != nil {
			return nil, err
		}

		if val != nil {
			if node, ok := val.(*Node); ok {
				children = append(children, node)
			} else if s, ok := val.(string); ok {
				sb.WriteString(s)
			}
		}
	}

	return &Node{
		Name:     name,
		Attrs:    attrs,
		Children: children,
		Content:  sb.String(),
	}, nil
}

func evalText(m *ebnf.Match[rune, runes.Pos], r ebnf.Reader[rune, runes.Pos]) (any, error) {
	rs, err := r.Range(m.Begin, m.End)
	if err != nil {
		return nil, err
	}
	return string(rs), nil
}

func main() {
	// Define Grammar
	ws := rep(ent.New[rune, runes.Pos](unicode.IsSpace))
	nameChar := alt(rb('a', 'z'), rb('A', 'Z'), rb('0', '9'), '_', '-')
	name := atLeast1(nameChar).SetEvalFunc(evalName)

	attrValue := ex(ra(), '"').SetEvalFunc(evalAttrValue)
	// attr: conc(name, '=', '"', rep(attrValue), '"')
	// index: 0: name, 1: '=', 2: '"', 3: rep, 4: '"'
	attr := conc(name, '=', '"', rep(attrValue), '"').SetEvalFunc(evalAttr)

	attrs := rep(conc(ws, attr))

	// forward tag declaration
	tag := conc()
	tag.SetEvalFunc(func(m *ebnf.Match[rune, runes.Pos], reader ebnf.Reader[rune, runes.Pos]) (any, error) {
		if len(m.Components) > 0 {
			return m.Components[0].Eval(reader)
		}
		return nil, nil
	})

	text := atLeast1(ex(ra(), '<')).SetEvalFunc(evalText)

	// content: rep(alt(tag, text))
	content := rep(alt(tag, text))
	content.SetEvalFunc(func(m *ebnf.Match[rune, runes.Pos], reader ebnf.Reader[rune, runes.Pos]) (any, error) {
		return m.Components, nil
	})

	// tag: alt(...)
	// Choice 0: conc('<', name, attrs, ws, rv("/>"))
	// Index:     0: <, 1: name, 2: attrs, 3: ws, 4: />
	// Choice 1: conc('<', name, attrs, ws, '>', content, rv("</"), name, ws, '>')
	// Index:     0: <, 1: name, 2: attrs, 3: ws, 4: >, 5: content, 6: </, 7: name, 8: ws, 9: >
	tag.SetPatterns(ebnf.Patterns[rune, runes.Pos]{
		alt(
			conc('<', name, attrs, ws, rv("/>")),
			conc('<', name, attrs, ws, '>', content, rv("</"), name, ws, '>'),
		).SetEvalFunc(evalTag),
	})

	xmlGrammar := conc(ws, tag, ws)

	// Test strings
	inputs := []string{
		`<root id="123" type="example">
			<item name="foo">Hello</item>
			<item name="bar" empty="true"/>
			<nested>
				<child>Deep</child>
			</nested>
		</root>`,
	}

	for _, input := range inputs {
		fmt.Printf("Input:\n%s\n", input)
		rd, err := runes.New(strings.NewReader(input))
		if err != nil {
			log.Fatalf("Reader error: %v", err)
		}

		ok, match, err := xmlGrammar.Match(rd)
		if err != nil {
			log.Fatalf("Match error: %v", err)
		}

		if !ok || match == nil {
			log.Fatalf("No match found")
		}

		// xmlGrammar: conc(ws, tag, ws)
		// index: 0: ws, 1: tag, 2: ws
		tagMatch := match.Components[1]
		val, err := tagMatch.Components[0].Eval(rd)
		if err != nil {
			log.Fatalf("Eval error: %v", err)
		}

		node := val.(*Node)
		printNode(node, 0)
	}
}

func printNode(n *Node, indent int) {
	ins := strings.Repeat("  ", indent)
	attrStr := ""
	for _, a := range n.Attrs {
		attrStr += fmt.Sprintf(" %s=%q", a.Name, a.Value)
	}

	if len(n.Children) == 0 && n.Content == "" {
		fmt.Printf("%s<%s%s/>\n", ins, n.Name, attrStr)
		return
	}

	fmt.Printf("%s<%s%s>\n", ins, n.Name, attrStr)
	trimmedContent := strings.TrimSpace(n.Content)
	if trimmedContent != "" {
		fmt.Printf("%s  TEXT: %q\n", ins, trimmedContent)
	}
	for _, child := range n.Children {
		printNode(child, indent+1)
	}
	fmt.Printf("%s</%s>\n", ins, n.Name)
}
