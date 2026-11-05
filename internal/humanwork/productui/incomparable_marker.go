package productui

// incomparable, carried as a blank field, makes the struct that holds it
// non-comparable, so the compiler builds no equality function for it. Each
// one is code and a table entry in the browser module, and the view records
// marked with it are only ever read field by field: the compiler refuses ==
// and map keys on them. A type carrying this marker must never be compared
// with ==, used as a map key, or put behind a comparable type parameter; the
// build fails if it is, which is the point.
type incomparable [0]func()
