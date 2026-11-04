package main

import (
	"fmt"
	"log"
	"reflect"
	"strings"
	"unicode"

	ebnf "github.com/almerlucke/exbana/v3"
	"github.com/almerlucke/exbana/v3/patterns/alternation"
	"github.com/almerlucke/exbana/v3/patterns/concatenation"
	ent "github.com/almerlucke/exbana/v3/patterns/entity"
	"github.com/almerlucke/exbana/v3/patterns/except"
	"github.com/almerlucke/exbana/v3/patterns/repetition"
	"github.com/almerlucke/exbana/v3/patterns/rule"
	vec "github.com/almerlucke/exbana/v3/patterns/vector"
	"github.com/almerlucke/exbana/v3/readers/runes"
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

type rl = *rule.Rule[rune, runes.Pos]

// anon wraps a pattern in a rule without a name, to give it an eval function
func anon(p ebnf.Pattern[rune, runes.Pos]) rl {
	return rule.New("", p)
}

func evalRunes(m *ebnf.Match[rune, runes.Pos], r ebnf.Reader[rune, runes.Pos]) (any, error) {
	return string(m.Value.([]rune)), nil
}

func ra() rl {
	return anon(ent.New[rune, runes.Pos](func(obj rune) bool { return true })).WithEval(evalRunes)
}

func rm(r rune) rl {
	e := ent.New[rune, runes.Pos](func(obj rune) bool { return obj == r })
	e.SetGenerateFunc(func() rune { return r })
	return anon(e).WithEval(evalRunes)
}

func rb(r1 rune, r2 rune) rl {
	return anon(rbOriginal(r1, r2)).WithEval(evalRunes)
}

func rbOriginal(r1 rune, r2 rune) *ent.Entity[rune, runes.Pos] {
	return ent.New[rune, runes.Pos](func(obj rune) bool { return obj >= r1 && obj <= r2 })
}

func rv(s string) rl {
	v := vec.New[rune, runes.Pos](req, []rune(s)...)
	return anon(v).WithEval(func(m *ebnf.Match[rune, runes.Pos], r ebnf.Reader[rune, runes.Pos]) (any, error) {
		return s, nil
	})
}

func conc(objs ...any) *concatenation.Concatenation[rune, runes.Pos] {
	return concatenation.New[rune, runes.Pos](objsToPatterns(objs...)...)
}

func rep(obj any) rl {
	r := repetition.New[rune, runes.Pos](objToPattern(obj), 0, 0).SetPossessive(true)
	return anon(r).WithEval(func(m *ebnf.Match[rune, runes.Pos], reader ebnf.Reader[rune, runes.Pos]) (any, error) {
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
}

func atLeast1(obj any) rl {
	r := repetition.New[rune, runes.Pos](objToPattern(obj), 1, 0).SetPossessive(true)
	return anon(r).WithEval(func(m *ebnf.Match[rune, runes.Pos], reader ebnf.Reader[rune, runes.Pos]) (any, error) {
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

	// the closing tag must repeat the name of the opening tag
	closeVal, err := choiceMatch.Components[7].Eval(r)
	if err != nil {
		return nil, err
	}
	if closeName := closeVal.(string); closeName != name {
		return nil, fmt.Errorf("closing tag </%s> does not match <%s>", closeName, name)
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

// Parse parses an XML document with a single root element into a tree of nodes.
func Parse(input string) (*Node, error) {
	// Define Grammar
	ws := rep(ent.New[rune, runes.Pos](unicode.IsSpace))
	nameChar := alt(rb('a', 'z'), rb('A', 'Z'), rb('0', '9'), '_', '-')
	name := atLeast1(nameChar).WithEval(evalName)

	attrValue := rule.New("attrValue", ex(ra(), '"')).WithEval(evalAttrValue)
	// attr: conc(name, '=', '"', rep(attrValue), '"')
	// index: 0: name, 1: '=', 2: '"', 3: rep, 4: '"'
	attr := rule.New("attr", conc(name, '=', '"', rep(attrValue), '"')).WithEval(evalAttr)

	attrs := rep(conc(ws, attr))

	// forward tag declaration
	tag := rule.Declare[rune, runes.Pos]("tag").WithEval(func(m *ebnf.Match[rune, runes.Pos], reader ebnf.Reader[rune, runes.Pos]) (any, error) {
		if len(m.Components) > 0 {
			return m.Components[0].Eval(reader)
		}
		return nil, nil
	})

	text := atLeast1(ex(ra(), '<')).WithEval(evalText)

	// content: rep(alt(tag, text))
	content := rep(alt(tag, text))
	content.WithEval(func(m *ebnf.Match[rune, runes.Pos], reader ebnf.Reader[rune, runes.Pos]) (any, error) {
		return m.Components, nil
	})

	// tag: alt(...)
	// Choice 0: conc('<', name, attrs, ws, rv("/>"))
	// Index:     0: <, 1: name, 2: attrs, 3: ws, 4: />
	// Choice 1: conc('<', name, attrs, ws, '>', content, rv("</"), name, ws, '>')
	// Index:     0: <, 1: name, 2: attrs, 3: ws, 4: >, 5: content, 6: </, 7: name, 8: ws, 9: >
	tag.Define(conc(rule.New("", alt(
		conc('<', name, attrs, ws, rv("/>")),
		conc('<', name, attrs, ws, '>', content, rv("</"), name, ws, '>'),
	)).WithEval(evalTag)))

	xmlGrammar := conc(ws, tag, ws)

	rd, err := runes.New(strings.NewReader(input))
	if err != nil {
		return nil, err
	}

	ok, match, err := ebnf.Parse[rune, runes.Pos](xmlGrammar, rd)
	if err != nil {
		return nil, err
	}

	if !ok || match == nil {
		return nil, fmt.Errorf("no match")
	}

	if !rd.Finished() {
		return nil, fmt.Errorf("unexpected input after the root element")
	}

	// xmlGrammar: conc(ws, tag, ws)
	// index: 0: ws, 1: tag, 2: ws
	val, err := match.Components[1].Components[0].Eval(rd)
	if err != nil {
		return nil, err
	}

	return val.(*Node), nil
}

func main() {
	input := `<root id="123" type="example">
			<item name="foo">Hello</item>
			<item name="bar" empty="true"/>
			<nested>
				<child>Deep</child>
			</nested>
		</root>`

	fmt.Printf("Input:\n%s\n", input)

	node, err := Parse(input)
	if err != nil {
		log.Fatal(err)
	}

	printNode(node, 0)
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
