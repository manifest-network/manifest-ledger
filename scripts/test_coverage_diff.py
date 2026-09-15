import contextlib
from decimal import Decimal
import importlib.util
import io
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


SCRIPT = Path(__file__).with_name("coverage-diff.py").resolve()
spec = importlib.util.spec_from_file_location("coverage_diff", SCRIPT)
coverage = importlib.util.module_from_spec(spec)
spec.loader.exec_module(coverage)
MODULE = "example.com/coverage-fixture"


def profile(*blocks, mode="count"):
    return f"mode: {mode}\n" + "".join(f"{MODULE}/{block}\n" for block in blocks)


class ProfileTest(unittest.TestCase):
    def test_adjacent_blocks_and_empty_branch_are_valid(self):
        parsed = coverage.parse_profile(profile(
            "a.go:2.1,2.5 2 100", "a.go:2.5,2.9 8 0", "a.go:3.1,3.1 0 1",
        ), MODULE)
        self.assertEqual(3, len(parsed["a.go"]))

    def test_malformed_and_unmerged_profiles_fail(self):
        for text in (
            "", "mode: other\n", "mode: count\n", "mode: count\ninvalid\n",
            profile("a.go:1.1,2.1 -1 0"), profile("a.go:1.1,2.1 1 -1"),
            profile("a.go:0.1,2.1 1 0"), profile("a.go:1.0,2.1 1 0"),
            profile("a.go:2.1,1.1 1 0"), profile("a.go:1.1,1.1 1 0"),
            profile("a.go:1.1,2.1 1 2", mode="set"),
            profile("../a.go:1.1,2.1 1 0"), profile("/a.go:1.1,2.1 1 0"),
            profile("a//b.go:1.1,2.1 1 0"), profile("./a.go:1.1,2.1 1 0"),
            profile("a.txt:1.1,2.1 1 0"), "mode: count\nforeign/a.go:1.1,2.1 1 0\n",
            profile("a.go:1.1,2.1 1 0", "a.go:1.1,2.1 1 1"),
            profile("a.go:1.1,4.1 1 0", "a.go:3.1,5.1 2 1"),
            profile("a.go:1.1,2.1 1 1", "a.go:3.1,3.1 0 0", "a.go:3.1,3.1 0 1"),
        ):
            with self.subTest(profile=text), self.assertRaises(ValueError):
                coverage.parse_profile(text, MODULE)


class GitDiffTest(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.repo = Path(self.directory.name)
        self.git("init", "--quiet")
        self.write("go.mod", f"module {MODULE}\n")
        self.write("unchanged.go", "package fixture\nfunc Unchanged() { println(1) }\n")
        self.base = self.commit()

    def git(self, *args):
        return subprocess.run(
            ["git", "-C", str(self.repo), "-c", "user.name=Coverage Fixture",
             "-c", "user.email=coverage@example.invalid", "-c", "commit.gpgSign=false", *args],
            check=True, capture_output=True, text=True,
        ).stdout.strip()

    def write(self, path, text):
        target = self.repo / path
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(text)

    def commit(self):
        self.git("add", "--all")
        self.git("commit", "--quiet", "-m", "fixture")
        return self.git("rev-parse", "HEAD")

    def analyze(self, text, head=None):
        return coverage.analyze(text, self.repo, self.base, head or self.commit())

    def rendered(self, result, floor="80"):
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            status = coverage.render(result, Decimal(floor))
        return status, output.getvalue()

    def test_uncovered_changed_block_fails_without_counting_unchanged_coverage(self):
        self.write("changed.go", "package fixture\nfunc Changed() { println(1) }\n")
        result = self.analyze(profile("unchanged.go:2.1,2.30 100 99", "changed.go:2.1,2.30 1 0"))
        self.assertEqual((0, 1), (result["covered"], result["statements"]))
        self.assertEqual(1, self.rendered(result)[0])

    def test_statement_weights_and_multiple_added_lines_count_each_block_once(self):
        self.write("changed.go", "package fixture\nfunc Changed() {\n println(1)\n println(2)\n}\n")
        result = self.analyze(profile("changed.go:2.1,5.2 8 100", "changed.go:5.2,5.3 2 0"))
        self.assertEqual([("changed.go", 8, 10, 2)], result["rows"])
        self.assertEqual(0, self.rendered(result, "80")[0])
        self.assertEqual(1, self.rendered(result, "80.001")[0])

    def test_more_than_300_files_do_not_hide_an_uncovered_go_file(self):
        for number in range(305):
            self.write(f"docs/{number:03}.txt", "changed documentation\n")
        self.write("zzz/changed.go", "package fixture\nfunc Changed() { println(1) }\n")
        result = self.analyze(profile("zzz/changed.go:2.1,2.30 1 0"))
        self.assertEqual(306, result["changed_files"])
        self.assertEqual([("zzz/changed.go", 0, 1, 1)], result["rows"])
        self.assertEqual(1, self.rendered(result)[0])

    def test_literal_filenames_and_binary_attributes_do_not_hide_changed_go(self):
        names = ("space name.go", "tab\tname.go", "[glob]*.go", "colon:name.go", "-leading.go")
        self.write(".gitattributes", "*.go -diff\n")
        for name in names:
            self.write(name, "package fixture\nfunc Changed() { println(1) }\n")
        result = self.analyze(profile(*(f"{name}:2.1,2.30 1 0" for name in names)))
        self.assertEqual(len(names), result["statements"])
        self.assertEqual(set(names), {row[0] for row in result["rows"]})
        self.assertEqual(1, self.rendered(result)[0])

    def test_test_and_generated_sources_are_excluded(self):
        names = ("a_test.go", "a.pb.go", "a.pb.gw.go", "a.pulsar.go")
        for name in names:
            self.write(name, "package fixture\nfunc Generated() { println(1) }\n")
        result = self.analyze(profile("unchanged.go:2.1,2.30 1 1", *(f"{name}:2.1,2.30 50 0" for name in names)))
        self.assertEqual(0, result["eligible_files"])
        status, output = self.rendered(result)
        self.assertEqual(0, status)
        self.assertIn("N/A", output)
        self.assertNotIn("100.00%", output)

    def test_diff_configuration_and_source_text_do_not_invent_added_lines(self):
        original = ("package fixture\nfunc First() { println(1) }\n"
                    "func Stable() { println(2) }\nfunc Last() { println(3) }\n")
        self.write("changed.go", original)
        self.base = self.commit()
        self.git("config", "diff.interHunkContext", "20")
        self.git("config", "color.ui", "always")
        self.git("config", "diff.external", "false")
        # Unicode separators and carriage returns are source text, not Git
        # record boundaries. A fake header inside a Go comment must stay text.
        changed = original.replace("println(1)", "println(10)").replace("println(3)", "println(30)")
        self.write("changed.go", changed + "// \u2028@@ -1 +3 @@\r@@ -1 +3 @@\n")
        result = self.analyze(profile(
            "changed.go:2.1,2.30 1 0", "changed.go:3.1,3.30 100 1", "changed.go:4.1,4.30 1 0",
        ))
        self.assertEqual(3, result["added_lines"])
        self.assertEqual([("changed.go", 0, 2, 2)], result["rows"])
        self.assertEqual(1, self.rendered(result)[0])

    def test_deletion_only_and_declarations_only_have_explicit_empty_result(self):
        self.write("unchanged.go", "package fixture\n")
        self.write("constants.go", "package fixture\nconst Answer = 42\n")
        result = self.analyze(profile("some_other_file.go:2.1,2.30 1 1"))
        self.assertEqual(["constants.go"], result["absent"])
        self.assertEqual(1, result["eligible_files"])
        status, output = self.rendered(result)
        self.assertEqual(0, status)
        self.assertIn("N/A", output)
        self.assertIn("must establish source completeness", output)

    def test_cli_status_and_revision_validation(self):
        self.write("changed.go", "package fixture\nfunc Changed() { println(1) }\n")
        head = self.commit()
        path = self.repo / "coverage.out"
        path.write_text(profile("changed.go:2.1,2.30 4 1"))
        for base, floor, expected in (
            (self.base, "80", 0), (self.base, "100.01", 2), (self.base, "NaN", 2),
            (self.base, "Infinity", 2), ("--help", "80", 2), ("missing-revision", "80", 2),
        ):
            with self.subTest(base=base, floor=floor):
                result = subprocess.run(
                    [sys.executable, "-B", str(SCRIPT), "--repo", str(self.repo),
                     "--", str(path), base, head, floor], capture_output=True, text=True,
                )
                self.assertEqual(expected, result.returncode, result.stderr)
        path.write_text(profile("changed.go:2.1,2.30 1 0"))
        result = subprocess.run(
            [sys.executable, "-B", str(SCRIPT), str(path), self.base, head, "80", "--repo", str(self.repo)],
            capture_output=True, text=True,
        )
        self.assertEqual(1, result.returncode, result.stderr)
        self.assertIn("0/1 (0.00%)", result.stdout)


if __name__ == "__main__":
    unittest.main()
