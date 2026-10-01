// Package clockadapters is the application composition boundary for hardware
// clock protocol adapters.
//
// Protocol parsing and HTTP details remain in transport/clockadapter. The
// exported contracts here are the authenticated-device, canonical-ingest,
// roster and content-addressed batch seams used by application composition.
package clockadapters
