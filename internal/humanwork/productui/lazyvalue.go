package productui

import "sync"

// lazyValue builds its value once, on first use, instead of at package init.
// Cold start pays for every package-level table and parser whether the loaded
// page needs it or not (UXBLIND-089); values held here cost nothing until the
// page that reads them renders. Get is safe for concurrent first use.
type lazyValue[T any] struct {
	build func() T
	once  sync.Once
	value T
}

func newLazyValue[T any](build func() T) *lazyValue[T] {
	return &lazyValue[T]{build: build}
}

// Get answers the built value, building it on the first call.
func (l *lazyValue[T]) Get() T {
	l.once.Do(func() { l.value = l.build() })
	return l.value
}
