import test from "node:test";
import assert from "node:assert/strict";
import { keyFileError, makeImportedKey } from "../src/lib/key-import";
import { readKeyHistory } from "../src/lib/key-history";
import type { KeyJsonPayload } from "../src/types/trace";

const payload: KeyJsonPayload = {
  passwords: {
    "2": "ABCDEF1234567890ABCDEF1234567890",
    "7": "ABCDEF1234567890ABCDEF1234567890",
  },
  getPwdData: {
    密码: "encrypted-password",
    uit: "import-user",
    den: 1200,
    pmn: 20,
    pattern: "3",
    whenLong: 1200,
    audio: 2,
    softwareName: "MAC_sz_import",
    appMd5: "a".repeat(32),
  },
};

test("import preserves exported keys, segment indexes, repeated values and player parameters", () => {
  const entry = makeImportedKey({
    name: "课程一.JSON",
    raw: "\uFEFF" + JSON.stringify(payload),
  });
  assert.equal(entry.name, "课程一");
  assert.equal(entry.source, "upload");
  assert.equal(entry.valid, true);
  assert.equal(entry.traces, undefined);
  assert.deepEqual(entry.keyJson, payload);
  assert.deepEqual(
    readKeyHistory(JSON.stringify({ version: 1, items: [entry] }))[0],
    entry,
  );
});

test("older exported files can omit whenLong without losing other parameters", () => {
  const { whenLong, ...data } = payload.getPwdData;
  const entry = makeImportedKey({
    name: "课程.json",
    raw: JSON.stringify({ ...payload, getPwdData: data }),
  });
  assert.deepEqual(entry.keyJson, payload);
});

test("malformed or incomplete keys never become usable history records", () => {
  const invalid = [
    "{",
    "null",
    "[]",
    JSON.stringify({ passwords: payload.passwords }),
    JSON.stringify({ ...payload, passwords: [] }),
    JSON.stringify({ ...payload, passwords: {} }),
    JSON.stringify({ ...payload, passwords: { "0": "a".repeat(32) } }),
    JSON.stringify({ ...payload, passwords: { "1": "invalid-key" } }),
    JSON.stringify({
      ...payload,
      getPwdData: { ...payload.getPwdData, uit: "" },
    }),
    JSON.stringify({
      ...payload,
      getPwdData: { ...payload.getPwdData, den: 1.5 },
    }),
    JSON.stringify({
      ...payload,
      getPwdData: { ...payload.getPwdData, pattern: "2" },
    }),
    JSON.stringify({
      ...payload,
      getPwdData: { ...payload.getPwdData, appMd5: "invalid" },
    }),
    JSON.stringify({
      ...payload,
      passwords: Object.fromEntries(
        Array.from({ length: 10001 }, (_, i) => [
          String(i + 1),
          "a".repeat(32),
        ]),
      ),
    }),
  ];
  for (const raw of invalid)
    assert.throws(() => makeImportedKey({ name: "invalid.json", raw }));
});

test("file selection rejects unsupported, empty and oversized files", () => {
  assert.equal(keyFileError({ name: "key.JSON", size: 1024 }), "");
  for (const file of [
    { name: "key.txt", size: 1024 },
    { name: "key.json", size: 0 },
    { name: "key.json", size: 1024 * 1024 + 1 },
  ])
    assert.ok(keyFileError(file));
});
