import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";
import { runInNewContext } from "node:vm";

const doc = readFileSync(new URL("../x/billing/docs/AUTHENTICATION_V2.md", import.meta.url), "utf8");
const code = doc.match(/```js\n([\s\S]*?)\n```/)[1];
const { canonicalAudience, providerAuthMessage, validateProviderAuthContext } = runInNewContext(
  `${code}\n({ canonicalAudience, providerAuthMessage, validateProviderAuthContext })`, { URL },
);
const token = {
  version: 2, chain_id: "manifest-1",
  provider_uuid: "01902a9b-1234-7000-8000-000000000002",
  audience: "https://provider.example/api", operation: "lease.connection.read",
  lease_uuid: "01902a9b-1234-7000-8000-000000000001", tenant: "manifest1tenant",
  issued_at: 1_700_000_000, expires_at: 1_700_000_300, meta_hash: "",
};

test("provider auth v2 matches the fixed canonical message vector", () => {
  const vector = doc.match(/```json\n([^\n]+)\n```/)[1];
  assert.equal(providerAuthMessage(token), vector);
  assert.equal(validateProviderAuthContext(token, token, token.issued_at), vector);
  assert.equal(providerAuthMessage(Object.fromEntries(Object.entries(token).reverse())), vector);
  assert.equal(providerAuthMessage({ ...token, audience: "https://provider.example/api/a&b" }),
    vector.replace("https://provider.example/api", "https://provider.example/api/a&b"));
});

test("provider auth v2 rejects replay into another trusted context", () => {
  for (const [field, value] of Object.entries({
    chain_id: "manifest-clone", provider_uuid: "01902a9b-1234-7000-8000-000000000003",
    audience: "https://provider.example/other-api", operation: "lease.data.upload",
    lease_uuid: "01902a9b-1234-7000-8000-000000000004", tenant: "manifest1other",
    meta_hash: "a".repeat(64),
  })) {
    assert.throws(() => validateProviderAuthContext(token, { ...token, [field]: value }, token.issued_at), /mismatch/, field);
  }
});

test("provider auth v2 pins expiration, future skew, hash and version rules", () => {
  assert.doesNotThrow(() => validateProviderAuthContext(token, token, token.expires_at - 1));
  assert.doesNotThrow(() => validateProviderAuthContext(token, token, token.issued_at - 30));
  for (const now of [token.expires_at, token.issued_at - 31, NaN, -1, "1700000000"]) {
    assert.throws(() => validateProviderAuthContext(token, token, now), /not current/);
  }
  for (const patch of [
    { version: 1 }, { version: undefined }, { issued_at: "1700000000" },
    { expires_at: token.issued_at }, { expires_at: token.expires_at + 1 },
    { expires_at: Number.MAX_SAFE_INTEGER + 1 }, { chain_id: "manifest\n1" },
    { meta_hash: "a".repeat(64) }, { operation: "other" },
    { operation: "lease.data.upload", meta_hash: "AA".repeat(32) },
    { operation: "lease.data.upload", meta_hash: "a".repeat(63) },
  ]) assert.throws(() => providerAuthMessage({ ...token, ...patch }));
  const upload = { ...token, operation: "lease.data.upload", meta_hash: "ab".repeat(32) };
  assert.equal(validateProviderAuthContext(upload, upload, token.issued_at), providerAuthMessage(upload));
  assert.throws(() => validateProviderAuthContext(upload, { ...upload, meta_hash: "cd".repeat(32) }, token.issued_at));
});

test("provider auth v2 includes a canonical HTTPS API base path", () => {
  assert.equal(canonicalAudience("https://PROVIDER.example:443/api///"), token.audience);
  assert.equal(canonicalAudience("https://provider.example/"), "https://provider.example");
  for (const audience of [
    "http://provider.example/api", "https://user@provider.example/api",
    "https://provider.example/api?q=1", "https://provider.example/api#fragment",
    "https://provider.example/api/", "https://PROVIDER.example/api", null,
  ]) assert.throws(() => providerAuthMessage({ ...token, audience }));
});
