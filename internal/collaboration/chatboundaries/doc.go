// Package chatboundaries documents who owns the chat domain libraries and
// tests that they stay libraries: pure domain decisions with no imports from
// the UI, workspace, composition, transport or tooling layers.
//
// The chat domain lives in internal/collaboration/chat* (owner:
// experience-and-transport, see definitions/architecture/repository-layout.yaml).
// Protocol adapters stay in internal/transport, UI adapters in
// internal/humanwork, and composition in internal/application. A domain library
// that imports any of those layers has its dependency arrow backwards, so
// TestChatBoundaries fails the build on the first such import.
//
// The lateral edges inside the domain today:
//
//	chat -> chatpolicy, chatfilter, chatgate, chatrender
//	chatrecipient -> chat
//	chatpolicyadapter -> chat, chatpolicy
//	chatroutingadapter -> chat, chatpolicy, chatrouting
//	chatrewrite, chatsearch, chatrender, chatrecords, chatrouting, chatstream,
//	chatadmission, chatapps, chatcalls, chatfilter, chatgate, chatlang,
//	chatmedia, chatpilot, chatpolicy -> no other chat* package
//
// The test pins the layering rule, not this edge list: a new lateral edge
// between domain libraries is a review decision, while an upward import is
// always a bug.
package chatboundaries
