package machineauth

import (
	"bytes"
	"encoding/base64"
)

// b64Codec is strict unpadded base64url: one byte string has exactly one
// spelling, the same codec the token format uses.
var b64Codec = base64.RawURLEncoding.Strict()

func bytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }
