package timestore

import "embed"

// Migrations is the time-keeping-owned migration tree. Ranges are reserved
// per concern so parallel work never collides on a version number:
// 00001-00009 foundation, sessions, observations and the event outbox;
// 00010-00019 devices, enrollment, heartbeats, photos and biometric consent;
// 00020-00029 profiles, timecards, shifts and missed-punch requests;
// 00030-00039 working-time ledger and destinations.
//
//go:embed migrations/*.sql
var Migrations embed.FS
