// Package runstate owns the durable execution journal for admitted agent runs.
// It records safe checkpoints and fences workers before external effects so a
// restart can resume deterministic work or reconcile ambiguous effects.
package runstate
