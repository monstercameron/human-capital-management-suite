// Package timeclockapp is the reference wall-mounted clock-in/clock-out
// kiosk (TCLOCK-016, scoped to clock in and clock out only).
//
// The package is the whole client minus the browser: a state machine
// (enroll -> idle -> identify -> confirm -> receipt -> idle), the offline
// punch queue, the en-US/de-DE/ar catalog and formatting, a protojson
// client for the public ClockDeviceService, and a hook-free GoWebComponents
// view that renders a Snapshot. Nothing here touches syscall/js, so every
// rule is proven by ordinary `go test`; cmd/timeclock/wasm is the thin
// browser host (localStorage, timers, delegated DOM events) and
// internal/timeclockapp/kioskserver serves the page and proxies the device
// API same-origin.
//
// # Contract decisions
//
//   - Only the device-facing ClockDeviceService methods in DeviceMethods
//     are ever called. No admin, workspace or TimeService route exists in
//     the client, and the kiosk server refuses every other path.
//   - EnrollDevice signs EnrollmentChallenge(code): the enrollment code is
//     the out-of-band server challenge the proto describes. A transport
//     lane that issues a separate challenge replaces that one function.
//   - Every punch is written to the persisted queue before the network is
//     tried, then the whole queue is flushed in device-sequence order, so a
//     punch made while offline and a punch made online take the same path
//     and a restart between the two loses nothing. The queue is trimmed only
//     up to SubmitPunchesResponse.highest_contiguous_sequence.
//   - Worker data (display name, status, PIN) lives only in memory and is
//     cleared after every interaction and on the idle timeout. The queue
//     holds only what the server needs to attribute a punch: the short-lived
//     punch token, or the badge/QR verifier when the badge was scanned
//     offline. A PIN is never queued; offline PIN entry is refused.
//
// # Browser storage limitation
//
// The Ed25519 private key seed, the device id and the queue are kept in the
// browser's localStorage. localStorage is readable by any script running on
// the kiosk origin and is not encrypted at rest by the browser, so the page
// is served under a strict content-security policy with no third-party
// script, and the kiosk must run in a managed single-app mode (Apple Single
// App Mode / Autonomous Single App Mode under supervised MDM, Guided Access
// unmanaged, Android Enterprise dedicated-device mode) so no other page
// shares the origin. A non-extractable WebCrypto key would be stronger; it
// is not used because the client is Go-first and the transport's device
// authentication scheme is not yet defined.
package timeclockapp
