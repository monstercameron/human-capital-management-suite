# Local chat pingback demo

Added `tools/chat-pingback/agent.py`, a small local machine client for the existing resource-scoped chat API. It listens for new posts beginning with `@pingback`, ignores its own posts, and posts an echo as a threaded reply. A stable event-derived idempotency key covers duplicate delivery; a local resume-cursor file supports restart. The CLI requires an installed agent identity and token through environment variables, uses only loopback HTTP, requires HTTPS remotely, and offers a dry-run mode. No credential is stored in source or state.

The chat resource now carries optional `parent_id` through the authenticated SendPost port. The service rejects missing, deleted, cross-room and wrong-tenant parents before writing. The CHAT-023 todo remains open because this small demo does not implement its complete ordered navigation, follow state, retained tombstone context or full test matrix.

Verification on Windows/arm64, Go 1.26.3: `python tools/chat-pingback/test_agent.py` passed (3 tests); `go test ./internal/collaboration/chat` passed; `go test ./internal/transport/chatresource` passed. No full repository gates or PostgreSQL integration test were run.
