#!/usr/bin/env python3
import tempfile
import unittest
from pathlib import Path

import operator_attention_listener as listener


class OperatorAttentionListenerTest(unittest.TestCase):
    def test_normalize_message_keeps_sender_and_pointers(self):
        message = listener.normalize_message(
            {
                "senderAgent": "agent2",
                "category": "operator_input",
                "summary": "Need direction on the next task",
                "pointers": [{"kind": "artifact", "uri": "logs/report-card", "label": "report card"}],
            },
            {},
            {},
        )
        self.assertEqual(message["senderAgent"], "agent2")
        self.assertEqual(message["pointers"][0]["uri"], "logs/report-card")

    def test_rejects_raw_payload_fields(self):
        with self.assertRaisesRegex(ValueError, "raw payload"):
            listener.normalize_message(
                {
                    "senderAgent": "agent3",
                    "category": "operator_input",
                    "summary": "Need direction",
                    "payload": "raw log text",
                    "pointers": [{"kind": "artifact", "uri": "logs/report-card", "label": "report card"}],
                },
                {},
                {},
            )

    def test_store_consolidates_multiple_senders(self):
        with tempfile.TemporaryDirectory() as tmp:
            store = listener.InboxStore(Path(tmp) / "messages.jsonl")
            for sender in ("agent1", "agent7"):
                store.append(
                    listener.normalize_message(
                        {
                            "senderAgent": sender,
                            "category": "governance_approval",
                            "summary": f"{sender} needs review",
                            "pointers": [{"kind": "artifact", "uri": f"logs/{sender}", "label": sender}],
                        },
                        {},
                        {},
                    )
                )
            self.assertEqual([message["senderAgent"] for message in store.list()], ["agent1", "agent7"])


if __name__ == "__main__":
    unittest.main()
