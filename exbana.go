package exbana

type EntityStreamer interface {
	Peek() interface{}
	Read() interface{}
	Finished() bool
	Position() int
	SetPosition(int)
}

type PatternMatchResult struct {
	StartPosition int
	EndPosition   int
}

type PatternMatcher interface {
	Match(EntityStreamer) (bool, PatternMatchResult)
	Identifier() string
}
