"""Offline release-profile regressions using an actual linked Go fixture."""

import copy
import hashlib
import json
import os
import subprocess
import tarfile
import tempfile
import unittest
from pathlib import Path

from verify_spdx_sbom import ROOT_MODULE, verify_release_profile


class ReleaseProfileTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.directory = tempfile.TemporaryDirectory(prefix="manifest-spdx-test-")
        cls.addClassCleanup(cls.directory.cleanup)
        root = Path(cls.directory.name)
        (root / "cmd/manifestd").mkdir(parents=True)
        (root / "go.mod").write_text(
            f"module {ROOT_MODULE}\n\ngo 1.23\n\n"
            "require example.com/replaced-uuid v1.0.0\n"
            "replace example.com/replaced-uuid v1.0.0 => github.com/google/uuid v1.6.0\n"
        )
        # This dependency is already part of the repository's locked module
        # graph. Build offline and retain its checked-in sums; do not download
        # tools or fabricate build metadata inside the test.
        sums = (Path(__file__).resolve().parent.parent / "go.sum").read_text()
        (root / "go.sum").write_text("\n".join(
            line for line in sums.splitlines() if line.startswith("github.com/google/uuid v1.6.0")
        ) + "\n")
        (root / "cmd/manifestd/main.go").write_text(
            'package main\nimport("fmt"; uuid "example.com/replaced-uuid")\n'
            'func main(){fmt.Println(uuid.NewString())}\n'
        )
        cls.binary = root / "manifestd"
        built = subprocess.run(
            ["go", "build", "-mod=readonly", "-buildvcs=false", "-o", str(cls.binary), "./cmd/manifestd"],
            cwd=root, env=os.environ | {"GOWORK": "off", "GOPROXY": "off"},
            check=False, capture_output=True, text=True, timeout=120,
        )
        if built.returncode:
            raise RuntimeError(f"offline Go fixture build failed: {built.stdout}{built.stderr}")
        build = json.loads(subprocess.run(
            ["go", "version", "-m", "-json", str(cls.binary)],
            check=True, capture_output=True, text=True,
        ).stdout)
        cls.archive = root / "manifest-test.tar.gz"
        with tarfile.open(cls.archive, "w:gz") as archive:
            archive.add(cls.binary, arcname="manifestd")

        def package(name, version, identifier):
            return {
                "name": name, "versionInfo": version, "SPDXID": identifier,
                "downloadLocation": "NOASSERTION", "filesAnalyzed": False,
            }

        cls.document = {
            "spdxVersion": "SPDX-2.3", "dataLicense": "CC0-1.0", "SPDXID": "SPDXRef-DOCUMENT",
            "name": cls.archive.name, "documentNamespace": "https://example.com/manifest-fixture",
            "creationInfo": {"created": "2026-09-14T12:00:00Z", "creators": ["Tool: fixture"]},
            "packages": [
                package(cls.archive.name, "archive", "SPDXRef-Archive"),
                package(ROOT_MODULE, "UNKNOWN", "SPDXRef-Root"),
                package("github.com/google/uuid", "v1.6.0", "SPDXRef-Dependency"),
                package("stdlib", build["GoVersion"], "SPDXRef-Stdlib"),
            ],
            "files": [{
                "fileName": "manifestd", "SPDXID": "SPDXRef-Binary",
                "checksums": [{"algorithm": "SHA256", "checksumValue": hashlib.sha256(cls.binary.read_bytes()).hexdigest()}],
            }],
            "relationships": [{
                "spdxElementId": "SPDXRef-DOCUMENT", "relationshipType": "DESCRIBES", "relatedSpdxElement": "SPDXRef-Archive",
            }],
        }
        cls.document["packages"][0]["checksums"] = [{
            "algorithm": "SHA256", "checksumValue": hashlib.sha256(cls.archive.read_bytes()).hexdigest(),
        }]
        for identifier in ["SPDXRef-Root", "SPDXRef-Dependency", "SPDXRef-Stdlib"]:
            cls.document["relationships"].extend([
                {"spdxElementId": "SPDXRef-Archive", "relationshipType": "CONTAINS", "relatedSpdxElement": identifier},
                {
                    "spdxElementId": identifier, "relationshipType": "OTHER", "relatedSpdxElement": "SPDXRef-Binary",
                    "comment": "evident-by: indicates the package's existence is evident by the given file",
                },
            ])
            if identifier != "SPDXRef-Root":
                cls.document["relationships"].append({
                    "spdxElementId": identifier, "relationshipType": "DEPENDENCY_OF", "relatedSpdxElement": "SPDXRef-Root",
                })

    def test_real_dependency_replacement_and_subject(self):
        self.assertEqual(2, verify_release_profile(self.document, self.archive, self.binary))

    def test_incomplete_or_incorrect_profile(self):
        for name in [
            "one package", "missing dependency", "missing root", "missing stdlib", "wrong version",
            "original requirement instead of replacement", "wrong archive hash", "wrong binary hash",
            "wrong subject", "missing dependency edge", "missing root containment", "missing binary evidence",
        ]:
            with self.subTest(name=name):
                document = copy.deepcopy(self.document)
                if name == "one package":
                    document["packages"] = document["packages"][:1]
                elif name.startswith("missing ") and name in ["missing dependency", "missing root", "missing stdlib"]:
                    identifier = {"missing dependency": "SPDXRef-Dependency", "missing root": "SPDXRef-Root", "missing stdlib": "SPDXRef-Stdlib"}[name]
                    document["packages"] = [p for p in document["packages"] if p["SPDXID"] != identifier]
                elif name == "wrong version":
                    document["packages"][2]["versionInfo"] = "v0.0.1"
                elif name == "original requirement instead of replacement":
                    document["packages"][2].update(name="example.com/replaced-uuid", versionInfo="v1.0.0")
                elif name == "wrong archive hash":
                    document["packages"][0]["checksums"][0]["checksumValue"] = "0" * 64
                elif name == "wrong binary hash":
                    document["files"][0]["checksums"][0]["checksumValue"] = "0" * 64
                elif name == "wrong subject":
                    document["relationships"][0]["relatedSpdxElement"] = "SPDXRef-Root"
                elif name == "missing dependency edge":
                    document["relationships"] = [r for r in document["relationships"] if r["relationshipType"] != "DEPENDENCY_OF"]
                elif name == "missing root containment":
                    document["relationships"] = [r for r in document["relationships"] if not (r["relationshipType"] == "CONTAINS" and r["relatedSpdxElement"] == "SPDXRef-Root")]
                elif name == "missing binary evidence":
                    document["relationships"] = [r for r in document["relationships"] if r["relationshipType"] != "OTHER"]
                with self.assertRaises(ValueError):
                    verify_release_profile(document, self.archive, self.binary)

    def test_symlink_inputs_are_rejected(self):
        for original in [self.archive, self.binary]:
            link = original.with_name(original.name + "-symlink")
            link.symlink_to(original)
            with self.subTest(input=original.name), self.assertRaisesRegex(ValueError, "regular file"):
                verify_release_profile(
                    self.document, link if original == self.archive else self.archive,
                    link if original == self.binary else self.binary,
                )


if __name__ == "__main__":
    unittest.main()
