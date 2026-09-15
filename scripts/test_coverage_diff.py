import contextlib
from decimal import Decimal
import importlib.util
import io
import os
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
            "a.go:2.3,2.3 1 0",
        ), MODULE)
        self.assertEqual(4, len(parsed["a.go"]))

    def test_malformed_and_unmerged_profiles_fail(self):
        for text in (
            "", "mode: other\n", "mode: count\n", "mode: count\ninvalid\n",
            profile("a.go:1.1,2.1 -1 0"), profile("a.go:1.1,2.1 1 -1"),
            profile("a.go:0.1,2.1 1 0"), profile("a.go:1.0,2.1 1 0"),
            profile("a.go:2.1,1.1 1 0"),
            profile("a.go:1.1,2.1 1 2", mode="set"),
            profile("../a.go:1.1,2.1 1 0"), profile("/a.go:1.1,2.1 1 0"),
            profile("a//b.go:1.1,2.1 1 0"), profile("./a.go:1.1,2.1 1 0"),
            profile("a.txt:1.1,2.1 1 0"), "mode: count\nforeign/a.go:1.1,2.1 1 0\n",
            profile("a.go:1.1,2.1 1 0", "a.go:1.1,2.1 1 1"),
            profile("a.go:1.1,4.1 1 0", "a.go:3.1,5.1 2 1"),
            profile("a.go:1.1,4.1 1 0", "a.go:2.1,2.1 1 0", "a.go:3.1,5.1 2 1"),
            profile("a.go:1.1,2.1 1 1", "a.go:3.1,3.1 0 0", "a.go:3.1,3.1 0 1"),
        ):
            with self.subTest(profile=text), self.assertRaises(ValueError):
                coverage.parse_profile(text, MODULE)


class GitDiffTest(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.repo = Path(self.directory.name) / "repo"
        self.repo.mkdir()
        self.artifacts = Path(self.directory.name) / "artifacts"
        self.artifacts.mkdir()
        self.git("init", "--quiet")
        self.write("go.mod", f"module {MODULE}\n")
        self.write(".coverageignore", "*.pb.go\n*.pb.gw.go\n*.pulsar.go\n")
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

    def go(self, *args):
        result = subprocess.run(
            ["go", *args], cwd=self.repo,
            env={**os.environ, "GOWORK": "off"},
            capture_output=True, text=True, timeout=120,
        )
        self.assertEqual(0, result.returncode, result.stdout + result.stderr)
        return result.stdout

    def actual_profile(self):
        path = self.artifacts / "coverage.out"
        self.go("test", "-count=1", "-covermode=atomic", "-coverpkg=" + MODULE + "/...",
                "-coverprofile=" + str(path), "./...")
        return path.read_text()

    def cover_function(self, name):
        self.write("fixture_test.go", "package fixture\nimport \"testing\"\n"
                   f"func TestCovered(t *testing.T) {{ {name}() }}\n")

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

    def test_multiline_statement_counts_once_and_floor_is_exact(self):
        self.write("changed.go", "package fixture\nfunc Covered() {\n"
                   " println(\n  1,\n )\n println(2)\n println(3)\n println(4)\n}\n"
                   "func Missed() { println(5) }\n")
        self.cover_function("Covered")
        result = self.analyze(self.actual_profile())
        self.assertEqual([("changed.go", 4, 5)], result["rows"])
        self.assertEqual({}, result["missing"])
        self.assertEqual(0, self.rendered(result, "80")[0])
        self.assertEqual(1, self.rendered(result, "80.001")[0])

    def test_unchanged_statements_in_large_covered_block_do_not_dilute_new_misses(self):
        original = "package fixture\nfunc Covered() {\n" + "".join(
            f" println({number})\n" for number in range(172)
        ) + "}\n"
        self.write("changed.go", original)
        self.cover_function("Covered")
        self.base = self.commit()
        self.write("changed.go", original.replace("println(0)", "println(999)") +
                   "func Uncovered() {\n" + "".join(
                       f" println({number})\n" for number in range(42)
                   ) + "}\n")
        actual = self.actual_profile()
        # These are actual Go block weights. Crediting the entire old block
        # would incorrectly report 172/214 (80.37%) for this one-line edit.
        blocks = coverage.parse_profile(actual, MODULE)["changed.go"]
        # Go 1.27 can repeat a basic block's NumStmt across split ranges.
        self.assertEqual({172, 42}, {block[2] for block in blocks})
        result = self.analyze(actual)
        self.assertEqual([("changed.go", 1, 43)], result["rows"])
        self.assertEqual({}, result["missing"])
        status, output = self.rendered(result)
        self.assertEqual(1, status)
        self.assertIn("1/43 (2.33%)", output)

    def test_comment_only_edit_receives_no_statement_credit(self):
        original = "package fixture\nfunc Covered() {\n // old comment\n println(1)\n}\n"
        self.write("changed.go", original)
        self.cover_function("Covered")
        self.base = self.commit()
        self.write("changed.go", original.replace("old comment", "new comment"))
        result = self.analyze(self.actual_profile())
        self.assertEqual({}, result["missing"])
        self.assertEqual([], result["rows"])
        status, output = self.rendered(result)
        self.assertEqual(0, status)
        self.assertIn("N/A", output)
        self.assertNotIn("100.00%", output)

    def test_pure_rename_cannot_dilute_new_uncovered_statements(self):
        self.write("covered.go", "package fixture\nfunc Covered() {\n" + "".join(
            f" println({number})\n" for number in range(40)
        ) + "}\n")
        self.cover_function("Covered")
        self.base = self.commit()
        self.git("mv", "covered.go", "moved.go")
        self.write("new.go", "package fixture\nfunc New() { println(1) }\n")
        result = self.analyze(self.actual_profile())
        self.assertEqual([("new.go", 0, 1)], result["rows"])
        self.assertEqual(1, self.rendered(result)[0])

    def test_modified_rename_counts_only_changed_statement_tokens(self):
        original = "package fixture\nfunc Covered() {\n" + "".join(
            f" println({number})\n" for number in range(40)
        ) + "}\n"
        self.write("covered.go", original)
        self.cover_function("Covered")
        self.base = self.commit()
        self.git("mv", "covered.go", "moved.go")
        self.write("moved.go", original.replace("println(0)", "println(999)"))
        self.write("new.go", "package fixture\nfunc New() { println(1) }\n")
        # Repository/user defaults must not disable rename detection.
        self.git("config", "diff.renames", "false")
        self.git("config", "diff.renameLimit", "1")
        result = self.analyze(self.actual_profile())
        self.assertEqual([("moved.go", 1, 1), ("new.go", 0, 1)], result["rows"])
        self.assertEqual({}, result["missing"])
        self.assertEqual(1, self.rendered(result)[0])

    def test_literal_rename_paths_and_excluded_source_entering_scope(self):
        original = "package fixture\nfunc Covered() { println(1) }\n"
        self.write("old [glob]:name.go", original)
        self.write("generated.pb.go", original)
        self.base = self.commit()
        self.git("mv", "--", "old [glob]:name.go", "new\tname.go")
        self.git("mv", "generated.pb.go", "included.go")
        result = self.analyze(profile("included.go:2.1,2.40 1 0"))
        self.assertEqual([("included.go", 0, 1)], result["rows"])
        self.assertEqual(1, self.rendered(result)[0])

    def test_shared_filter_policy_and_testdata_exclusions(self):
        self.write(".coverageignore", "# custom profile-path patterns\n*.pb.go\n"
                   f"{MODULE}/ignored:*.go\n")
        names = ("ignored:file.go", "a.pb.go", "testdata/fixture.go", "nested/testdata/fixture.go")
        for name in names:
            self.write(name, "package fixture\nfunc Ignored() { println(1) }\n")
        self.write("included.go", "package fixture\nfunc Included() { println(1) }\n")
        raw = self.artifacts / "raw.out"
        raw.write_text(profile("unchanged.go:2.1,2.40 1 1", "included.go:2.1,2.40 1 0",
                               *(f"{name}:2.1,2.40 1 1" for name in names)))
        filtered = self.artifacts / "filtered.out"
        subprocess.run([str(SCRIPT.with_name("filter-coverage.sh")), str(raw), str(filtered)],
                       cwd=self.repo, check=True, capture_output=True, text=True)
        result = self.analyze(filtered.read_text())
        self.assertEqual([("included.go", 0, 1)], result["rows"])
        self.assertEqual({}, result["missing"])
        self.assertEqual(1, self.rendered(result)[0])
        for name in names:
            self.assertNotIn(name + ":", filtered.read_text())

    def test_covdata_omitted_untested_leaf_fails_despite_other_coverage(self):
        self.write("changed.go", "package fixture\nfunc Covered() {\n" + "".join(
            f" println({number})\n" for number in range(9)
        ) + "}\n")
        self.cover_function("Covered")
        self.write("leaf/leaf.go", "package leaf\nfunc Leaf() {\n" + "".join(
            f" println({number})\n" for number in range(42)
        ) + "}\n")
        counters = self.artifacts / "counters"
        counters.mkdir()
        ordinary = self.artifacts / "coverage.out"
        self.go("test", "-count=1", "-covermode=atomic", "-coverpkg=" + MODULE + "/...",
                "-coverprofile=" + str(ordinary), "./...", "-args", "-test.gocoverdir=" + str(counters))
        path = self.artifacts / "covdata.out"
        self.go("tool", "covdata", "textfmt", "-i=" + str(counters), "-o=" + str(path))
        actual = path.read_text()
        # Raw runtime counters omit the package that has no test process.
        # The gate must inspect its changed source instead of blessing N/A.
        self.assertNotIn(MODULE + "/leaf/leaf.go:", actual)
        leaf_blocks = coverage.parse_profile(ordinary.read_text(), MODULE)["leaf/leaf.go"]
        self.assertEqual(42, max(block[2] for block in leaf_blocks))
        self.assertTrue(all(block[3] == 0 for block in leaf_blocks))
        head = self.commit()
        result = self.analyze(actual, head=head)
        self.assertEqual((9, 51), (result["covered"], result["statements"]))
        self.assertEqual({"leaf/leaf.go"}, set(result["missing"]))
        status, output = self.rendered(result)
        self.assertEqual(1, status)
        self.assertIn("incomplete coverage evidence", output)
        self.assertNotIn("N/A", output)

        # The ordinary Go profile supplies the leaf's real zero-hit blocks.
        # Merging it with runtime counters makes evidence complete, while
        # still rejecting the uncovered additions at the unchanged 80% floor.
        merged = self.artifacts / "merged.out"
        command = subprocess.run(
            ["go", "run", "./tools/coverage", "merge", "-output", str(merged),
             str(ordinary), str(path)],
            cwd=SCRIPT.parent.parent, capture_output=True, text=True, timeout=120,
        )
        self.assertEqual(0, command.returncode, command.stdout + command.stderr)
        result = self.analyze(merged.read_text(), head=head)
        self.assertEqual({}, result["missing"])
        self.assertEqual((9, 51), (result["covered"], result["statements"]))
        status, output = self.rendered(result)
        self.assertEqual(1, status)
        self.assertIn("9/51 (17.65%)", output)
        self.assertNotIn("incomplete coverage evidence", output)

    def test_missing_function_fails_even_when_observed_coverage_is_ninety_percent(self):
        self.write("changed.go", "package fixture\nfunc Covered() {\n" + "".join(
            f" println({number})\n" for number in range(9)
        ) + "}\nfunc Omitted() { println(42) }\n")
        self.cover_function("Covered")
        actual = self.actual_profile()
        omitted = MODULE + "/changed.go:13."
        self.assertIn(omitted, actual)
        partial = "\n".join(line for line in actual.splitlines() if not line.startswith(omitted)) + "\n"
        result = self.analyze(partial)
        self.assertEqual((9, 10), (result["covered"], result["statements"]))
        self.assertEqual({"changed.go"}, set(result["missing"]))
        status, output = self.rendered(result)
        self.assertEqual(1, status)
        self.assertIn("incomplete coverage evidence", output)

    def test_partial_file_profile_cannot_omit_an_unchanged_function(self):
        original = ("package fixture\nfunc Covered() { println(1) }\n"
                    "func Omitted() { println(2) }\n")
        self.write("changed.go", original)
        self.cover_function("Covered")
        self.base = self.commit()
        self.write("changed.go", original.replace("println(1)", "println(10)"))
        actual = self.actual_profile()
        omitted = MODULE + "/changed.go:3."
        self.assertIn(omitted, actual)
        partial = "\n".join(line for line in actual.splitlines() if not line.startswith(omitted)) + "\n"
        result = self.analyze(partial)
        self.assertEqual((1, 1), (result["covered"], result["statements"]))
        self.assertEqual({"changed.go"}, set(result["missing"]))
        self.assertEqual(3, result["missing"]["changed.go"][0][0])
        status, output = self.rendered(result)
        self.assertEqual(1, status)
        self.assertIn("incomplete coverage evidence", output)

    def test_more_than_300_files_do_not_hide_an_uncovered_go_file(self):
        for number in range(305):
            self.write(f"docs/{number:03}.txt", "changed documentation\n")
        self.write("zzz/changed.go", "package fixture\nfunc Changed() { println(1) }\n")
        result = self.analyze(profile("zzz/changed.go:2.1,2.30 1 0"))
        self.assertEqual(306, result["changed_files"])
        self.assertEqual([("zzz/changed.go", 0, 1)], result["rows"])
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
        self.assertEqual([("changed.go", 0, 2)], result["rows"])
        self.assertEqual(1, self.rendered(result)[0])

    def test_deletion_only_and_declarations_only_have_explicit_empty_result(self):
        self.write("unchanged.go", "package fixture\n")
        self.write("constants.go", "package fixture\nconst Answer = 42\n")
        self.write("empty.go", "package fixture\nfunc Empty() {}\n")
        result = self.analyze(profile("some_other_file.go:2.1,2.30 1 1"))
        self.assertEqual(["constants.go", "empty.go"], result["declarations"])
        self.assertEqual({}, result["missing"])
        self.assertEqual(2, result["eligible_files"])
        status, output = self.rendered(result)
        self.assertEqual(0, status)
        self.assertIn("N/A", output)
        self.assertIn("declarations or empty bodies", output)

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
