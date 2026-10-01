//go:build js && wasm

package main

import (
	"errors"
	"fmt"
	"sync"
	"syscall/js"

	"github.com/monstercameron/human-capital-management-suite/internal/timeclockapp"
)

// clockLocalStorage is the only adapter used by the kiosk for its encrypted
// device record, sequence and offline queue. The browser API is deliberately
// kept here so the WEB-031 source gate can review this durable boundary as a
// unit.
type clockLocalStorage struct{ value js.Value }

func newClockLocalStorage() (clockLocalStorage, error) {
	value := js.Global().Get("localStorage")
	if value.IsUndefined() || value.IsNull() || value.Type() != js.TypeObject {
		return clockLocalStorage{}, errors.New("timeclock host: local storage unavailable")
	}
	return clockLocalStorage{value: value}, nil
}

func (s clockLocalStorage) Load(key string) (string, bool) {
	var value js.Value
	failed := false
	func() {
		defer func() {
			if recover() != nil {
				failed = true
			}
		}()
		value = s.value.Call("getItem", key)
	}()
	if failed {
		return "", true
	}
	if value.IsNull() || value.IsUndefined() {
		return "", false
	}
	return value.String(), true
}

func (s clockLocalStorage) Save(key, value string) error {
	failed := false
	func() {
		defer func() {
			if recover() != nil {
				failed = true
			}
		}()
		s.value.Call("setItem", key, value)
	}()
	if failed {
		return fmt.Errorf("timeclock host: local storage write failed")
	}
	return nil
}

var retainedFuncs []js.Func

func retain(fn js.Func) js.Func {
	retainedFuncs = append(retainedFuncs, fn)
	return fn
}

// prepareDataKey keeps the wrapping key as a non-extractable CryptoKey in
// IndexedDB. Only the short-lived data key is exported to Go; neither key is
// written to localStorage. The callback runs after the durable key record has
// been committed, so mounting the model cannot claim persistence prematurely.
func prepareDataKey(done func([]byte, error)) {
	var once sync.Once
	original := done
	done = func(key []byte, err error) { once.Do(func() { original(key, err) }) }
	idb := js.Global().Get("indexedDB")
	if idb.IsUndefined() || idb.IsNull() {
		done(nil, errors.New("indexeddb is unavailable"))
		return
	}
	request := idb.Call("open", "hcm.timeclock", 1)
	request.Set("onupgradeneeded", retain(js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		db := args[0].Get("target").Get("result")
		names := db.Get("objectStoreNames")
		if !names.Call("contains", "keys").Bool() {
			db.Call("createObjectStore", "keys")
		}
		return nil
	})))
	request.Set("onerror", retain(js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		done(nil, errors.New("indexeddb open failed"))
		return nil
	})))
	request.Set("onsuccess", retain(js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		db := args[0].Get("target").Get("result")
		tx := db.Call("transaction", "keys", "readwrite")
		store := tx.Call("objectStore", "keys")
		get := store.Call("get", "wrapping")
		get.Set("onerror", retain(js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			done(nil, errors.New("indexeddb key read failed"))
			return nil
		})))
		get.Set("onsuccess", retain(js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			wrapping := args[0].Get("target").Get("result")
			if wrapping.IsUndefined() || wrapping.IsNull() {
				generateWrapping(db, done)
				return nil
			}
			loadDataKey(wrapping, store, done)
			return nil
		})))
		return nil
	})))
}

func generateWrapping(db js.Value, done func([]byte, error)) {
	subtle := js.Global().Get("crypto").Get("subtle")
	algorithm := js.ValueOf(map[string]interface{}{"name": "AES-GCM", "length": 256})
	usages := js.ValueOf([]interface{}{"wrapKey", "unwrapKey"})
	promise := subtle.Call("generateKey", algorithm, false, usages)
	thenPromise(promise, func(key js.Value) {
		generateDataKey(db, key, done)
	}, done)
}

func generateDataKey(db, wrapping js.Value, done func([]byte, error)) {
	subtle := js.Global().Get("crypto").Get("subtle")
	algorithm := js.ValueOf(map[string]interface{}{"name": "AES-GCM", "length": 256})
	dataPromise := subtle.Call("generateKey", algorithm, true, js.ValueOf([]interface{}{"encrypt", "decrypt"}))
	thenPromise(dataPromise, func(data js.Value) {
		iv := js.Global().Get("Uint8Array").New(12)
		js.Global().Get("crypto").Call("getRandomValues", iv)
		wrappedPromise := subtle.Call("wrapKey", "raw", data, wrapping, js.ValueOf(map[string]interface{}{"name": "AES-GCM", "iv": iv}))
		thenPromise(wrappedPromise, func(wrapped js.Value) {
			tx := db.Call("transaction", "keys", "readwrite")
			store := tx.Call("objectStore", "keys")
			store.Call("put", wrapping, "wrapping")
			store.Call("put", js.ValueOf(map[string]interface{}{"wrapped": wrapped, "iv": iv}), "data")
			tx.Set("oncomplete", retain(js.FuncOf(func(this js.Value, args []js.Value) interface{} {
				exportDataKey(data, done)
				return nil
			})))
			tx.Set("onerror", retain(js.FuncOf(func(this js.Value, args []js.Value) interface{} {
				done(nil, errors.New("indexeddb key commit failed"))
				return nil
			})))
			tx.Set("onabort", retain(js.FuncOf(func(this js.Value, args []js.Value) interface{} {
				done(nil, errors.New("indexeddb key commit aborted"))
				return nil
			})))
		}, done)
	}, done)
}

func loadDataKey(wrapping, store js.Value, done func([]byte, error)) {
	get := store.Call("get", "data")
	get.Set("onerror", retain(js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		done(nil, errors.New("indexeddb data key read failed"))
		return nil
	})))
	get.Set("onsuccess", retain(js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		record := args[0].Get("target").Get("result")
		if record.IsUndefined() || record.IsNull() {
			done(nil, errors.New("wrapped data key is missing"))
			return nil
		}
		subtle := js.Global().Get("crypto").Get("subtle")
		promise := subtle.Call("unwrapKey", "raw", record.Get("wrapped"), wrapping, js.ValueOf(map[string]interface{}{"name": "AES-GCM", "iv": record.Get("iv")}), js.ValueOf(map[string]interface{}{"name": "AES-GCM", "length": 256}), true, js.ValueOf([]interface{}{"encrypt", "decrypt"}))
		thenPromise(promise, func(data js.Value) { exportDataKey(data, done) }, done)
		return nil
	})))
}

func exportDataKey(data js.Value, done func([]byte, error)) {
	promise := js.Global().Get("crypto").Get("subtle").Call("exportKey", "raw", data)
	thenPromise(promise, func(raw js.Value) {
		view := js.Global().Get("Uint8Array").New(raw)
		bytes := make([]byte, view.Get("byteLength").Int())
		js.CopyBytesToGo(bytes, view)
		done(bytes, nil)
	}, done)
}

func thenPromise(promise js.Value, success func(js.Value), done func([]byte, error)) {
	promise.Call("then", retain(js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		success(args[0])
		return nil
	}))).Call("catch", retain(js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		done(nil, errors.New("webcrypto operation failed"))
		return nil
	})))
}

func prepareClockStorage(done func(timeclockapp.Storage, error)) {
	prepareDataKey(func(key []byte, err error) {
		if err != nil {
			done(nil, err)
			return
		}
		base, err := newClockLocalStorage()
		if err != nil {
			done(nil, err)
			return
		}
		store, err := timeclockapp.NewEncryptedStorage(base, key)
		if err != nil {
			done(nil, errors.New("secure storage unavailable"))
			return
		}
		done(store, nil)
	})
}
