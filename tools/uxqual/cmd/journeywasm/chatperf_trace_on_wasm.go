//go:build js && wasm && chatperf

package main

import (
	"context"
	"runtime"
	"strconv"
	"strings"
	"syscall/js"

	"google.golang.org/grpc"
)

// The chatperf build tag turns on a trace of the Chat load: every render of the
// page, every request for one (with the function that asked), and the start and
// end of every call over the tunnel, as User Timing marks under "hcm:t:". It is
// for measuring where a load spends its time and is never in a served build:
//
//	GOOS=js GOARCH=wasm go build -tags=grpcnotrace,nethttpomithttp2,chatperf ...
//
// Unlike bootMark, a name is marked every time it happens.

// chatperfTrace records one occurrence of name.
func chatperfTrace(name string) {
	performance := js.Global().Get("performance")
	if !performance.Truthy() || !performance.Get("mark").Truthy() {
		return
	}
	performance.Call("mark", bootMarkPrefix+"t:"+name)
}

// chatperfTraceCaller records one occurrence of name with the function that
// called the traced function.
func chatperfTraceCaller(name string) {
	caller := "?"
	if pc, _, _, ok := runtime.Caller(2); ok {
		if fn := runtime.FuncForPC(pc); fn != nil {
			caller = fn.Name()
			caller = caller[strings.LastIndex(caller, "/")+1:]
		}
	}
	chatperfTrace(name + " " + caller)
}

// chatperfTraceEvent records one stream event by kind, and whether it changed
// what is drawn.
func chatperfTraceEvent(kind int32, drew bool) {
	state := "same"
	if drew {
		state = "drew"
	}
	chatperfTrace("event " + strconv.Itoa(int(kind)) + " " + state)
}

// chatperfConn marks the start and end of every call made on conn.
func chatperfConn(conn grpc.ClientConnInterface) grpc.ClientConnInterface {
	return chatperfTracedConn{conn}
}

type chatperfTracedConn struct{ grpc.ClientConnInterface }

func (c chatperfTracedConn) Invoke(ctx context.Context, method string, args, reply any, opts ...grpc.CallOption) error {
	name := method[strings.LastIndex(method, "/")+1:]
	chatperfTrace("rpc> " + name)
	err := c.ClientConnInterface.Invoke(ctx, method, args, reply, opts...)
	chatperfTrace("rpc< " + name)
	return err
}

func (c chatperfTracedConn) NewStream(ctx context.Context, desc *grpc.StreamDesc, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	chatperfTrace("stream " + method[strings.LastIndex(method, "/")+1:])
	return c.ClientConnInterface.NewStream(ctx, desc, method, opts...)
}
