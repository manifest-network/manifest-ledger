import importlib.util
from pathlib import Path
import unittest


spec = importlib.util.spec_from_file_location(
    "coverage_summary", Path(__file__).with_name("coverage-summary.py")
)
coverage = importlib.util.module_from_spec(spec)
spec.loader.exec_module(coverage)


class CoverageSummaryTest(unittest.TestCase):
    def test_statement_weighting_and_uncovered_packages(self):
        self.assertEqual(
            {"module/pkg": [2, 10], "module/other": [0, 3]},
            coverage.summarize(
                "mode: atomic\nmodule/pkg/a.go:1.1,2.1 2 100\n"
                "module/pkg/a.go:3.1,4.1 8 0\nmodule/other/b.go:1.1,2.1 3 0\n"
            ),
        )

    def test_invalid_or_unmerged_profiles_are_rejected(self):
        for profile in (
            "", "mode: other\n", "mode: set\nm/a.go:1.1,2.1 -2 0\n",
            "mode: set\nm/a.go:1.1,2.1 2 -1\n",
            "mode: set\nm/a.go:1.1,2.1 2 0\nm/a.go:1.1,2.1 2 1\n",
        ):
            with self.subTest(profile=profile), self.assertRaises(ValueError):
                coverage.summarize(profile)
