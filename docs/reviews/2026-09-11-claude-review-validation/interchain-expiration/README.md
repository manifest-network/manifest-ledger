# Fresh-image pending-expiration validation

The repository Dockerfile built successfully from the current worktree's
captured inputs, including the review fixes, under the unique local tag
`manifest:review-pr179-sep11-9d25368`. The image ID is
`sha256:ef27053903b57024add946164a57512d4560dfc1f583346c477b9a30da31fcd4`.
The [build log](docker-build.log) records dependency verification, successful
compilation, static linkage and the expected version assertion. Runtime
[version metadata](docker-version.log) and [image metadata](docker-image.json)
are included.

The race-enabled command in [provenance.json](provenance.json) selected
`TestBillingState/PendingLeaseExpiration` with a 15-minute test timeout.
The [test log](pending-expiration.log) records exit 1 during faucet-container
startup, before chain initialization or any expiration assertions. Docker
could not install a published-port DNAT rule:

```text
iptables v1.8.13 (nf_tables): RULE_APPEND failed (No such file or directory): rule in chain DOCKER
```

The accompanying daemon error also reports unsupported DNAT revision 0. This
is an environment blocker, not a passed or failed expiration assertion. The
test framework's earlier attempt to pull the local image was nonfatal; it
proceeded with the locally built image and failed while configuring networking.
No host firewall or network setting was changed to work around the failure.

[Cleanup checks](cleanup.json) confirm that the exact failed container and
test network no longer exist. The user's preexisting `manifest:local` image
retained ID `sha256:2e1835724b286e76b48dd6caa495ea23e6b11b271a9c4f0182abe41894b6aed5`.
The separate review image remains available locally.

The manifest records the actual build/test/version argv, cwd, explicit test
environment overrides, exit statuses and artifact hashes. Build and test
commands otherwise inherited the normal host environment; no credentials are
archived. An initial attempt used `--progress=plain`, unsupported by this host's
legacy Docker builder; its [CLI error](docker-initial-cli-error.log) is retained.
Removing that display-only flag produced the successful build.

Display logs expand tabs at 8-column stops, normalize line endings, remove
trailing whitespace and EOF blank lines, and end with one LF. Exact captured
bytes are preserved as base64 for the [test](pending-expiration-raw.json),
[build](docker-build-raw.json), [version output](docker-version-raw.json) and
[initial CLI error](docker-initial-cli-error-raw.json). The manifest has separate
hashes for raw bytes, JSON archives and display files.

The [compressed Docker context](docker-context.tar.gz) contains the exact
293-file snapshot consumed by the build, with each member verified against
the source hash map. The two later source changes were documentation only,
recorded in `snapshot_drift_after_build`; no Go source, dependency file or build
script changed.
The [test inputs](interchain-inputs.tar.gz) include the test module and workspace
files. The [changed test bytes](billing-state-test.go.txt) match the hash checked
immediately before execution. The [overlay](image-tag-overlay.json) replaces
only the test fixture's image-tag constant using the included
[setup bytes](setup-image-tag.go.txt); the tracked fixture configuration stays
unchanged. Absolute paths in the recorded overlay must be rebased to the
chosen extraction directory when reproducing elsewhere.

Nested-module compilation passed separately. The updated assertions still
require execution on a host whose Docker published-port networking works.
