package index

import (
	"reflect"
	"testing"
)

// A Query field Search and Matches both skip passes every parity case that
// doesn't rule a book out by it, so this checks the wiring structurally: every
// field but Order and Limit is a filter, and setting it shows up in filters.
func TestEveryQueryFieldIsAFilter(t *testing.T) {
	filterType := reflect.TypeFor[filter]()
	qt := reflect.TypeFor[Query]()

	for i := range qt.NumField() {
		field := qt.Field(i)
		if field.Name == "Order" || field.Name == "Limit" {
			continue
		}

		if !field.Type.Implements(filterType) {
			t.Errorf("Query.%s is a %s, which does not implement filter", field.Name, field.Type)
			continue
		}

		var q Query

		set := reflect.ValueOf(&q).Elem().Field(i)
		fillNonZero(set)

		if !containsFilter(q.filters(), set.Interface()) {
			t.Errorf("Query.filters does not return Query.%s", field.Name)
		}
	}
}

// fillNonZero sets every slice reachable from v to one zero element, which is
// enough to make a filter non-empty.
func fillNonZero(v reflect.Value) {
	switch v.Kind() {
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 1, 1))
	case reflect.Struct:
		for _, f := range v.Fields() {
			fillNonZero(f)
		}
	default:
	}
}

func containsFilter(fs []filter, want any) bool {
	for _, f := range fs {
		if reflect.DeepEqual(f, want) {
			return true
		}
	}

	return false
}
