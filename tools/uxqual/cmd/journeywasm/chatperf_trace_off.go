//go:build !(js && wasm && chatperf)

package main

import "google.golang.org/grpc"

// The load trace (chatperf_trace_on_wasm.go) is compiled only under the
// chatperf build tag. Every other build gets these, which do nothing.

func chatperfTrace(string) {}

func chatperfTraceCaller(string) {}

func chatperfTraceEvent(int32, bool) {}

func chatperfConn(conn grpc.ClientConnInterface) grpc.ClientConnInterface { return conn }
