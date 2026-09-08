import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, renameSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { delimiter, join } from "node:path";
import { test } from "node:test";
import { runInNewContext } from "node:vm";

function example(path, marker, language) {
  const doc = readFileSync(new URL(path, import.meta.url), "utf8");
  const section = doc.slice(doc.indexOf(marker));
  assert.notEqual(doc.indexOf(marker), -1, `missing section: ${marker}`);
  const block = section.match(new RegExp("```" + language + "\\n([\\s\\S]*?)\\n```"));
  assert.ok(block, `missing ${language} example`);
  return block[1];
}

test("frontend authentication survives wallet confirmation across second boundaries", async () => {
  const code = example("../docs/FRONTEND.md", "### Tenant authentication to your provider API", "ts");
  let now = 1_700_000_000_900;
  let signedMessage;
  let request;
  await runInNewContext(`(async () => { ${code} })()`, {
    Date: { now: () => now },
    leaseUuid: "01902a9b-1234-7000-8000-000000000001",
    tenant: "manifest1tenant",
    provider: { api_url: "https://provider.example" },
    btoa: (value) => Buffer.from(value).toString("base64"),
    window: {
      keplr: {
        signArbitrary: async (_chainId, _tenant, message) => {
          signedMessage = message;
          now += 12_500; // wallet confirmation after several second boundaries
          return { pub_key: { type: "test", value: "pubkey" }, signature: "signature" };
        },
      },
    },
    fetch: async (url, options) => { request = { url, ...options }; },
  });
  const token = JSON.parse(Buffer.from(request.headers.Authorization.slice(7), "base64").toString());
  assert.equal(signedMessage, `manifest lease access ${token.lease_uuid} ${token.timestamp}`);
  assert.equal(token.timestamp, 1_700_000_000);
  assert.equal(request.url, `https://provider.example/v1/leases/${token.lease_uuid}/connection`);
});

const mockManifestd = `#!/usr/bin/env node
const fs = require("node:fs");
const path = require("node:path");
const root = process.env.DOC_FIXTURE_STATE;
const args = process.argv.slice(2);
const mode = process.env.DOC_FIXTURE_MODE || "success";
const log = path.join(root, "calls.jsonl");
fs.appendFileSync(log, JSON.stringify(args) + "\\n");
const emit = value => process.stdout.write(JSON.stringify(value));
if (args[0] === "tx") {
  const key = args[args.indexOf("--key") + 1];
  if (key !== "" && key !== "AP8B") throw new Error("wrong continuation key: " + key);
  if (args[args.indexOf("--broadcast-mode") + 1] !== "sync") throw new Error("expected sync");
  if (mode === "broadcast-error") process.exit(1);
  const submissions = fs.readFileSync(log, "utf8").trim().split("\\n").map(JSON.parse).filter(call => call[0] === "tx").length;
  const hash = (key === "" ? (submissions === 1 ? "A" : "C") : "B").repeat(64);
  emit({ code: 0, height: "0", txhash: hash, raw_log: "" });
} else if (args[1] === "tx") {
  if (mode === "timeout") process.exit(1);
  const failure = path.join(root, "failed-txhash");
  if (mode === "execution-error") fs.writeFileSync(failure, args[2]);
  const failed = fs.existsSync(failure) && fs.readFileSync(failure, "utf8") === args[2];
  emit({ code: failed ? 5 : 0, height: "42", txhash: args[2], data: "1200" });
} else if (args[1] === "billing" && args[2] === "withdraw-result") {
  if (mode === "decode-error") process.exit(1);
  const first = args[3] !== "B".repeat(64);
  emit({ total_amounts: [], payout_address: "manifest1payout", withdrawal_count: "50",
    has_more: first, next_key: first ? "AP8B" : "",
    failed_lease_uuids: [first ? "lease-first-page" : "lease-final-page"] });
} else throw new Error("unexpected command " + JSON.stringify(args));
`;

const hasShellDependencies = ["bash", "jq"].every((command) => spawnSync(command, ["--version"]).status === 0);

test("documented withdrawal workflow handles committed SDK envelopes and restart checkpoints", {
  skip: !hasShellDependencies && "Bash and jq are required by the documented recipe",
}, async (t) => {
  for (const mode of ["success", "timeout", "execution-error", "decode-error", "broadcast-error"]) {
    await t.test(mode, () => {
      const root = mkdtempSync(join(tmpdir(), "manifest-doc-examples-"));
      try {
        const script = join(root, "withdraw.sh");
        writeFileSync(script, example("../x/billing/docs/API.md", "**Resumable automation example", "bash"));
        writeFileSync(join(root, "manifestd"), mockManifestd, { mode: 0o755 });
        writeFileSync(join(root, "sleep"), "#!/bin/sh\nexit 0\n", { mode: 0o755 });
        execFileSync("bash", ["-n", script]);
        const run = (mode) => spawnSync("bash", [script], {
          cwd: root,
          encoding: "utf8",
          env: { ...process.env, PATH: root + delimiter + process.env.PATH, DOC_FIXTURE_STATE: root, DOC_FIXTURE_MODE: mode },
          timeout: 30_000,
        });
        const state = join(root, "withdraw-state-01912345-6789-7abc-8def-0123456789ab");
        const checkpoint = () => JSON.parse(readFileSync(join(state, "checkpoint.json")));
        const calls = () => readFileSync(join(root, "calls.jsonl"), "utf8").trim().split("\n").map(JSON.parse);
        const result = run(mode);
        if (mode !== "success") {
          assert.notEqual(result.status, 0, result.stdout);
          assert.equal(checkpoint().has_more, true);
          assert.equal(checkpoint().key, "");
          assert.equal(calls().filter((args) => args[0] === "tx").length, 1);
          if (mode === "broadcast-error") {
            // An incomplete submission receipt must halt, including on restart.
            assert.notEqual(run("success").status, 0);
            assert.equal(calls().filter((args) => args[0] === "tx").length, 1);
            return;
          }
          assert.equal(JSON.parse(readFileSync(join(state, "pending.json"))).txhash, "A".repeat(64));
          if (mode === "execution-error") {
            // Committed failures are immutable: restart stops until the operator
            // diagnoses the failure and archives it to authorize a fresh attempt.
            assert.notEqual(run("success").status, 0);
            assert.equal(calls().filter((args) => args[0] === "tx").length, 1);
            renameSync(join(state, "pending.json"), join(state, "failed-admission.json"));
            renameSync(join(state, "included.json"), join(state, "failed-execution.json"));
          }
          const resumed = run("success");
          assert.equal(resumed.status, 0, resumed.stderr);
        } else {
          assert.equal(result.status, 0, result.stderr);
        }
        assert.equal(checkpoint().has_more, false);
        assert.equal(checkpoint().key, "");
        assert.deepEqual(checkpoint().failed_lease_uuids, ["lease-final-page", "lease-first-page"]);
        const expectedSubmissions = mode === "execution-error" ? 3 : 2;
        assert.equal(calls().filter((args) => args[0] === "tx").length, expectedSubmissions);
        if (mode === "execution-error") {
          assert.ok(calls().some((args) => args[1] === "tx" && args[2] === "C".repeat(64)), "recovery must use a fresh transaction hash");
        }
        const rerun = run("success");
        assert.equal(rerun.status, 0, rerun.stderr);
        assert.equal(calls().filter((args) => args[0] === "tx").length, expectedSubmissions, "completed checkpoints must not resubmit");
      } finally {
        rmSync(root, { recursive: true, force: true });
      }
    });
  }
});
