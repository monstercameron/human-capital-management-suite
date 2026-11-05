// Package taskpool offers a bounded runtime task pool for case work
// (WF-EXT-025): a node declares a pool that admits runtime-added tasks from
// an allow-listed catalog up to a declared maximum, each with a typed output
// and audit. The pool carries no case semantics - the case kernel keeps
// those - and it cannot add effects: every catalog kind is read or draft
// only.
package taskpool
