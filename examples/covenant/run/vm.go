package run

import (
	"context"
	"fmt"

	"github.com/almerlucke/exbana/v3/examples/covenant/ast"
	"github.com/almerlucke/exbana/v3/examples/covenant/check"
)

// frame is a call that is running: the function, where it is in it, and where its local variables are on the stack. When it ends
// the stack is cut back to ret, and the result is put there.
type frame struct {
	pr   *proto
	pc   int
	base int
	ret  int
	caps []Value
}

// vm runs bytecode. There is one for each call into the program, and it is used by one goroutine.
type vm struct {
	p      *Program
	ctx    context.Context
	entry  *check.Module
	steps  int64
	stack  []Value
	frames []frame
	consts map[*check.ConstDef]Value
	protos map[*check.FnDef]*proto
}

func newVM(p *Program, ctx context.Context, entry *check.Module) *vm {
	return &vm{p: p, ctx: ctx, entry: entry, stack: make([]Value, 0, 256), frames: make([]frame, 0, 32)}
}

func (v *vm) program() *Program        { return v.p }
func (v *vm) context() context.Context { return v.ctx }

func (v *vm) callback(f Value, at ast.Node, args ...Value) (Value, error) {
	return v.callFunc(f.(*Func), args, at)
}

// fault makes a fault at a place in the source, in the module of the code that is running.
func (v *vm) fault(at ast.Node, kind, msg string) *Fault {
	mod := v.entry
	if n := len(v.frames); n > 0 {
		mod = v.frames[n-1].pr.module
	}

	f := &Fault{Kind: kind, Msg: msg, Module: mod.Name}

	if at != nil {
		f.Line, f.Col = position(mod, at.At().Begin)
	}

	for i := len(v.frames) - 1; i >= 0; i-- {
		f.Stack = append(f.Stack, v.frames[i].pr.name)
	}

	return f
}

func (v *vm) spanAt(fr *frame) ast.Node {
	pos := int(fr.pr.pos[fr.pc-1])
	return ast.Span{Begin: pos, End: pos}
}

func (v *vm) faultHere(fr *frame, kind, msg string) *Fault {
	return v.fault(v.spanAt(fr), kind, msg)
}

// callFunc calls a function value.
func (v *vm) callFunc(f *Func, args []Value, at ast.Node) (Value, error) {
	if f.def != nil {
		return v.callDef(f.def, args, at)
	}

	return v.invoke(f.proto, args, f.caps, at)
}

func (v *vm) callDef(fd *check.FnDef, args []Value, at ast.Node) (Value, error) {
	if fd.Decl.Body == nil {
		return nativeCall(v, fd, args, at)
	}

	pr, err := v.protoOf(fd)
	if err != nil {
		return nil, err
	}

	return v.invoke(pr, args, nil, at)
}

func (v *vm) protoOf(fd *check.FnDef) (*proto, error) {
	if pr, ok := v.protos[fd]; ok {
		return pr, nil
	}

	pr, err := v.p.protoOf(fd)
	if err != nil {
		return nil, err
	}

	if v.protos == nil {
		v.protos = map[*check.FnDef]*proto{}
	}

	v.protos[fd] = pr

	return pr, nil
}

// invoke runs a function that is called from outside the bytecode: from the host, or from the library.
func (v *vm) invoke(pr *proto, args []Value, caps []Value, at ast.Node) (Value, error) {
	if len(v.frames) >= v.p.cfg.MaxDepth {
		return nil, v.fault(at, "stack", "calls are nested too deep")
	}

	base := len(v.stack)
	v.stack = append(v.stack, args...)
	v.enter(pr, base, base, caps)

	return v.run(len(v.frames) - 1)
}

// enter starts a call: the arguments are on the stack, and the rest of the local variables are made.
func (v *vm) enter(pr *proto, base, ret int, caps []Value) {
	for i := pr.nparams; i < pr.nlocals; i++ {
		v.stack = append(v.stack, nil)
	}

	v.frames = append(v.frames, frame{pr: pr, base: base, ret: ret, caps: caps})
}

// iter goes over a List, a Set or a Map in a for.
type iter struct {
	list *List
	set  *Set
	mp   *Map
	i    int
}

func (v *vm) run(entry int) (Value, error) {
	maxSteps := v.p.cfg.MaxSteps
	maxDepth := v.p.cfg.MaxDepth

outer:
	for {
		fr := &v.frames[len(v.frames)-1]
		code := fr.pr.code

		for {
			in := code[fr.pc]
			fr.pc++

			v.steps++

			if maxSteps > 0 && v.steps > maxSteps {
				return nil, &Limit{Kind: "steps"}
			}

			if v.steps&1023 == 0 && v.ctx != nil {
				if err := v.ctx.Err(); err != nil {
					return nil, &Limit{Kind: "cancelled", Err: err}
				}
			}

			switch in.op {
			case opConst:
				v.stack = append(v.stack, fr.pr.consts[in.a])
			case opUnit:
				v.stack = append(v.stack, Unit{})
			case opLoad:
				v.stack = append(v.stack, v.stack[fr.base+int(in.a)])
			case opStore:
				n := len(v.stack) - 1
				v.stack[fr.base+int(in.a)] = v.stack[n]
				v.stack = v.stack[:n]
			case opLoadCap:
				v.stack = append(v.stack, fr.caps[in.a])
			case opLoadConst:
				cd := fr.pr.consts[in.a].(*check.ConstDef)

				val, ok := v.consts[cd]
				if !ok {
					pr, err := v.p.constProto(cd)
					if err != nil {
						return nil, err
					}

					val, err = v.invoke(pr, nil, nil, v.spanAt(fr))
					if err != nil {
						return nil, err
					}

					if v.consts == nil {
						v.consts = map[*check.ConstDef]Value{}
					}

					v.consts[cd] = val

					v.stack = append(v.stack, val)

					continue outer
				}

				v.stack = append(v.stack, val)
			case opPop:
				v.stack = v.stack[:len(v.stack)-1]
			case opDup:
				v.stack = append(v.stack, v.stack[len(v.stack)-1])

			case opAdd, opSub, opMul, opDiv, opRem, opLt, opLe, opGt, opGe:
				n := len(v.stack)
				a, b := v.stack[n-2], v.stack[n-1]

				// Ints are what most programs are made of
				if x, ok := a.(int64); ok {
					if y, ok := b.(int64); ok {
						switch in.op {
						case opAdd:
							if s := x + y; (x^s)&(y^s) >= 0 {
								v.stack[n-2], v.stack = s, v.stack[:n-1]
								continue
							}
						case opSub:
							if s := x - y; (x^y)&(x^s) >= 0 {
								v.stack[n-2], v.stack = s, v.stack[:n-1]
								continue
							}
						case opLt:
							v.stack[n-2], v.stack = x < y, v.stack[:n-1]
							continue
						case opLe:
							v.stack[n-2], v.stack = x <= y, v.stack[:n-1]
							continue
						case opGt:
							v.stack[n-2], v.stack = x > y, v.stack[:n-1]
							continue
						case opGe:
							v.stack[n-2], v.stack = x >= y, v.stack[:n-1]
							continue
						}
					}
				}

				res, kind, msg := binaryOp(opSigns[in.op], a, b)
				if kind != "" {
					return nil, v.faultHere(fr, kind, msg)
				}

				v.stack[n-2], v.stack = res, v.stack[:n-1]
			case opEq:
				n := len(v.stack)
				v.stack[n-2], v.stack = Equal(v.stack[n-2], v.stack[n-1]), v.stack[:n-1]
			case opNe:
				n := len(v.stack)
				v.stack[n-2], v.stack = !Equal(v.stack[n-2], v.stack[n-1]), v.stack[:n-1]
			case opNot:
				n := len(v.stack) - 1
				v.stack[n] = v.stack[n] != true
			case opNeg:
				n := len(v.stack) - 1

				res, kind, msg := negate(v.stack[n])
				if kind != "" {
					return nil, v.faultHere(fr, kind, msg)
				}

				v.stack[n] = res

			case opJmp:
				fr.pc = int(in.a)
			case opJmpFalse, opJmpTrue:
				n := len(v.stack) - 1
				c := v.stack[n]
				v.stack = v.stack[:n]

				if (c == true) == (in.op == opJmpTrue) {
					fr.pc = int(in.a)
				}
			case opJmpFalseK, opJmpTrueK:
				n := len(v.stack) - 1

				if (v.stack[n] == true) == (in.op == opJmpTrueK) {
					fr.pc = int(in.a)
				} else {
					v.stack = v.stack[:n]
				}

			case opRet, opRetUnit, opTry:
				var val Value = Unit{}

				switch in.op {
				case opRet:
					val = v.stack[len(v.stack)-1]
				case opTry:
					n := len(v.stack) - 1
					e := v.stack[n].(*Enum)

					if e.Variant.Name == "Ok" || e.Variant.Name == "Some" {
						v.stack[n] = e.Fields[0]
						continue
					}

					val = e
				}

				idx := len(v.frames) - 1
				v.stack = v.stack[:fr.ret]
				v.frames = v.frames[:idx]

				if idx == entry {
					return val, nil
				}

				v.stack = append(v.stack, val)

				continue outer

			case opCall:
				n := int(in.a)
				sp := len(v.stack)

				f, ok := v.stack[sp-n-1].(*Func)
				if !ok {
					return nil, v.faultHere(fr, "internal", "a call of something that is not a function")
				}

				switch {
				case f.def == nil:
					if len(v.frames) >= maxDepth {
						return nil, v.faultHere(fr, "stack", "calls are nested too deep")
					}

					v.enter(f.proto, sp-n, sp-n-1, f.caps)

					continue outer
				case f.def.Decl.Body != nil:
					pr, err := v.protoOf(f.def)
					if err != nil {
						return nil, err
					}

					if len(v.frames) >= maxDepth {
						return nil, v.faultHere(fr, "stack", "calls are nested too deep")
					}

					v.enter(pr, sp-n, sp-n-1, nil)

					continue outer
				}

				args := append([]Value(nil), v.stack[sp-n:]...)

				res, err := nativeCall(v, f.def, args, v.spanAt(fr))
				if err != nil {
					return nil, err
				}

				v.stack = append(v.stack[:sp-n-1], res)

				continue outer
			case opCallDef:
				n := int(in.b)
				sp := len(v.stack)

				switch c := fr.pr.consts[in.a].(type) {
				case *proto:
					if len(v.frames) >= maxDepth {
						return nil, v.faultHere(fr, "stack", "calls are nested too deep")
					}

					v.enter(c, sp-n, sp-n, nil)

					continue outer
				case *check.FnDef:
					args := append([]Value(nil), v.stack[sp-n:]...)

					res, err := nativeCall(v, c, args, v.spanAt(fr))
					if err != nil {
						return nil, err
					}

					v.stack = append(v.stack[:sp-n], res)

					continue outer
				}
			case opCallMethod:
				n := int(in.b)
				sp := len(v.stack)
				name := fr.pr.consts[in.a].(string)

				fd, self := v.p.dispatch(v.stack[sp-n], name)
				if fd == nil {
					return nil, v.faultHere(fr, "internal", "no method "+name)
				}

				v.stack[sp-n] = self

				if fd.Decl.Body == nil {
					args := append([]Value(nil), v.stack[sp-n:]...)

					res, err := nativeCall(v, fd, args, v.spanAt(fr))
					if err != nil {
						return nil, err
					}

					v.stack = append(v.stack[:sp-n], res)

					continue outer
				}

				pr, err := v.protoOf(fd)
				if err != nil {
					return nil, err
				}

				if len(v.frames) >= maxDepth {
					return nil, v.faultHere(fr, "stack", "calls are nested too deep")
				}

				v.enter(pr, sp-n, sp-n, nil)

				continue outer

			case opMkList:
				n := int(in.a)
				items := make([]Value, n)
				copy(items, v.stack[len(v.stack)-n:])
				v.stack = append(v.stack[:len(v.stack)-n], &List{&listBase{items}, n})
			case opMkMap:
				n := int(in.a)
				base := len(v.stack) - 2*n
				out := NewMap()

				for i := 0; i < n; i++ {
					out = out.Set(v.stack[base+2*i], v.stack[base+2*i+1])
				}

				v.stack = append(v.stack[:base], out)
			case opMkStruct, opMkVariant:
				var (
					def    *check.Def
					vr     *check.Variant
					nfield int
				)

				if in.op == opMkStruct {
					def = fr.pr.consts[in.a].(*check.Def)
					nfield = len(def.Fields)
				} else {
					vr = fr.pr.consts[in.a].(*check.Variant)
					def, nfield = vr.Enum, len(vr.Fields)
				}

				base := len(v.stack) - nfield
				vals := make([]Value, nfield)

				if in.b >= 0 {
					for i, to := range fr.pr.consts[in.b].([]int) {
						vals[to] = v.stack[base+i]
					}
				} else {
					copy(vals, v.stack[base:])
				}

				var made Value = &Struct{Def: def, Fields: vals}
				if vr != nil {
					made = &Enum{def, vr, vals}
				}

				v.stack = append(v.stack[:base], made)
			case opField:
				n := len(v.stack) - 1
				name := fr.pr.consts[in.a].(string)

				s, ok := v.stack[n].(*Struct)
				if !ok {
					return nil, v.faultHere(fr, "internal", "no field "+name)
				}

				val, ok := s.Field(name)
				if !ok {
					return nil, v.faultHere(fr, "internal", "no field "+name)
				}

				v.stack[n] = val
			case opUpdate:
				k := int(in.b)
				names := fr.pr.consts[in.a].([]string)
				base := len(v.stack) - k - 1

				s, ok := v.stack[base].(*Struct)
				if !ok {
					return nil, v.faultHere(fr, "internal", "with on something that is not a struct")
				}

				out := &Struct{Def: s.Def, Fields: append([]Value(nil), s.Fields...), Host: s.Host}

				for i, name := range names {
					for j, fi := range s.Def.Fields {
						if fi.Name == name {
							out.Fields[j] = v.stack[base+1+i]
						}
					}
				}

				v.stack = append(v.stack[:base], out)
			case opClosure:
				n := int(in.b)
				caps := append([]Value(nil), v.stack[len(v.stack)-n:]...)
				f := &Func{name: "closure", proto: fr.pr.protos[in.a], caps: caps}
				v.stack = append(v.stack[:len(v.stack)-n], f)

			case opIsVariant:
				n := len(v.stack) - 1
				e, ok := v.stack[n].(*Enum)
				v.stack[n] = ok && e.Variant == fr.pr.consts[in.a].(*check.Variant)
			case opIsStruct:
				n := len(v.stack) - 1
				s, ok := v.stack[n].(*Struct)
				v.stack[n] = ok && s.Def == fr.pr.consts[in.a].(*check.Def)
			case opFieldN:
				n := len(v.stack) - 1

				switch x := v.stack[n].(type) {
				case *Enum:
					v.stack[n] = x.Fields[in.a]
				case *Struct:
					v.stack[n] = x.Fields[in.a]
				}
			case opBox:
				n := len(v.stack) - 1
				v.stack[n] = boxed(v.stack[n], fr.pr.consts[in.a].(*check.TraitDef))
			case opNoMatch:
				return nil, v.faultHere(fr, "internal", "no case of the match fits "+Format(v.stack[fr.base+int(in.a)]))
			case opFault:
				return nil, v.faultHere(fr, fr.pr.consts[in.a].(string), fr.pr.consts[in.b].(string))

			case opIter:
				n := len(v.stack) - 1

				it := &iter{}

				switch x := v.stack[n].(type) {
				case *List:
					it.list = x
				case *Set:
					it.set = x
				case *Map:
					it.mp = x
				}

				v.stack[n] = it
			case opIterNext:
				n := len(v.stack) - 1
				it := v.stack[n].(*iter)

				switch {
				case it.list != nil && it.i < it.list.Len():
					v.stack = append(v.stack, it.list.At(it.i))
				case it.set != nil && it.i < it.set.Len():
					v.stack = append(v.stack, it.set.Item(it.i))
				case it.mp != nil && it.i < it.mp.Len():
					k, val := it.mp.Entry(it.i)
					v.stack = append(v.stack, k, val)
				default:
					v.stack = v.stack[:n]
					fr.pc = int(in.a)

					continue
				}

				it.i++
			default:
				return nil, v.faultHere(fr, "internal", fmt.Sprintf("an instruction the machine does not know: %v", in.op))
			}
		}
	}
}

var opSigns = map[opcode]string{
	opAdd: "+", opSub: "-", opMul: "*", opDiv: "/", opRem: "%", opLt: "<", opLe: "<=", opGt: ">", opGe: ">=",
}
