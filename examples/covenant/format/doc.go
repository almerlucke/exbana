package format

import (
	"strings"
	"unicode/utf8"
)

// A document is what the printer builds before it is text: pieces that are put one after the other, groups that go on one line if
// they fit and on several if they do not, and the indentation of the lines in them. The layout is chosen when it is written out,
// which is when the width is known.

type doc interface{}

type (
	text   string // a piece of text without a line end in it
	cat    []doc  // one after the other
	indent struct{ d doc }

	// group is laid out on one line if it fits, and with every line in it broken if it does not. brk makes it break whatever its size.
	// A group is used as a pointer, so that breaks can set brk.
	//
	// A chain is a group that has no brackets of its own, so that the line ends in it are in the brackets around it, and a list of
	// which the source has a line end in it is on several lines: a chain that breaks on its own makes the groups around it break.
	group struct {
		d     doc
		brk   bool
		chain bool
	}

	// line is a space when its group is on one line, and a line end when it is not. A soft line is nothing instead of a space. A hard
	// line is always a line end, and makes every group it is in break.
	line struct{ soft, hard bool }

	// suffix is a comment at the end of the line it is on: it is written before the next line end, and it makes the groups it is in
	// break, as nothing can follow it on its line.
	suffix string

	// hug is a piece that is laid out by itself: a group around it does not have to break because of the line ends inside it. It is
	// for what has a body of its own, a closure with a block, which goes on the line of what it is an argument of.
	hug struct{ d doc }
)

var (
	hardline = line{hard: true}
	softline = line{soft: true}
	space    = line{}
)

func join(sep doc, docs []doc) cat {
	var out cat

	for i, d := range docs {
		if i > 0 {
			out = append(out, sep)
		}

		out = append(out, d)
	}

	return out
}

// breaks says that a document has a line end in it that has to be, and sets brk on the groups with one.
func breaks(d doc) bool {
	switch d := d.(type) {
	case cat:
		forced := false

		for _, c := range d {
			if breaks(c) {
				forced = true
			}
		}

		return forced
	case indent:
		return breaks(d.d)
	case *group:
		forced := breaks(d.d)
		if forced {
			d.brk = true
		}

		return forced || (d.chain && d.brk)
	case line:
		return d.hard
	case suffix:
		return true
	case hug:
		breaks(d.d)

		return false
	}

	return false
}

func newGroup(d doc) *group { return &group{d: d} }

// source is a group that breaks because the source had it on more than one line.
func sourceGroup(d doc, broken bool) *group { return &group{d: d, brk: broken} }

func chainGroup(d doc, broken bool) *group { return &group{d: d, brk: broken, chain: true} }

type cmd struct {
	ind  int
	flat bool
	d    doc
}

const indentWidth = 4

// render writes the document, with lines no longer than width where it can.
func render(d doc, width int) string {
	breaks(d)

	var (
		out     []byte
		col     int
		pending []string
		stack   = []cmd{{0, false, d}}
	)

	flush := func() {
		for _, s := range pending {
			out = append(out, ' ')
			out = append(out, s...)
		}

		pending = pending[:0]
	}

	newline := func(ind int) {
		flush()

		for len(out) > 0 && out[len(out)-1] == ' ' {
			out = out[:len(out)-1]
		}

		out = append(out, '\n')
		out = append(out, strings.Repeat(" ", ind)...)

		col = ind
	}

	for len(stack) > 0 {
		c := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		switch d := c.d.(type) {
		case text:
			out = append(out, string(d)...)
			col += utf8.RuneCountInString(string(d))
		case cat:
			for i := len(d) - 1; i >= 0; i-- {
				stack = append(stack, cmd{c.ind, c.flat, d[i]})
			}
		case indent:
			// a group on one line has no lines to indent: what is in it that has lines of its own (a hug) is as far in as the
			// group is
			ind := c.ind
			if !c.flat {
				ind += indentWidth
			}

			stack = append(stack, cmd{ind, c.flat, d.d})
		case hug:
			stack = append(stack, cmd{c.ind, false, d.d})
		case suffix:
			pending = append(pending, string(d))
		case line:
			switch {
			case d.hard || !c.flat:
				newline(c.ind)
			case !d.soft:
				out = append(out, ' ')
				col++
			}
		case *group:
			switch {
			case c.flat:
				stack = append(stack, cmd{c.ind, !d.brk, d.d})
			default:
				next := cmd{c.ind, true, d.d}
				if !d.brk && fits(next, stack, width-col) {
					stack = append(stack, next)
				} else {
					stack = append(stack, cmd{c.ind, false, d.d})
				}
			}
		}
	}

	flush()

	return strings.TrimRight(string(out), " \n") + "\n"
}

// fits says whether what comes up to the next line end that is a line end fits in the room that is left. next is tried on one
// line; the rest of the commands are as they are going to be.
func fits(next cmd, rest []cmd, room int) bool {
	stack := []cmd{next}
	ri := len(rest)

	for room >= 0 {
		if len(stack) == 0 {
			if ri == 0 {
				return true
			}

			ri--
			stack = append(stack, rest[ri])

			continue
		}

		c := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		switch d := c.d.(type) {
		case text:
			room -= utf8.RuneCountInString(string(d))
		case cat:
			for i := len(d) - 1; i >= 0; i-- {
				stack = append(stack, cmd{c.ind, c.flat, d[i]})
			}
		case indent:
			stack = append(stack, cmd{c.ind, c.flat, d.d})
		case hug:
			stack = append(stack, cmd{c.ind, false, d.d})
		case *group:
			stack = append(stack, cmd{c.ind, c.flat && !d.brk, d.d})
		case line:
			if d.hard || !c.flat {
				return true
			}

			if !d.soft {
				room--
			}
		}
	}

	return false
}
