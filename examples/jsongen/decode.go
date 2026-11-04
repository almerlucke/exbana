package main

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/almerlucke/exbana/v3/examples/jsongen/parser"
)

// Decode turns a parsed document into Go values, the way encoding/json does for an any: nil, bool, float64, string,
// []any and map[string]any. It reads the tree through the typed views of the parser: a value is a string, a number, an
// array, an object or a literal, and Kind says which, so there is no node number or rule check in sight.
func Decode(tree *parser.JSONTree) (any, error) {
	return decodeValue(tree.Top().Value())
}

func decodeValue(v parser.JSONValue) (any, error) {
	switch kind, _ := v.Kind(); kind {
	case parser.JSONRuleString:
		s, _ := v.String()

		return decodeString(s)
	case parser.JSONRuleNumber:
		n, _ := v.Number()

		return strconv.ParseFloat(strings.TrimSpace(n.Text()), 64)
	case parser.JSONRuleLiteral:
		l, _ := v.Literal()

		switch strings.TrimSpace(l.Text()) {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}

		return nil, nil
	case parser.JSONRuleArray:
		a, _ := v.Array()
		out := []any{}

		for e := range a.Values() {
			x, err := decodeValue(e)
			if err != nil {
				return nil, err
			}

			out = append(out, x)
		}

		return out, nil
	case parser.JSONRuleObject:
		o, _ := v.Object()
		out := map[string]any{}

		for m := range o.Members() {
			key, err := decodeString(m.String())
			if err != nil {
				return nil, err
			}

			x, err := decodeValue(m.Value())
			if err != nil {
				return nil, err
			}

			out[key] = x
		}

		return out, nil
	}

	return nil, fmt.Errorf("a value that is none of the kinds: %q", v.Text())
}

// decodeString reads the escapes of a string. The text of the node is the quotes, what is between them, and the white
// space after it.
func decodeString(s parser.JSONString) (string, error) {
	text := strings.TrimRight(s.Text(), " \t\r\n")
	body := text[1 : len(text)-1]

	if !strings.Contains(body, `\`) {
		return body, nil
	}

	var sb strings.Builder

	for i := 0; i < len(body); i++ {
		if body[i] != '\\' {
			sb.WriteByte(body[i])
			continue
		}

		i++

		switch body[i] {
		case 'b':
			sb.WriteByte('\b')
		case 'f':
			sb.WriteByte('\f')
		case 'n':
			sb.WriteByte('\n')
		case 'r':
			sb.WriteByte('\r')
		case 't':
			sb.WriteByte('\t')
		case 'u':
			r, err := strconv.ParseUint(body[i+1:i+5], 16, 32)
			if err != nil {
				return "", err
			}

			i += 4

			// a high surrogate and a low one after it are one character
			if rest := body[i+1:]; utf16.IsSurrogate(rune(r)) && len(rest) >= 6 && rest[:2] == `\u` {
				if low, err := strconv.ParseUint(rest[2:6], 16, 32); err == nil {
					if pair := utf16.DecodeRune(rune(r), rune(low)); pair != '\uFFFD' {
						sb.WriteRune(pair)
						i += 6

						continue
					}
				}
			}

			sb.WriteRune(rune(r))
		default: // " \ /
			sb.WriteByte(body[i])
		}
	}

	return sb.String(), nil
}
