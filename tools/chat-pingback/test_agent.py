import importlib.util
import tempfile
import unittest
from pathlib import Path


MODULE = Path(__file__).with_name("agent.py")
SPEC = importlib.util.spec_from_file_location("chat_pingback_agent", MODULE)
agent = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(agent)


class PingbackAgentTests(unittest.TestCase):
    def test_explicit_trigger_echoes_payload_and_ignores_nontriggers_and_self(self):
        self.assertEqual(agent.trigger_reply("@pingback please check", "alice", "demo-agent"), "Pingback echo: please check")
        self.assertEqual(agent.trigger_reply("@pingback", "alice", "demo-agent"), "Pingback echo:")
        self.assertIsNone(agent.trigger_reply("hello @pingback", "alice", "demo-agent"))
        self.assertIsNone(agent.trigger_reply("@pingback again", "demo-agent", "demo-agent"))

    def test_event_idempotency_survives_duplicate_delivery_and_restart(self):
        first = agent.idempotency_key("room-a", 42)
        duplicate = agent.idempotency_key("room-a", 42)
        self.assertEqual(first, duplicate)
        self.assertNotEqual(first, agent.idempotency_key("room-a", 43))
        with tempfile.TemporaryDirectory() as directory:
            state = Path(directory) / "cursor.json"
            agent.save_cursor(state, "opaque-signed-cursor")
            self.assertEqual(agent.load_cursor(state), "opaque-signed-cursor")

    def test_http_requires_loopback_and_remote_uses_https(self):
        self.assertEqual(agent.validate_base_url("http://127.0.0.1:8888/"), "http://127.0.0.1:8888")
        self.assertEqual(agent.validate_base_url("https://chat.example.test/"), "https://chat.example.test")
        with self.assertRaises(ValueError):
            agent.validate_base_url("http://chat.example.test")
        with self.assertRaises(ValueError):
            agent.validate_base_url("https://user:secret@chat.example.test")


if __name__ == "__main__":
    unittest.main()
