#!/usr/bin/env python3
"""Validate SPDX 2.3 semantics and the release archive's actual Go dependencies."""

import hashlib
import json
import subprocess
import sys
from pathlib import Path


ROOT_MODULE = "github.com/manifest-network/manifest-ledger"


def regular_file(path):
    path = Path(path)
    if path.is_symlink() or not path.is_file():
        raise ValueError(f"release input must be a regular file: {path}")
    return path


def sha256(path):
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def has_sha256(element, expected):
    return any(
        checksum.get("algorithm") == "SHA256"
        and checksum.get("checksumValue") == expected
        for checksum in element.get("checksums", [])
    )


def only(elements, description):
    if len(elements) != 1:
        raise ValueError(f"expected exactly one {description}; found {len(elements)}")
    return elements[0]


def verify_release_profile(document, archive, binary):
    """Compare the pinned Syft profile with files and standard Go build info.

    Complete SPDX semantic validation runs before this function. Keeping the
    comparison independent permits offline fixture tests without replacing
    that validator or offering a production validation bypass.
    """
    archive, binary = regular_file(archive), regular_file(binary)
    archive_hash, binary_hash = sha256(archive), sha256(binary)
    result = subprocess.run(
        ["go", "version", "-m", "-json", str(binary)],
        check=True, capture_output=True, text=True,
    )
    build = json.loads(result.stdout)
    main = build.get("Main", {})
    if main.get("Path") != ROOT_MODULE or build.get("Path") != ROOT_MODULE + "/cmd/manifestd":
        raise ValueError("release binary build info does not identify manifestd's root module")
    if not build.get("Deps"):
        raise ValueError("release binary build info contains no module dependencies")

    packages = document.get("packages", [])
    subject = only([
        package for package in packages
        if package.get("name") == archive.name and has_sha256(package, archive_hash)
    ], "archive subject matching its name and SHA256")
    binary_file = only([
        file for file in document.get("files", [])
        if file.get("fileName") == "manifestd" and has_sha256(file, binary_hash)
    ], "manifestd file matching its SHA256")

    relationships = {
        (relation["spdxElementId"], relation["relationshipType"], relation["relatedSpdxElement"])
        for relation in document.get("relationships", [])
    }
    evidence = {
        (relation["spdxElementId"], relation["relatedSpdxElement"])
        for relation in document.get("relationships", [])
        if relation["relationshipType"] == "OTHER"
        and relation.get("comment", "").startswith("evident-by:")
    }
    if ("SPDXRef-DOCUMENT", "DESCRIBES", subject["SPDXID"]) not in relationships:
        raise ValueError("SPDX document does not describe the release archive")

    def module_package(name, version):
        package = only([
            package for package in packages
            if package.get("name") == name and package.get("versionInfo") == version
        ], f"module package {name}@{version}")
        package_id = package["SPDXID"]
        if (subject["SPDXID"], "CONTAINS", package_id) not in relationships:
            raise ValueError(f"release archive does not contain module {name}@{version}")
        if (package_id, binary_file["SPDXID"]) not in evidence:
            raise ValueError(f"module {name}@{version} lacks evidence from manifestd")
        return package_id

    main_version = main.get("Version")
    if not main_version or main_version == "(devel)":
        main_version = "UNKNOWN"  # Pinned Syft represents local Go builds this way.
    root_id = module_package(ROOT_MODULE, main_version)

    dependencies = {("stdlib", build["GoVersion"])}
    for dependency in build["Deps"]:
        # Compare the replacement actually linked, not the original requirement.
        # An unversioned local replacement cannot establish a release identity.
        effective = dependency.get("Replace") or dependency
        name, version = effective.get("Path"), effective.get("Version")
        if not name or not version or version == "(devel)":
            raise ValueError("release binary contains an unversioned module dependency")
        dependencies.add((name, version))
    for name, version in sorted(dependencies):
        package_id = module_package(name, version)
        if (
            (package_id, "DEPENDENCY_OF", root_id) not in relationships
            and (root_id, "DEPENDS_ON", package_id) not in relationships
        ):
            raise ValueError(f"module {name}@{version} is not a dependency of manifestd")
    return len(dependencies)


def main():
    if len(sys.argv) != 4:
        raise ValueError("usage: verify_spdx_sbom.py <spdx-json> <archive> <manifestd>")
    sbom, archive, binary = map(regular_file, sys.argv[1:])

    # SPDX's maintained validator checks required fields, ID references,
    # cardinalities, license expressions, and other SPDX 2.3 semantic rules.
    from spdx_tools.spdx.parser.parse_anything import parse_file
    from spdx_tools.spdx.validation.document_validator import validate_full_spdx_document

    parsed = parse_file(str(sbom))
    errors = validate_full_spdx_document(parsed, "SPDX-2.3")
    if errors:
        raise ValueError("SPDX 2.3 semantic validation failed: " + "; ".join(str(error) for error in errors))
    with sbom.open(encoding="utf-8") as stream:
        document = json.load(stream)
    if document.get("spdxVersion") != "SPDX-2.3":
        raise ValueError("release SBOM must use SPDX-2.3")
    count = verify_release_profile(document, archive, binary)
    print(f"verified SPDX 2.3 semantics and {count} binary dependencies (including Go stdlib)")


if __name__ == "__main__":
    try:
        main()
    except (ImportError, KeyError, TypeError, ValueError, OSError, subprocess.SubprocessError) as error:
        print(f"release SBOM validation failed: {error}", file=sys.stderr)
        sys.exit(1)
