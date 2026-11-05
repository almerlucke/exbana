package ast

import (
	"fmt"
	"reflect"
	"strings"
)

// Dump writes a node, or anything in it, on one line, to see what the tree is: (Binary Op:"+" L:(Name Path:[a]) R:(Lit Text:"1")).
// The positions are left out, and so is everything that is empty.
func Dump(v any) string {
	var sb strings.Builder
	dump(&sb, reflect.ValueOf(v))

	return sb.String()
}

func dump(sb *strings.Builder, v reflect.Value) {
	switch v.Kind() {
	case reflect.Invalid:
		sb.WriteString("nil")
	case reflect.Interface, reflect.Pointer:
		if v.IsNil() {
			sb.WriteString("nil")
			return
		}

		dump(sb, v.Elem())
	case reflect.Struct:
		sb.WriteString("(" + v.Type().Name())

		for i := range v.NumField() {
			f := v.Type().Field(i)
			if f.Name == "Span" || v.Field(i).IsZero() {
				continue
			}

			sb.WriteString(" " + f.Name + ":")
			dump(sb, v.Field(i))
		}

		sb.WriteString(")")
	case reflect.Slice:
		sb.WriteString("[")

		for i := range v.Len() {
			if i > 0 {
				sb.WriteString(" ")
			}

			dump(sb, v.Index(i))
		}

		sb.WriteString("]")
	case reflect.String:
		if strings.ContainsAny(v.String(), " \"") {
			fmt.Fprintf(sb, "%q", v.String())
		} else {
			sb.WriteString(v.String())
		}
	default:
		fmt.Fprint(sb, v.Interface())
	}
}
