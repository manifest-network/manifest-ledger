# Complete patch reconstruction transcripts

The corrected [bundled transcript](../2026-09-10-claude-follow-up-review-validation/mutation-provenance/patch-reconstruction-checks.json)
and [terminal transcript](../2026-09-11-claude-review-validation/terminal-mutations/verification.json)
record every required step for each modification patch:

1. Initialize a fresh scratch repository.
2. Seed the source path from the archived baseline and verify its hash before
   and after writing it.
3. Verify the patch hash.
4. Apply the patch using `git apply --unidiff-zero`.
5. Verify the reconstructed source hash against the recorded mutant.

The [shared verifier](verify_patch_reconstruction.py) executes each recorded
`portable_argv` after binding only `{python}`, `{verifier}`, `{evidence}` and
`{work}`. It requires the complete five-step sequence; it does not supply an
unrecorded baseline copy. Each executed step includes its resolved argv, cwd,
exit status and exact stdout/stderr bytes as base64, with byte hashes. Display
strings expand tabs at eight-column stops, normalize line endings, remove
trailing whitespace and EOF blank lines, and end in one LF; empty streams stay
empty. Display hashes are recorded separately. Integrity checks use explicit
errors and remain active under `python3 -O`.

The bundled baseline is read as an individual member from the existing
`inputs/baseline-b4d8403.tar.gz`; the compressed archive and source-member hashes
are checked. Terminal reconstruction copies the existing `fixed.go.txt`, whose
SHA-256 is `04e23cbbda86e72b4eaac68ed14e25cd8c2672b9738ce55736269b60b1fad01f`.
It does not depend on the current `msg_server.go`. The original terminal
verification's source/run hashes remain explicitly historical. The two
seed plans are archived as [bundled](bundled-patch-plan.json) and
[terminal](terminal-patch-plan.json) inputs.

Literal replay of both recorded transcripts at new paths succeeded:
[three bundled mutations](bundled-patch-literal-replay.json) and
[three terminal mutations](terminal-patch-literal-replay.json). Each seeded
baseline and reconstructed mutant matches its recorded hash. These checks
exercise patch reconstruction; they do not rerun the historical Go tests.
Both archive-member and standalone-file seeding also
[reject an incorrect baseline hash](patch-reconstruction-rejection-checks.json)
under `python3 -O` before creating the destination.

From a checkout containing these evidence directories, choose unused work and
output paths:

```bash
python3 -O docs/reviews/2026-09-11-claude-hygiene-validation/verify_patch_reconstruction.py replay \
  docs/reviews/2026-09-10-claude-follow-up-review-validation/mutation-provenance/patch-reconstruction-checks.json \
  docs/reviews/2026-09-10-claude-follow-up-review-validation/mutation-provenance \
  .review-tmp/replay-bundled-patches .review-tmp/replay-bundled-patches.json

python3 -O docs/reviews/2026-09-11-claude-hygiene-validation/verify_patch_reconstruction.py replay \
  docs/reviews/2026-09-11-claude-review-validation/terminal-mutations/verification.json \
  docs/reviews/2026-09-11-claude-review-validation/terminal-mutations \
  .review-tmp/replay-terminal-patches .review-tmp/replay-terminal-patches.json
```

Python and Git are required. Baseline input bytes come from existing archived
files; no historical Git object, Go toolchain or additional source archive is
needed. The [existing source snapshot check](terminal-archive-input-checks.json)
also identifies a historical source tree for the separate terminal Go runner
when later edits make the current checkout's input hashes differ.
