package machine

import "slices"

// The library: the aggregates, the string, array and dictionary functions,
// and the numeric helpers a payment rule writes most. They are the kernel's,
// not the standard pack's, so that a call to one is a kernel call — no
// deadline to look at, no result to hold to its type, no recover around a
// run that makes only such calls — and so that they work on a value's own
// backing rather than through the public constructors. They register in
// the order the pack did: the order of a name's overloads is the order the
// compiler tries them, and the pack's money overloads of these names come
// after all of them.
func registerLibrary(registry *Registry) {
	for _, spec := range librarySpecs() {
		mustRegister(registry, spec)
	}
}

// librarySpecs are the library's functions, in the order they register.
func librarySpecs() []FunctionSpec {
	return slices.Concat(
		sumSpecs(), extremeSpecs(), quantifierSpecs(), rangeSpecs(),
		caseSpecs(), testSpecs(), partSpecs(),
		shapeSpecs(), sortSpecs(), numericSpecs(),
		selectSpecs(), keyedSpecs(), positionSpecs(), extremeBySpecs(), dictSpecs(),
	)
}

// libGo is a Go function as a spec.
func libGo(name string, doc Doc, fn any) FunctionSpec {
	return FunctionSpec{Name: name, Doc: doc, Go: fn}
}

// libEach registers one name for every element type it serves. The language
// has no type classes, so a function that works on int, float and string is
// three registrations — that is the signature, not repetition.
func libEach(name string, doc Doc, implementations ...any) []FunctionSpec {
	specs := make([]FunctionSpec, len(implementations))
	for i, implementation := range implementations {
		specs[i] = libGo(name, doc, implementation)
	}
	return specs
}

// packArray is items, of elem already, in the canonical backing.
func packArray(elem Type, items []Value) Value {
	builder := newArrayBuilder(elem, len(items))
	for _, item := range items {
		builder.add(item)
	}
	return builder.finish()
}
