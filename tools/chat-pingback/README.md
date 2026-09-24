# Local chat pingback demo

This small Python agent demonstrates the installed-agent chat API. It listens
to one conversation for a post whose body begins with `@pingback` followed by
whitespace, then replies in that post's thread with `Pingback echo: <message>`.
It ignores edits, ordinary mentions, and posts authored by its own machine
subject. The trigger text is removed from the reply so an echo cannot retrigger
the agent even if its configured subject is wrong.

## Run against the local preview

Start the HCM app on its existing local port (`8888`) and open the target
conversation. Use **Copy API curl** to get its conversation ID. The machine
identity must already be installed in that conversation with both
`chat.posts.read` and `chat.posts.write`; the token subject must equal the
installation's app ID. A development token alone does not create an
installation. Do not use a human login token.

In PowerShell, set the token and exact machine subject from the authorized
local credential issuer, then run:

For the local `local-dev` profile, the supervisor below mints a 15-minute
identity-only agent token in memory, including the existing app ID as its
stable quota client ID. It renews the token every 10 minutes and restarts the
listener if the API process exits during a local rebuild. It does not change
an installation or its scopes:

```powershell
powershell -NoProfile -ExecutionPolicy RemoteSigned -File tools/chat-pingback/run-local.ps1 `
  -ConversationId 'f4079843-2274-5bd8-a998-20598abeba88' -Background
```

The supervisor PID is recorded under `.artifacts/chat-pingback/`. Stop it and
its listener with the same command plus `-Stop`. Supervisor and listener logs
are also kept under `.artifacts/chat-pingback/`; bearer tokens are not logged.

To use a token from another authorized local issuer, set the environment
variables below and run the agent directly:

```powershell
$env:HCM_CHAT_TOKEN = '<short-lived installed-agent token>'
$env:HCM_CHAT_AGENT_ID = '<exact token subject / installed app ID>'
python tools/chat-pingback/agent.py --conversation '<conversation-id>'
```

The default URL is `http://127.0.0.1:8888`. Plain HTTP is accepted only for a
loopback host; a remote API must use HTTPS. The agent stores the signed resume
cursor in `.artifacts/chat-pingback/state.json`, so it can resume after a
restart. The state file contains no bearer token. Post creation uses a stable
UUID idempotency key derived from the source conversation and event sequence,
so redelivery retries do not create duplicate replies.

To inspect one bounded page without posting, use `--once --dry-run`:

```powershell
python tools/chat-pingback/agent.py --conversation '<conversation-id>' --once --dry-run
```

Start the listener, then send `@pingback hello from chat`. The agent posts
`Pingback echo: hello from chat` as a threaded reply to that message. Stop it
with Ctrl+C. Run the dependency-free checks with:

```powershell
python tools/chat-pingback/test_agent.py
go test ./internal/transport/chatresource ./internal/collaboration/chat
```
