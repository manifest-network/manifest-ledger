# Contributing to Manifest Ledger

All types of contributions are encouraged and valued. See the [Table of Contents](#table-of-contents) for different ways to help and details about how this project handles them. Please make sure to read the relevant section before making your contribution.

## Table of Contents

- [I Have a Question](#i-have-a-question)
- [I Want To Contribute](#i-want-to-contribute)
- [I Want To Open A Pull Request](#i-want-to-open-a-pull-request)
- [Reporting Bugs](#reporting-bugs)

## I Have a Question

> If you want to ask a question, we assume that you have read the available [Documentation](./README.md).

Before you ask a question, it is best to search for existing [Issues](https://github.com/manifest-network/manifest-ledger/issues) that might help you. In case you have found a suitable issue and still need clarification, you can write your question in this issue. It is also advisable to search the internet for answers first.

If you then still feel the need to ask a question and need clarification, we recommend the following:

- Open an [Issue](https://github.com/manifest-network/manifest-ledger/issues/new).
- Select a template and stick to its guidelines.
- Provide as much context as you can about what you're running into.
- Provide project and platform versions (os, arch, go, etc.), depending on what seems relevant.

## I Want To Contribute

### Legal Notice

When contributing to this project, you must agree that you have authored 100% of the content, that you have the necessary rights to the content and that the content you contribute may be provided under the project license.

### Reporting Bugs

#### Before Submitting a Bug Report

Please complete the following steps in advance to help us fix any potential bug as fast as possible.

- Make sure that you are using the latest version.
- Determine if your bug is really a bug and not an error on your side e.g. using incompatible environment components/versions (Make sure that you have read the [documentation](./README.md). If you are looking for support, you might want to check [this section](#i-have-a-question)).
- To see if other users have experienced (and potentially already solved) the same issue you are having, check if there is not already a bug report existing for your bug or issue.
- Collect information about the bug:
- Stack trace (Traceback)
- OS, Platform and Version (Windows, Linux, macOS, x86, ARM)
- Version of the interpreter, compiler, SDK, runtime environment, package manager, depending on what seems relevant.
- Can you reliably reproduce the issue? And can you also reproduce it with older versions?

#### How Do I Submit a Good Bug Report?

> You must never report security related issues, vulnerabilities or bugs including sensitive information to the issue tracker, or elsewhere in public. Instead sensitive bugs must be sent by email to <security@manifest.network>. See [SECURITY.md](SECURITY.md) for further details.

We use GitHub issues to track bugs and errors. If you run into an issue with the project:

- Open an [Issue](https://github.com/manifest-network/manifest-ledger/issues/new).
- Explain the behavior you would expect and the actual behavior.
- Please provide as much context as possible and describe the _reproduction steps_ that someone else can follow to recreate the issue on their own. This usually includes your code. For good bug reports you should isolate the problem and create a reduced test case.
- Provide the information you collected in the previous section.

## I Want To Open A Pull Request

We use the [GitHub Flow](https://guides.github.com/introduction/flow/index.html) for our development process: fork the repository, create a branch for your changes, and open a pull request against `main` when you're ready.

PRs created without filling in the PR template will be ignored and closed. Please follow the template as best as you can, removing any irrelevant sections and filling in the rest to the best of your ability.

Follow the project conventions in [`CLAUDE.md`](./CLAUDE.md). CI runs lint, module
tidy checks, native Linux/Darwin builds, unit tests, simulations, the E2E matrix,
ARM64 Docker compilation, `make proto-check`, `make govulncheck`, CodeQL, the
GoReleaser snapshot build and coverage. See [`.github/workflows/`](./.github/workflows/)
for the canonical commands. The snapshot and release jobs both use Ubuntu 22.04;
changing that runner can raise the glibc version required by shipped binaries.
Coverage requires both instrumented Docker images and a matching official host Go
release; follow the [coverage instructions](README.md#coverage).

### Vulnerability policy

`GOTOOLCHAIN=go1.25.14 make govulncheck` installs scanner v1.7.0 under `build/` and
runs the same script as CI. It scans `./cmd/manifestd` with release tags
`netgo,ledger`, including hardware-wallet dependencies. Keep the tags aligned with
`.goreleaser.yaml`. The raw JSON report is retained at
`build/vulnerability/govulncheck.json`. Module/package findings remain visible;
the gate fails on reachable symbol findings unless a reviewed exception matches.
This scan covers Go dependencies, not native C/Rust libraries or container packages.

`scripts/govulncheck-exceptions.json` maps an advisory ID to these fields:

| Field | Meaning |
| --- | --- |
| `module`, `version` | Exact affected module path and selected version. |
| `advisory_modified` | Exact advisory revision timestamp that was reviewed. |
| `expires` | First rejected UTC date, in `YYYY-MM-DD` format. |
| `reason` | Why this exact finding is excepted, including follow-up tracking. |
| `evidence` | Source supporting the exception. |

An exception also requires the advisory to remain `UNREVIEWED` and to report no
fixed version. Any version/revision change, new reachable finding, or expiry
fails the gate. Re-review evidence before changing an exception; extending a date
alone does not establish safety. Run `python3 scripts/test-vulnerability-report.py`
after editing the policy or its parser. The current MessagePack exception is
tracked in ENG-867 and expires on 2026-10-15.
