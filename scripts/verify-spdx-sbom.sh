#!/bin/sh

set -eu

if [ "$#" -ne 1 ]; then
	printf '%s\n' "usage: $0 <spdx-json>" >&2
	exit 64
fi

sbom=$1
if [ -L "$sbom" ] || [ ! -f "$sbom" ]; then
	printf '%s\n' "SPDX SBOM must be a regular file: $sbom" >&2
	exit 1
fi
if ! command -v jq >/dev/null 2>&1; then
	printf '%s\n' "required SPDX verifier command is unavailable: jq" >&2
	exit 69
fi

if ! jq -e '
  . as $document
  |
  .spdxVersion == "SPDX-2.3"
  and .dataLicense == "CC0-1.0"
  and .SPDXID == "SPDXRef-DOCUMENT"
  and (.name | type == "string" and length > 0)
  and (.documentNamespace | type == "string" and startswith("https://"))
  and (.creationInfo | type == "object")
  and (.creationInfo.created | type == "string" and length > 0)
  and (.creationInfo.creators | type == "array" and length > 0)
  and (.packages | type == "array" and length > 0)
  and (.relationships | type == "array" and length > 0)
  and any(
    .relationships[];
    .spdxElementId == "SPDXRef-DOCUMENT"
    and .relationshipType == "DESCRIBES"
    and (
      .relatedSpdxElement as $related
      | any($document.packages[]; .SPDXID == $related)
    )
  )
' "$sbom" >/dev/null; then
	printf '%s\n' "release SBOM does not satisfy the required SPDX 2.3 document profile: $sbom" >&2
	exit 1
fi
