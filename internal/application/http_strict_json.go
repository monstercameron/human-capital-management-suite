package application

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// errHTTPStrictJSONInvalid means an HTTP JSON body is malformed, names an
// unknown field, carries trailing data, or exceeds its byte limit. Callers map
// it to their own response; the body itself is never described back.
var errHTTPStrictJSONInvalid = errors.New("application: invalid JSON request body")

// decodeStrictJSONBody reads the one JSON value of an HTTP request body: at
// most limit bytes, unknown fields refused, trailing data refused. It is the
// shared form of the strict decode every JSON handler in this package repeats.
func decodeStrictJSONBody(w http.ResponseWriter, r *http.Request, limit int64, into any) error {
	if w == nil || r == nil || r.Body == nil || into == nil || limit <= 0 {
		return errHTTPStrictJSONInvalid
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(into) != nil || decoder.Decode(new(any)) != io.EOF {
		return errHTTPStrictJSONInvalid
	}
	return nil
}
