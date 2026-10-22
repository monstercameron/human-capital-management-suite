//go:build js && wasm

package main

import (
	"syscall/js"
	"testing"
)

func TestTodo_WEB_031_Browser(t *testing.T) {
	object := js.Global().Get("Object")
	storage := object.New()
	values := map[string]string{}
	setItem := js.FuncOf(func(_ js.Value, args []js.Value) any {
		values[args[0].String()] = args[1].String()
		return nil
	})
	getItem := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if value, ok := values[args[0].String()]; ok {
			return value
		}
		return nil
	})
	defer setItem.Release()
	defer getItem.Release()
	storage.Set("setItem", setItem)
	storage.Set("getItem", getItem)

	oldStorage := js.Global().Get("localStorage")
	js.Global().Set("localStorage", storage)
	t.Cleanup(func() { js.Global().Set("localStorage", oldStorage) })

	base, err := newClockLocalStorage()
	if err != nil {
		t.Fatalf("newClockLocalStorage() error = %v", err)
	}
	if err := base.Save("hcm.timeclock.queue", "encrypted-queue"); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if got, ok := base.Load("hcm.timeclock.queue"); !ok || got != "encrypted-queue" {
		t.Fatalf("Load() = %q/%v, want encrypted-queue/true", got, ok)
	}

	throwing := js.Global().Call("eval", `({setItem(){throw new Error("quota")},getItem(){throw new Error("denied")}})`)
	failed := clockLocalStorage{value: throwing}
	if got, ok := failed.Load("queue"); !ok || got != "" {
		t.Fatalf("failed Load() = %q/%v, want empty/true", got, ok)
	}
	if err := failed.Save("queue", "value"); err == nil {
		t.Fatal("failed Save() returned nil error")
	}
}
