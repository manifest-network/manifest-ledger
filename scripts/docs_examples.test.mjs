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

const mockOperationalManifestd = `#!/usr/bin/env node
const fs = require("node:fs");
const path = require("node:path");
const root = process.env.DOC_FIXTURE_STATE;
const args = process.argv.slice(2);
const mode = process.env.DOC_FIXTURE_MODE || "success";
const log = path.join(root, "calls.jsonl");
fs.appendFileSync(log, JSON.stringify(args) + "\\n");
const emit = value => process.stdout.write(JSON.stringify(value));
const statePath = path.join(root, "chain.json");
const state = fs.existsSync(statePath) ? JSON.parse(fs.readFileSync(statePath)) : { txs: {}, active: 151 };
const save = () => fs.writeFileSync(statePath, JSON.stringify(state));
if (args[0] === "tx") {
  if (args[args.indexOf("--broadcast-mode") + 1] !== "sync") throw new Error("expected sync");
  const stage = ({ "fund-credit": "fund", "create-lease-for-tenant": "create", "acknowledge-lease": "ack", "deactivate-provider": "deactivate" })[args[2]];
  if (!stage) throw new Error("unexpected command");
  if (stage === "ack" && !/^[0-9a-f-]{36}$/.test(args[3])) throw new Error("missing lease UUID");
  if (mode === stage + "-broadcast-error") process.exit(1);
  const sequence = Object.keys(state.txs).length + 1;
  const hash = sequence.toString(16).padStart(64, "0").toUpperCase();
  state.txs[hash] = { stage, sequence, queries: 0, failed: mode === stage + "-execution-error", committed: false };
  save();
  emit({ code: mode === stage + "-broadcast-rejection" ? 7 : 0, height: "0", txhash: hash });
} else if (args[1] === "tx") {
  const hash = args[2];
  const tx = state.txs[hash];
  if (!tx) throw new Error("unknown transaction");
  tx.queries++;
  save();
  if (mode === "timeout" || (mode === "delayed" && tx.queries < 3)) process.exit(1);
  if (!tx.committed) {
    tx.committed = true;
    if (!tx.failed && tx.stage === "deactivate") state.active = Math.max(0, state.active - 50);
    tx.active = state.active;
    save();
  }
  const events = tx.stage === "create" ? [{ type: "lease_created", attributes: [
    { key: "lease_uuid", value: "01902a9b-1234-7000-8000-" + tx.sequence.toString().padStart(12, "0") },
  ] }] : [];
  if (mode === "missing-event") events.length = 0;
  if (mode === "ambiguous-event" && tx.stage === "create") events.push(events[0]);
  emit({ code: tx.failed ? 17 : 0, height: String(42 + tx.sequence), txhash: hash, events, logs: [] });
} else if (args[1] === "sku" && args[2] === "skus-by-provider") {
  if (!args.includes("--active-only") || args[args.indexOf("--limit") + 1] !== "1") throw new Error("expected bounded active query");
  const height = Number(args[args.indexOf("--height") + 1]);
  const tx = Object.values(state.txs).find(tx => tx.sequence + 42 === height && tx.committed);
  if (!tx) throw new Error("query must use the committed height");
  if (mode === "query-error") process.exit(1);
  emit({ skus: tx.active ? [{ uuid: "remaining-active-sku" }] : [], pagination: {} });
} else throw new Error("unexpected command " + JSON.stringify(args));
`;

function operationalFixture(document, marker, check) {
  const root = mkdtempSync(join(tmpdir(), "manifest-operational-docs-"));
  try {
    const script = join(root, "workflow.sh");
    writeFileSync(script, example(document, marker, "bash"));
    writeFileSync(join(root, "manifestd"), mockOperationalManifestd, { mode: 0o755 });
    writeFileSync(join(root, "sleep"), "#!/bin/sh\nexit 0\n", { mode: 0o755 });
    execFileSync("bash", ["-n", script]);
    const run = (mode) => spawnSync("bash", [script], {
      cwd: root,
      encoding: "utf8",
      env: { ...process.env, PATH: root + delimiter + process.env.PATH, DOC_FIXTURE_STATE: root, DOC_FIXTURE_MODE: mode },
      timeout: 30_000,
    });
    const calls = () => readFileSync(join(root, "calls.jsonl"), "utf8").trim().split("\n").map(JSON.parse);
    check({ root, run, calls });
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
}

test("migration recipe confirms every stage and resumes without duplicate funding or leases", {
  skip: !hasShellDependencies && "Bash and jq are required by the documented recipe",
}, async (t) => {
  for (const mode of ["success", "delayed", "timeout", "missing-event", "ambiguous-event",
    ...["fund", "create", "ack"].flatMap(stage => ["broadcast-error", "broadcast-rejection", "execution-error"].map(failure => `${stage}-${failure}`))]) {
    await t.test(mode, () => operationalFixture("../x/billing/docs/MIGRATION.md", "### Batch Migration Script Example", ({ run, calls }) => {
      const result = run(mode);
      const submissions = () => calls().filter(args => args[0] === "tx");
      if (["success", "delayed"].includes(mode)) {
        assert.equal(result.status, 0, result.stderr);
        assert.match(result.stdout, /Migration complete!/);
        assert.equal(submissions().length, 9);
        if (mode === "delayed") assert.equal(calls().filter(args => args[1] === "tx").length, 27);
        assert.equal(run("success").status, 0, "completed stages must be reusable");
        assert.equal(submissions().length, 9, "restart must not double-fund or create duplicate leases");
      } else {
        assert.notEqual(result.status, 0, result.stdout);
        assert.doesNotMatch(result.stdout, /Migration complete!/);
        const expected = mode.startsWith("ack-") ? 3 : mode.startsWith("create-") || mode.endsWith("event") ? 2 : 1;
        assert.equal(submissions().length, expected, "failure must stop subsequent stages and tenants");
        if (mode === "timeout") {
          assert.equal(run("success").status, 0);
          assert.equal(submissions().length, 9, "resume must look up the original funding transaction");
        } else {
          assert.notEqual(run("success").status, 0, "immutable failed or ambiguous receipts need operator resolution");
          assert.equal(submissions().length, expected, "a failed receipt must never trigger automatic resubmission");
        }
      }
    }));
  }
});

test("deactivation recipe completes 151 SKUs using committed receipts and bounded state queries", {
  skip: !hasShellDependencies && "Bash and jq are required by the documented recipe",
}, async (t) => {
  for (const mode of ["success", "delayed", "timeout", "deactivate-execution-error", "deactivate-broadcast-rejection", "deactivate-broadcast-error", "query-error"]) {
    await t.test(mode, () => operationalFixture("../x/sku/docs/API.md", "##### Complete a provider deactivation cascade", ({ run, calls }) => {
      const result = run(mode);
      const submissions = () => calls().filter(args => args[0] === "tx");
      if (["success", "delayed"].includes(mode)) {
        assert.equal(result.status, 0, result.stderr);
        assert.equal(submissions().length, 4);
        assert.match(result.stdout, /no active SKUs remain/);
        assert.equal(run("success").status, 0);
        assert.equal(submissions().length, 4, "completed cascade must not be sent again on restart");
      } else {
        assert.notEqual(result.status, 0, result.stdout);
        assert.doesNotMatch(result.stdout, /deactivation complete/);
        assert.equal(submissions().length, 1);
        if (["timeout", "query-error"].includes(mode)) {
          assert.equal(run("success").status, 0);
          assert.equal(submissions().length, 4, "resume must resolve the previous transaction before continuing");
        } else {
          assert.notEqual(run("success").status, 0);
          assert.equal(submissions().length, 1);
        }
      }
    }));
  }
});

test("UUID extraction recipes use committed SDK events with empty legacy logs", {
  skip: !hasShellDependencies && "jq is required by the documented recipe",
}, () => {
  for (const [module, entity] of [["sku", "provider"], ["billing", "lease"]]) {
    const code = example(`../x/${module}/docs/API.md`, "### Querying Events", "bash");
    const expression = code.match(/jq -er '([^']+)'/)[1];
    const uuid = "01902a9b-1234-7000-8000-000000000001";
    const input = JSON.stringify({ code: 0, height: "42", logs: [], events: [
      { type: `${entity}_created`, attributes: [{ key: `${entity}_uuid`, value: uuid }, { key: "msg_index", value: "0" }] },
    ] });
    assert.equal(execFileSync("jq", ["-er", expression], { input, encoding: "utf8" }).trim(), uuid);
    for (const invalid of [{ ...JSON.parse(input), code: 17 }, { ...JSON.parse(input), height: "0" }]) {
      const rejected = spawnSync("jq", ["-er", expression], { input: JSON.stringify(invalid), encoding: "utf8" });
      assert.notEqual(rejected.status, 0);
      assert.equal(rejected.stdout, "", "failed or uncommitted transactions must not yield a UUID");
    }
  }
});

test("domain lookup documentation uses the registered billing command", () => {
  const doc = readFileSync(new URL("../x/billing/docs/API.md", import.meta.url), "utf8");
  const commands = readFileSync(new URL("../x/billing/client/cli/query.go", import.meta.url), "utf8");
  assert.doesNotMatch(doc, /lease-by-custom-domain/);
  assert.match(commands, /Use:\s+"lease-by-domain \[domain\]"/);
  assert.match(doc, /use `lease-by-domain`/);
});
