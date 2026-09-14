import copy
import importlib.util
import json
from pathlib import Path
import subprocess
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location(
    "release_controls", Path(__file__).with_name("verify-release-controls.py")
)
controls = importlib.util.module_from_spec(spec)
spec.loader.exec_module(controls)


class ReleaseControlsTest(unittest.TestCase):
    def test_metadata_and_order_are_not_policy_drift(self):
        expected = {"rules": [{"type": "creation"}, {"type": "deletion"}]}
        actual = {"id": 123, "rules": [{"type": "deletion"}, {"type": "creation"}]}
        self.assertTrue(controls.matches(actual, expected))

    def test_additional_bypass_or_missing_check_fails_closed(self):
        policy = json.loads(
            (Path(__file__).resolve().parent.parent / ".github/release-controls.json").read_text()
        )["requirements"]
        for endpoint in (key for key in policy if key.startswith("rulesets/")):
            expected = policy[endpoint]
            actual = copy.deepcopy(expected)
            actual["bypass_actors"].append(
                {"actor_type": "RepositoryRole", "actor_id": 5, "bypass_mode": "always"}
            )
            self.assertFalse(controls.matches(actual, expected))
        expected = policy["rulesets/487057"]
        actual = copy.deepcopy(expected)
        actual["rules"][-1]["parameters"]["required_status_checks"].pop()
        self.assertFalse(controls.matches(actual, expected))

    def test_duplicate_list_items_cannot_stand_in_for_missing_control(self):
        self.assertFalse(controls.matches([{"id": 1}, {"id": 1}], [{"id": 1}, {"id": 2}]))
        self.assertFalse(controls.matches({"enabled": 1}, {"enabled": True}))

    @patch("builtins.print")
    @patch.object(controls.subprocess, "run", side_effect=subprocess.TimeoutExpired("gh", 30))
    def test_api_failure_never_verifies_controls(self, run, output):
        self.assertEqual(1, controls.main())
        self.assertGreater(run.call_count, 0)
        self.assertIn("API read failed", output.call_args.args[0])
