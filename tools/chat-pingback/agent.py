#!/usr/bin/env python3
"""Local demo agent for the resource-scoped HCM chat API."""
from __future__ import annotations

import argparse
import ipaddress
import json
import os
import sys
import urllib.error
import urllib.parse
import urllib.request
import uuid
from pathlib import Path
from typing import Any

# Stable namespace makes retries of one source event reuse the same API key.
_IDEMPOTENCY_NAMESPACE = uuid.UUID("c22d3f55-06c7-5f3c-9ab0-bfe5f7dded0b")
_TRIGGER = "@pingback"
_POST_CREATED = "POST_CREATED"


def trigger_reply(body: str, author_id: str, agent_id: str) -> str | None:
    """Return the echoed payload for a human callout, or None to ignore it."""
    if not body or author_id == agent_id:
        return None
    if body == _TRIGGER:
        return "Pingback echo:"
    if body.startswith(_TRIGGER) and len(body) > len(_TRIGGER) and body[len(_TRIGGER)].isspace():
        payload = body[len(_TRIGGER):].strip()
        return f"Pingback echo: {payload}" if payload else "Pingback echo:"
    return None


def idempotency_key(conversation_id: str, sequence: int) -> str:
    return str(uuid.uuid5(_IDEMPOTENCY_NAMESPACE, f"{conversation_id}\0{sequence}"))


def validate_base_url(value: str) -> str:
    parsed = urllib.parse.urlparse(value)
    if (parsed.scheme not in {"http", "https"} or not parsed.netloc or parsed.username or parsed.password
            or parsed.path not in {"", "/"} or parsed.query or parsed.fragment):
        raise ValueError("base URL must be an http(s) origin without embedded credentials")
    if parsed.scheme == "http":
        host = parsed.hostname or ""
        try:
            loopback = ipaddress.ip_address(host).is_loopback
        except ValueError:
            loopback = host.lower() == "localhost"
        if not loopback:
            raise ValueError("plain HTTP is allowed only for localhost; use HTTPS for remote servers")
    return value.rstrip("/")


def request_json(url: str, token: str, *, method: str = "GET", payload: dict[str, Any] | None = None,
                 idempotency: str | None = None, timeout: float = 10) -> dict[str, Any]:
    data = json.dumps(payload).encode("utf-8") if payload is not None else None
    headers = {"Authorization": f"Bearer {token}", "Accept": "application/json"}
    if data is not None:
        headers["Content-Type"] = "application/json"
    if idempotency:
        headers["Idempotency-Key"] = idempotency
    req = urllib.request.Request(url, data=data, headers=headers, method=method)
    with urllib.request.urlopen(req, timeout=timeout) as response:
        result = json.load(response)
    if not isinstance(result, dict):
        raise ValueError("chat API returned a non-object JSON response")
    return result


def load_cursor(path: Path) -> str:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except FileNotFoundError:
        return ""
    cursor = value.get("resume_cursor") if isinstance(value, dict) else None
    if not isinstance(cursor, str):
        raise ValueError("state file has no valid resume_cursor")
    return cursor


def save_cursor(path: Path, cursor: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    temp = path.with_suffix(path.suffix + ".tmp")
    temp.write_text(json.dumps({"resume_cursor": cursor}) + "\n", encoding="utf-8")
    os.replace(temp, path)


def run(base_url: str, conversation_id: str, token: str, agent_id: str,
        state_file: Path, *, once: bool = False, dry_run: bool = False, wait_ms: int = 1000) -> None:
    base_url = validate_base_url(base_url)
    root = f"{base_url}/v1/conversations/{urllib.parse.quote(conversation_id, safe='')}"
    cursor = load_cursor(state_file)
    print(f"Pingback demo connected to conversation {conversation_id}; trigger: @pingback <message>", flush=True)
    while True:
        query = urllib.parse.urlencode({"max_events": 50, "wait_ms": wait_ms, **({"resume_cursor": cursor} if cursor else {})})
        page = request_json(f"{root}/events?{query}", token, timeout=wait_ms / 1000 + 5)
        events = page.get("events", [])
        next_cursor = page.get("resume_cursor", cursor)
        if not isinstance(events, list) or not isinstance(next_cursor, str):
            raise ValueError("chat API returned an invalid event page")
        for event in events:
            if not isinstance(event, dict) or event.get("kind") != _POST_CREATED:
                continue
            post = event.get("post")
            if not isinstance(post, dict):
                continue
            body = post.get("body")
            author_id = post.get("author_id")
            sequence = event.get("sequence")
            if not isinstance(body, str) or not isinstance(author_id, str) or type(sequence) is not int or sequence < 1:
                continue
            reply = trigger_reply(body, author_id, agent_id)
            if reply is not None:
                if dry_run:
                    print(f"Would reply to post {post.get('id')} at event {sequence}: {reply}", flush=True)
                else:
                    parent_id = post.get("id")
                    if not isinstance(parent_id, str) or not parent_id:
                        raise ValueError("trigger event is missing its source post ID")
                    request_json(root + "/posts", token, method="POST",
                                 payload={"body": reply, "parent_id": parent_id},
                                 idempotency=idempotency_key(conversation_id, sequence))
                    print(f"Echoed callout at event {sequence}", flush=True)
        if next_cursor:
            save_cursor(state_file, next_cursor)
            cursor = next_cursor
        if once:
            return


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base-url", default="http://127.0.0.1:8888", help="chat API origin")
    parser.add_argument("--conversation", required=True, help="conversation ID from Copy API curl")
    parser.add_argument("--state-file", default=".artifacts/chat-pingback/state.json", help="local resume cursor file")
    parser.add_argument("--once", action="store_true", help="pull one bounded page then exit")
    parser.add_argument("--dry-run", action="store_true", help="show replies without posting")
    parser.add_argument("--wait-ms", type=int, default=1000, help="bounded poll wait (1-5000)")
    args = parser.parse_args()
    token = os.environ.get("HCM_CHAT_TOKEN", "").strip()
    agent_id = os.environ.get("HCM_CHAT_AGENT_ID", "").strip()
    if not token or not agent_id:
        parser.error("set HCM_CHAT_TOKEN and HCM_CHAT_AGENT_ID to the installed agent credential and its exact subject ID")
    if not 1 <= args.wait_ms <= 5000:
        parser.error("--wait-ms must be from 1 through 5000")
    try:
        run(args.base_url, args.conversation, token, agent_id, Path(args.state_file), once=args.once,
            dry_run=args.dry_run, wait_ms=args.wait_ms)
    except (OSError, ValueError, urllib.error.URLError) as exc:
        print(f"pingback: {exc}", file=sys.stderr)
        return 1
    except KeyboardInterrupt:
        print("\nPingback demo stopped", file=sys.stderr)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
