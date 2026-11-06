// Package tool is what makes the public functions of a Covenant program tools for an agent: it says what they are, as JSON that an
// agent can read, and calls them with JSON.
//
// Describe gives a manifest: for each public function its name, what the comment above it says, a JSON Schema for the arguments and
// for the result, the effects it needs, and its examples. Call runs a function with arguments in JSON and gives the result in
// JSON. The two go by the same rules, which are these:
//
//	Int, Float, Text, Bool        a number, a number, a string, a boolean
//	Unit                          null
//	List<T>, Set<T>               an array
//	Map<Text, V>                  an object, with the keys in the order they are written
//	Map<K, V>                     an array of pairs [key, value]
//	Option<T>                     null, or the value (an Option of an Option is not a tool: it could not be told apart)
//	Result<T, E>                  {"ok": value} or {"err": error}
//	a struct                      an object with all its fields
//	an enum                       "Name" for a variant that holds nothing, and {"Name": {field: value}} for one that does
//	Data                          any JSON
//
// A function is not a tool if it is generic, or has a function or a dyn in its parameters or its result: there is no JSON for them.
package tool

import (
	"fmt"
	"strings"

	"github.com/almerlucke/exbana/v3/examples/covenant/check"
)

type shape int

const (
	shInt shape = iota
	shFloat
	shText
	shBool
	shUnit
	shList
	shSet
	shMap
	shOption
	shResult
	shData
	shStruct
	shEnum
	shNone
)

// shapeOf is what a type is made of, for JSON: and the type with its arguments, for what has them.
func shapeOf(t check.Type) (shape, *check.Con) {
	con, ok := check.Resolve(t).(*check.Con)
	if !ok {
		return shNone, nil
	}

	if con.Def.Module == "prelude" {
		switch con.Def.Name {
		case "Int":
			return shInt, con
		case "Float":
			return shFloat, con
		case "Text":
			return shText, con
		case "Bool":
			return shBool, con
		case "Unit":
			return shUnit, con
		case "List":
			return shList, con
		case "Set":
			return shSet, con
		case "Map":
			return shMap, con
		case "Option":
			return shOption, con
		case "Result":
			return shResult, con
		case "Data":
			return shData, con
		}
	}

	switch con.Def.Kind {
	case check.DefStruct:
		return shStruct, con
	case check.DefEnum:
		return shEnum, con
	}

	return shNone, con
}

// supported says why a type has no JSON, or "" if it has.
func supported(t check.Type, seen map[*check.Def]bool) string {
	switch rt := check.Resolve(t).(type) {
	case *check.Fn:
		return "a function has no JSON"
	case *check.Dyn:
		return "a dyn has no JSON"
	case *check.Param, *check.Var:
		return "a type that is not known has no JSON"
	case *check.Con:
		sh, con := shapeOf(rt)
		if sh == shNone {
			return fmt.Sprintf("%s has no JSON", rt)
		}

		if sh == shOption {
			if inner, _ := shapeOf(con.Args[0]); inner == shOption {
				return "an Option of an Option has no JSON: Some(None) could not be told from None"
			}
		}

		for _, a := range con.Args {
			if why := supported(a, seen); why != "" {
				return why
			}
		}

		if (sh == shStruct || sh == shEnum) && !seen[con.Def] {
			seen[con.Def] = true

			for _, f := range con.Def.Fields {
				if why := supported(check.FieldType(con, f), seen); why != "" {
					return why
				}
			}

			for _, v := range con.Def.Variants {
				for _, f := range v.Fields {
					if why := supported(check.FieldType(con, f), seen); why != "" {
						return why
					}
				}
			}
		}
	}

	return ""
}

// defName is the name a struct or an enum has in the definitions of a schema: the module, the name, and what it is given.
func defName(con *check.Con) string {
	name := con.Def.Module + "." + con.Def.Name

	for _, a := range con.Args {
		name += "_" + sanitize(check.Resolve(a).String())
	}

	return name
}

func sanitize(s string) string {
	var sb strings.Builder

	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.':
			sb.WriteRune(r)
		default:
			sb.WriteByte('_')
		}
	}

	return sb.String()
}
