package main

import "sync/atomic"

// productLoaderSettled is true once any route loader run has returned.
// UXBLIND-089: retries before the first settle are dropped (see
// guardBootRevalidate); every loader return, success or failure, settles.
var productLoaderSettled atomic.Bool

// guardBootRevalidate wraps a route revalidation so a call that lands
// before the first loader run has settled is dropped instead of run. A
// boot-time revalidation cancels the in-flight bootstrap load and repeats
// its reads (measured: a chat repaint tick fired mid-boot re-read the
// whole bootstrap on Home), while the loader's own answer already reads
// everything fresh. Once any run has settled, retries pass through.
func guardBootRevalidate(retry func()) func() {
	return func() {
		if retry == nil || !productLoaderSettled.Load() {
			return
		}
		retry()
	}
}
