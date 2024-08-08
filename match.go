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

// Values for components (Concat & Repeat)
func (m *Match[T, P]) Values() []any {
	components := m.Components
	values := make([]any, len(components))
	for index, component := range components {
		values[index] = component.Value
	}
	return values
}

// Optional match (Alt)
func (m *Match[T, P]) Optional() (*Match[T, P], bool) {
	if len(m.Components) > 0 {
		return m.Components[0], true
	}

	return nil, false
}

func (m *Match[T, P]) Unpack() *Match[T, P] {
	// Unpack components until we can not unpack anymore (useful to get to the first real match in Alternation pattern for example)
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
