package main

// incomparable, carried as a blank field, makes the struct that holds it
// non-comparable. The compiler then builds no equality function for the struct
// (nor for arrays and structs made of it): each one is code and a table entry in
// the browser module, and the types below are request, response and draft
// records that are only ever read field by field. Nothing compares them, which
// the compiler checks for == and for map keys; they are not component props or
// hook dependencies, where an interface comparison would silently turn into
// "always different" instead of a compile error.
//
// Do not add this to a struct that is a component's props, a hook's state or
// dependency, or part of one: GoWebComponents compares those, and a type that
// cannot be compared re-renders every time.
type incomparable [0]func()
