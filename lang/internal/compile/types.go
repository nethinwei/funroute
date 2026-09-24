package compile

import "funroute/lang/internal/machine"

// elemOf is an array's or a dictionary's element type, the zero Type for
// anything else.
func elemOf(t machine.Type) machine.Type {
	elem, _ := t.Elem()
	return elem
}

// hasElem reports a type with an element type.
func hasElem(t machine.Type) bool {
	_, ok := t.Elem()
	return ok
}
