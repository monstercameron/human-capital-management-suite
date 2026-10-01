// Package clockadapter translates supported hardware clock protocols into the
// canonical clock device service. It authenticates device identity through an
// injected verifier and delegates all policy, replay, and persistence decisions
// to injected canonical services.
package clockadapter
