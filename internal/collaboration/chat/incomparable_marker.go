package chat

// incomparable, carried as a blank field, makes the struct that holds it
// non-comparable, so the compiler builds no equality function for it. Each one
// is code and a table entry in the browser module, and the records marked with
// it are only ever read field by field: the compiler refuses == and map keys on
// them, and a component that compared one as a prop would fall back to a
// reflective comparison with the same answer.
type incomparable [0]func()
