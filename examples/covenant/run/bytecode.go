package run

import (
	"fmt"
	"strings"

	"github.com/almerlucke/exbana/v3/examples/covenant/check"
	"github.com/almerlucke/exbana/v3/examples/covenant/token"
)

// The bytecode is for a machine with a stack. Each function and each closure is a proto: a list of instructions, the values the
// instructions refer to, and the protos of the closures made in it. The parameters of a function are its first local variables,
// and every name a function declares has a local variable of its own, so a name is a number. A closure takes the values of the
// names it uses when it is made, in an array it carries, which is all that capturing by value needs.

type opcode uint8

const (
	opConst     opcode = iota // a: the value in the constants. Push it.
	opUnit                    // Push Unit.
	opLoad                    // a: local. Push it.
	opStore                   // a: local. Pop into it.
	opLoadCap                 // a: captured value. Push it.
	opLoadConst               // a: the constant (*check.ConstDef). Push its value, made when it is first needed.
	opPop
	opDup

	opAdd
	opSub
	opMul
	opDiv
	opRem
	opLt
	opLe
	opGt
	opGe
	opEq
	opNe
	opNot
	opNeg

	opJmp       // a: where to.
	opJmpFalse  // a: where to. Pop; jump if it is false.
	opJmpTrue   // a: where to. Pop; jump if it is true.
	opJmpFalseK // a: where to. If the top is false, jump and keep it; if not, pop it.
	opJmpTrueK  // a: where to. If the top is true, jump and keep it; if not, pop it.
	opRet       // Pop the result, and end the function.
	opRetUnit
	opTry // Pop a Result or an Option: push what is in it, or end the function with it.

	opCall       // a: the number of arguments. The function and the arguments are on the stack.
	opCallDef    // a: the function in the constants (a *proto, or a *check.FnDef without a body), b: the number of arguments.
	opCallMethod // a: the name in the constants, b: the number of arguments, with the value the method is of first.

	opMkList    // a: the number of items.
	opMkMap     // a: the number of entries; keys and values are on the stack, in turn.
	opMkStruct  // a: the *check.Def in the constants, b: the order of the arguments in the constants, or -1 if it is the order of the fields.
	opMkVariant // a: the *check.Variant in the constants, b: as for a struct.
	opField     // a: the name of the field in the constants. Pop a struct, push the field.
	opUpdate    // a: the names of the fields in the constants. The struct and the new values are on the stack.
	opClosure   // a: the proto, b: the number of captured values, which are on the stack.

	opIsVariant // a: the *check.Variant. Pop a value, push whether it is of the variant.
	opIsStruct  // a: the *check.Def. Pop a value, push whether it is of the struct.
	opFieldN    // a: the number of the field. Pop a variant or a struct, push the field.
	opNoMatch   // a: the local with the value no case of the match fits.
	opFault     // a: the kind in the constants, b: the message in the constants. End the program with a fault.

	opIter     // Pop a List, a Set or a Map, push what goes over it.
	opIterNext // a: where to go when there is no more, b: how many values it gives (a Map gives two).
)

var opNames = [...]string{
	opConst: "const", opUnit: "unit", opLoad: "load", opStore: "store", opLoadCap: "loadcap", opLoadConst: "loadconst",
	opPop: "pop", opDup: "dup", opAdd: "add", opSub: "sub", opMul: "mul", opDiv: "div", opRem: "rem", opLt: "lt", opLe: "le",
	opGt: "gt", opGe: "ge", opEq: "eq", opNe: "ne", opNot: "not", opNeg: "neg", opJmp: "jmp", opJmpFalse: "jmpfalse", opJmpTrue: "jmptrue",
	opJmpFalseK: "jmpfalsek", opJmpTrueK: "jmptruek", opRet: "ret", opRetUnit: "retunit", opTry: "try", opCall: "call",
	opCallDef: "calldef", opCallMethod: "callmethod", opMkList: "mklist", opMkMap: "mkmap", opMkStruct: "mkstruct",
	opMkVariant: "mkvariant", opField: "field", opUpdate: "update", opClosure: "closure", opIsVariant: "isvariant",
	opIsStruct: "isstruct", opFieldN: "fieldn", opNoMatch: "nomatch", opFault: "fault", opIter: "iter", opIterNext: "iternext",
}

func (o opcode) String() string { return opNames[o] }

type instr struct {
	op   opcode
	a, b int32
}

// proto is a function, or a closure, as bytecode.
type proto struct {
	name    string
	module  *check.Module
	nparams int
	nlocals int // the parameters, and every name in the function
	ncaps   int
	code    []instr
	pos     []int32 // for each instruction, where in the source it comes from
	consts  []Value
	protos  []*proto
}

// Disassemble writes the bytecode of a function of a module, and of the closures in it, one instruction to a line.
func (p *Program) Disassemble(module, function string) (string, error) {
	m := p.res.Modules[module]
	if m == nil {
		return "", fmt.Errorf("there is no module %s", module)
	}

	d, ok := m.Decls[function]
	if !ok {
		return "", fmt.Errorf("module %s has no %s", module, function)
	}

	fd, ok := d.Entity.(*check.FnDef)
	if !ok || fd.Decl.Body == nil {
		return "", fmt.Errorf("%s::%s is not a function with a body", module, function)
	}

	pr, err := p.protoOf(fd)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	pr.disassemble(&sb, "")

	return sb.String(), nil
}

func (pr *proto) disassemble(sb *strings.Builder, indent string) {
	fmt.Fprintf(sb, "%sfunc %s (params %d, locals %d, captures %d)\n", indent, pr.name, pr.nparams, pr.nlocals, pr.ncaps)

	for i, in := range pr.code {
		line := ""
		if pr.module != nil {
			l, _ := token.Position(pr.module.Source, int(pr.pos[i]))
			line = fmt.Sprintf("%d", l)
		}

		fmt.Fprintf(sb, "%s  %3d  %-4s %-10s%s\n", indent, i, line, in.op, pr.operands(in))
	}

	for _, c := range pr.protos {
		c.disassemble(sb, indent+"  ")
	}
}

func (pr *proto) operands(in instr) string {
	konst := func(i int32) string {
		switch c := pr.consts[i].(type) {
		case *proto:
			return "func " + c.name
		case *check.FnDef:
			return "native " + qualified(c)
		case *check.Variant:
			return c.Enum.Name + "::" + c.Name
		case *check.Def:
			return c.Name
		case *check.ConstDef:
			return "const " + c.Name
		case []int, []string:
			return fmt.Sprintf("%v", c)
		case *Func:
			return Format(c)
		}

		return Format(pr.consts[i])
	}

	switch in.op {
	case opConst, opLoadConst, opField, opIsVariant, opIsStruct, opUpdate:
		return " " + konst(in.a)
	case opLoad, opStore, opLoadCap, opFieldN, opNoMatch, opMkList, opMkMap, opCall:
		return fmt.Sprintf(" %d", in.a)
	case opJmp, opJmpFalse, opJmpTrue, opJmpFalseK, opJmpTrueK:
		return fmt.Sprintf(" -> %d", in.a)
	case opCallDef:
		return fmt.Sprintf(" %s, %d args", konst(in.a), in.b)
	case opCallMethod:
		return fmt.Sprintf(" .%s, %d args", pr.consts[in.a], in.b)
	case opMkStruct, opMkVariant:
		if in.b >= 0 {
			return fmt.Sprintf(" %s, order %v", konst(in.a), pr.consts[in.b])
		}

		return " " + konst(in.a)
	case opClosure:
		return fmt.Sprintf(" func %s, %d captured", pr.protos[in.a].name, in.b)
	case opFault:
		return fmt.Sprintf(" %s: %s", pr.consts[in.a], pr.consts[in.b])
	case opIterNext:
		return fmt.Sprintf(" -> %d, %d values", in.a, in.b)
	}

	return ""
}
