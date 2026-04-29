package query

type Query []Predicate

type Predicate struct {
	Type  string
	Value string
}
