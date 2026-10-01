package timeclockstore

import "errors"

// ErrNilStore reports an invalid production composition.
var ErrNilStore = errors.New("timeclockstore: nil timestore")
