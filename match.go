package exbana

// Match contains matched pattern, position, optional value and optional components
type Match[T, P any] struct {
	Pattern    Pattern[T, P]
	Begin      P
	End        P
	Value      any
	Components []*Match[T, P]
}

// NewMatch creates a new pattern match result
func NewMatch[T, P any](pattern Pattern[T, P], begin P, end P, value []T, components []*Match[T, P]) *Match[T, P] {
	return &Match[T, P]{
		Pattern:    pattern,
		Begin:      begin,
		End:        end,
		Value:      value,
		Components: components,
	}
}

// Match returns the matched components' values.
// This is particularly useful for patterns like Concatenation or Repetition that have multiple sub-matches.
func (m *Match[T, P]) Values() []any {
	components := m.Components
	values := make([]any, len(components))
	for index, component := range components {
		values[index] = component.Value
	}
	return values
}

// Optional returns the first component match and true if it exists, nil and false otherwise.
// This is useful for Alternation or Optional Repetition patterns.
func (m *Match[T, P]) Optional() (*Match[T, P], bool) {
	if len(m.Components) > 0 {
		return m.Components[0], true
	}

	return nil, false
}

// Unpack navigates through the match hierarchy to find the first match from a pattern with an ID,
// or the deepest match if no ID is set. This is useful for stripping away wrapper patterns (like Alternation)
// that don't have their own ID.
func (m *Match[T, P]) Unpack() *Match[T, P] {
	u := m

	for len(u.Components) > 0 && IDOf(u.Pattern) == NoID {
		if up, ok := u.Pattern.(Unpackable); !ok || !up.CanUnpack() {
			break
		}

		u = u.Components[0]
	}

	return u
}

// ID returns the identifier of the matched pattern, empty if it has none.
func (m *Match[T, P]) ID() string {
	return IDOf(m.Pattern)
}

// Eval evaluates the match with the pattern that matched, if it is an Evaluator, and otherwise returns the matched value.
func (m *Match[T, P]) Eval(r Reader[T, P]) (any, error) {
	if e, ok := m.Pattern.(Evaluator[T, P]); ok {
		return e.Eval(m, r)
	}

	return m.Value, nil
}
