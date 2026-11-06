package tool

import (
	"github.com/almerlucke/exbana/v3/examples/covenant/check"
)

type schemer struct {
	defs  map[string]any
	doing map[string]bool
}

func newSchemer() *schemer { return &schemer{defs: map[string]any{}, doing: map[string]bool{}} }

// of is the JSON Schema of a type. A struct or an enum is in the definitions, and is referred to: that is what lets a type have
// itself inside it.
func (s *schemer) of(t check.Type) map[string]any {
	sh, con := shapeOf(t)

	switch sh {
	case shInt:
		return map[string]any{"type": "integer"}
	case shFloat:
		return map[string]any{"type": "number"}
	case shText:
		return map[string]any{"type": "string"}
	case shBool:
		return map[string]any{"type": "boolean"}
	case shUnit:
		return map[string]any{"type": "null"}
	case shList:
		return map[string]any{"type": "array", "items": s.of(con.Args[0])}
	case shSet:
		return map[string]any{"type": "array", "items": s.of(con.Args[0]), "uniqueItems": true}
	case shMap:
		if k, _ := shapeOf(con.Args[0]); k == shText {
			return map[string]any{"type": "object", "additionalProperties": s.of(con.Args[1])}
		}

		return map[string]any{"type": "array", "items": map[string]any{
			"type": "array", "prefixItems": []any{s.of(con.Args[0]), s.of(con.Args[1])}, "minItems": 2, "maxItems": 2,
		}}
	case shOption:
		return map[string]any{"anyOf": []any{s.of(con.Args[0]), map[string]any{"type": "null"}}}
	case shResult:
		return map[string]any{"oneOf": []any{
			single("ok", s.of(con.Args[0])),
			single("err", s.of(con.Args[1])),
		}}
	case shData:
		return map[string]any{"description": "any JSON"}
	case shStruct, shEnum:
		name := defName(con)

		if !s.doing[name] {
			s.doing[name] = true

			if sh == shStruct {
				s.defs[name] = s.object(con, con.Def.Fields)
			} else {
				s.defs[name] = s.enum(con)
			}
		}

		return map[string]any{"$ref": "#/$defs/" + name}
	}

	return map[string]any{}
}

// single is an object with one key.
func single(key string, schema map[string]any) map[string]any {
	return map[string]any{
		"type": "object", "properties": map[string]any{key: schema}, "required": []any{key}, "additionalProperties": false,
	}
}

func (s *schemer) object(con *check.Con, fields []*check.FieldInfo) map[string]any {
	props := map[string]any{}
	required := []any{}

	for _, f := range fields {
		props[f.Name] = s.of(check.FieldType(con, f))
		required = append(required, f.Name)
	}

	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
}

func (s *schemer) enum(con *check.Con) map[string]any {
	var cases []any

	for _, v := range con.Def.Variants {
		if len(v.Fields) == 0 {
			cases = append(cases, map[string]any{"const": v.Name})
			continue
		}

		cases = append(cases, single(v.Name, s.object(con, v.Fields)))
	}

	return map[string]any{"oneOf": cases}
}

// withDefs is a schema, with the definitions it refers to.
func (s *schemer) withDefs(schema map[string]any) map[string]any {
	if len(s.defs) > 0 {
		schema["$defs"] = s.defs
	}

	return schema
}
