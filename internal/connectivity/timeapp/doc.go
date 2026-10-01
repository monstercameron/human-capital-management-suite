// Package timeapp imports time punches from third-party time applications.
//
// The package is a thin provider translator: webhook admission and replay are
// delegated to connectivity/webhook, resume and change classification to
// connectivity/syncjob, and worker identity to an injected authoritative
// linker. It writes only through ClockObservationPort; it never evaluates
// attendance or writes timecards.
package timeapp
