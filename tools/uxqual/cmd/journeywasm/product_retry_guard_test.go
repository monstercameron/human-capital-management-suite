package main

import "testing"

// UXBLIND-089: a boot-time revalidation cancels the in-flight bootstrap
// load and repeats its reads, so it is dropped until the first loader run
// settles.
func TestGuardBootRevalidateDropsBeforeSettle(t *testing.T) {
	productLoaderSettled.Store(false)
	t.Cleanup(func() { productLoaderSettled.Store(false) })
	called := false
	guardBootRevalidate(func() { called = true })()
	if called {
		t.Fatal("guardBootRevalidate ran the retry before the first loader run settled")
	}
}

func TestGuardBootRevalidatePassesAfterSettle(t *testing.T) {
	productLoaderSettled.Store(true)
	t.Cleanup(func() { productLoaderSettled.Store(false) })
	called := false
	guardBootRevalidate(func() { called = true })()
	if !called {
		t.Fatal("guardBootRevalidate dropped the retry after the loader settled")
	}
}

func TestGuardBootRevalidateNilRetryIsNoop(t *testing.T) {
	productLoaderSettled.Store(true)
	t.Cleanup(func() { productLoaderSettled.Store(false) })
	guardBootRevalidate(nil)()
}
