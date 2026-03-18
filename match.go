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

	for u.Pattern.CanUnpack() && len(u.Components) > 0 && u.Pattern.ID() == NoID {
		u = u.Components[0]
	}

	return u
}

func (m *Match[T, P]) ID() string {
	return m.Pattern.ID()
}

func (m *Match[T, P]) Eval(r Reader[T, P]) (any, error) {
	return m.Pattern.Eval(m, r)
}
