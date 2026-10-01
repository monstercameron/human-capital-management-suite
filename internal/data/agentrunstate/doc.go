// Package agentrunstate implements the runstate store in the isolated agent
// database. Every operation runs under a tenant-bound transaction; execution
// snapshots, append-only checkpoints and mutable effect observations remain
// separate durable records.
package agentrunstate
