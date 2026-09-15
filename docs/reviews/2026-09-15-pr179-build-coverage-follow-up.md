# PR #179: build graph and coverage follow-up

Review: [Claude's audit of c50a2fc](https://github.com/manifest-network/manifest-ledger/pull/179#issuecomment-5687860074).
Confidence below describes reproduction and disposition of each finding, not a
claim that all defects have been excluded. This follow-up changes development,
test, and coverage tooling; the shipping dependency versions, SDK pin, and
billing/SKU runtime source are unchanged.

| Finding | Severity / confidence | Verified disposition |
| --- | --- | --- |
| Go 1.27 coverage ranges rejected | Medium / 100% | A real Go 1.27.1 profile contains a zero-width range with positive `NumStmt`; the previous merger rejects it. Both parsers now accept this valid metadata. Empty ranges cannot satisfy executable-source evidence or conceal overlapping nonempty ranges. Go 1.26.8 and 1.27.1 regressions pass. The review's high rating is narrowed to development-tool reliability: pinned CI uses 1.26.8, and no chain/runtime vulnerability was established. |
| Rename gives unchanged code new coverage weight | Medium / 100% | A covered file renamed alongside uncovered additions could make the old gate pass. Rename detection now runs over the complete Git comparison with an explicit 50% similarity threshold and no rename limit. Matched blobs are compared directly; pure renames receive no credit and modified renames count only changed statement lines. Moving excluded source into the measured scope counts as an addition. Real Go fixtures and hostile Git-configuration/literal-path probes pass. |
| Root workspace differs from shipped dependency graph | Medium / 100% | The previous workspace changes six linked modules, including Pebble and the gogo/protobuf replacement, relative to `GOWORK=off`. Root `go.work` now contains only the root module, and root Make/CI commands explicitly disable workspace overrides. `interchaintest/go.work` preserves the existing host client's graph. A real daemon dependency regression compares versions and replacements against the shipped graph. No dependency upgrade or demonstrated behavioral vulnerability is attributed to this change. |
| Profile filtering and diff eligibility use separate exclusions | Low / 100% | Both now use the same Python policy and `.coverageignore` pattern semantics, including module-qualified paths. The diff reads the policy at the compared head. A regression passes the actual filtered profile through the gate and proves an excluded file cannot cause a later missing-evidence failure. |
| Boolean counts presented as frequency counts | Low / 100% | Merged profiles now declare `mode: set`. The HTML legend represents covered/uncovered source honestly; normalized counts do not claim execution frequency. |
| Temporary profiles and binary merge counters retained after success | Low / 100% | Successful collection removes all temporary input profiles and binary counter directories. The two combined profiles and HTML remain. Failed runs retain diagnostic evidence, cleared at the start of the next collection. An orchestration fixture verifies the final artifact set and repeat-run cleanup. This does not claim to resolve unrelated host disk usage. |
| `testdata/` fixture source cannot meet ordinary package coverage | Informational / 100% | The shared policy explicitly excludes fixture directories omitted by Go's ordinary `./...` package expansion. The release documentation explains this scope and where implementation/tests belong. Build-tag/platform omissions still fail when eligible executable source lacks evidence. |
| Host/container coverage compiler versions can diverge | Informational / 100% | An early preflight requires the active host version to match the digest-pinned Docker builder. It runs before image construction in CI and before simulations locally. CI reads its version from `go.mod`. Mismatch and future matching-version fixtures pass; the documented local command selects the pinned compiler and rebuilds the image. |
| Raw combined profile promised but not uploaded | Informational / 100% | CI now uploads both unfiltered and filtered combined profiles alongside summaries, including when a later floor fails. |
| Previous claim of 21 Python checks | Refuted / 100% | The previous CI command discovers all three `test_*.py` files: 15 diff, 2 summary, and 4 release-control tests, totaling 21. Running only `test_coverage_diff.py` explains the reviewer's count of 15. This follow-up adds four diff tests, bringing discovery to 25. |

Coverage collection now runs root unit tests separately from the integration
client. Root tests, simulations, and the daemon use the shipping graph. The
client's instrumentation is restricted to its own nested module; it cannot
contribute counters for root code compiled with different dependencies. A real
client compilation with test bodies skipped produces 644 blocks across eight
client files and no root-module blocks. Container daemon counters remain part
of the combined profile. HTML/function rendering uses the nested workspace only
to locate source files from both modules.

Go 1.27 repeats some basic-block `NumStmt` values across split source ranges.
The same small fixture changes standard Go coverage from 4/6 to 7/9, while the
logical AST metric remains 4/5. The statement-count change is confirmed; it does
not inherently change the AST denominator. Preserve the toolchain's metadata
and re-measure coverage when advancing the pin. Both Codecov 80% floors and all
required contexts remain intact.

Focused validation includes both Go toolchains, repeated/shuffled coverage-tool
tests (96.8% statement coverage), all 25 Python tests on both toolchains, the
daemon graph comparison, client-only instrumentation, pipeline cleanup and
preflight fixtures, real profile HTML rendering, and workflow actionlint. The
complete root Go suite passes with `GOWORK=off`; root and integration-client lint
pass using the pinned golangci-lint v2.12.2 and their respective module graphs. Prior
workspace-generated coverage is historical evidence, not a fresh validation of
the corrected shipping graph. Exact-head CI, including the complete simulation
and race-enabled container coverage run, remains the merge gate.
