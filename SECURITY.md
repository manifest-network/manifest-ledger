# Security

> **🚨 IMPORTANT 🚨** — if you find a security issue, report it privately to <security@manifest.network>. **Do not** open a public GitHub issue.

## Reporting

Send a report to [security@manifest.network](mailto:security@manifest.network). We aim to respond within 72 hours.

If you need to send encrypted material, request our current GPG public key by email first — we issue keys per-disclosure rather than publishing a long-lived key here, so the contents and the channel match each report.

## Packages in scope

- [x/manifest](/x/manifest)
- [x/sku](/x/sku)
- [x/billing](/x/billing)
- [pkg/uuid](/pkg/uuid)
- [app](/app)

## Threat model and supporting surfaces

Reports also cover transaction and query entry points, CLI input handling,
application wiring, genesis import/export, migrations, and simulation/integration
fixtures that protect production behavior. Assume permissionless funding,
adversarial transaction inputs, malformed remote requests, and concurrent public
queries. Review CPU, memory, gas, pagination, vesting-account size, and gateway
concurrency limits alongside authorization and consensus determinism.

The release boundary includes GitHub rulesets and environments, workflow token
permissions, dependencies and vulnerability policy, build tools, protobuf
compatibility, binary/container provenance and SBOMs, container privileges, and
operational scripts. A compromised write-capable collaborator must not gain
release authority through an unprotected tag or automatically created environment.
See [the release controls and verification procedure](docs/RELEASE.md).

The project currently has one maintainer: release approval by that maintainer is
an explicit authorization gate, not independent review. Repository administrators
remain trusted to change the controls. Production snapshot rehearsals, query and
invariant capacity measurements, RPC rate/concurrency controls, key handling, and
consumer upgrade validation remain part of deployment readiness; green CI alone
does not establish those properties. Report gaps in these controls through the
same private disclosure channel.
