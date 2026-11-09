import test from "node:test";
import assert from "node:assert/strict";
import { buildKeyJson } from "../src/lib/keyjson";
import {
  makeHistoryEntry,
  upsertKeyHistory,
  readKeyHistory,
} from "../src/lib/key-history";
import { makeImportedKey } from "../src/lib/key-import";
import type { TrafficRow } from "../src/lib/traffic";

const key = "0123456789abcdef0123456789abcdef";
const other = "fedcba9876543210fedcba9876543210";
function row(
  path: string,
  body: unknown,
  request: unknown = {},
  uit = "request-one",
): TrafficRow {
  return {
    id: path + JSON.stringify(request) + uit,
    url: "https://video.shenzaokeji.com/api/courseLocal/" + path,
    path,
    timestamp: "2026-11-09T00:00:00Z",
    method: "POST",
    status: 200,
    durationMs: 1,
    request: {
      headers: { softwareName: "MAC_sz_26.10.56", uit },
      body: JSON.stringify(request),
    },
    response: { headers: {}, body: JSON.stringify(body) },
    requestTruncated: false,
    responseTruncated: false,
    hasRequest: true,
    hasResponse: true,
  };
}
function meta(
  pattern: unknown = "3",
  uit = "request-one",
  cipher = "cipher-one",
) {
  return row(
    "getPerinutePwd",
    {
      data: {
        pwdMaps: { 第一个密码: { 密码: cipher } },
        pattern,
        whenLong: 10,
        pmn: 2,
      },
    },
    {},
    uit,
  );
}
function slot(index: number, pwd = key) {
  return row(
    "getPwd3V2",
    { data: { pwd } },
    { den: 10, dts: (index - 1) * 20, num: 1 },
  );
}

test("26.10.56 preserves actual slots, repeated values and out-of-order responses", () => {
  const result = buildKeyJson([slot(7), meta(), slot(2), slot(1, other)]);
  assert.equal(result.valid, true, result.issues.join(", "));
  assert.equal(result.keyJson.getPwdData.pmn, 2);
  assert.deepEqual(result.keyJson.passwords, {
    "1": other,
    "2": key,
    "7": key,
  });
});

test("26.10.56 fixed mode uses pmn slot and failed API keys are rejected", () => {
  const fixed = buildKeyJson([meta("1"), slot(2)]);
  assert.equal(fixed.valid, true, fixed.issues.join(", "));
  assert.deepEqual(fixed.keyJson.passwords, { "2": key });
  const failed = buildKeyJson([
    meta(),
    row(
      "getPwd3V2",
      { data: { type: "1", pwd: key } },
      { den: 10, dts: 0, num: 1 },
    ),
  ]);
  assert.equal(failed.valid, false);
  assert.equal(failed.passwordCount, 0);
});

test("26.10.56 refreshes UIT per request and keeps ciphertext paired with its request", () => {
  const result = buildKeyJson([
    meta(),
    slot(1),
    meta("3", "request-two", "cipher-two"),
    slot(2),
  ]);
  assert.equal(result.valid, true, result.issues.join(", "));
  assert.equal(result.keyJson.getPwdData.uit, "request-two");
  assert.equal(result.keyJson.getPwdData.密码, "cipher-two");
  const incomplete = meta("3", "request-three", "");
  const paired = buildKeyJson([meta(), slot(1), incomplete]);
  assert.equal(paired.keyJson.getPwdData.uit, "request-one");
  assert.equal(paired.keyJson.getPwdData.密码, "cipher-one");
});

test("26.10.56 slot identities survive archive updates and reload", () => {
  const first = [meta(), slot(7)];
  const saved = makeHistoryEntry(buildKeyJson(first), first)!;
  const nextRows = [meta(), slot(2)];
  const incoming = makeHistoryEntry(buildKeyJson(nextRows), nextRows)!;
  incoming.id = saved.id;
  const merged = upsertKeyHistory([saved], incoming)[0]!;
  assert.deepEqual(merged.keyJson.passwords, { "2": key, "7": key });
  assert.equal(merged.valid, true);
  assert.deepEqual(
    readKeyHistory(JSON.stringify({ version: 1, items: [merged] }))[0]!.keyJson,
    merged.keyJson,
  );
});

test("26.10.56 null mode needs no slot; literal null and missing mode fail", () => {
  const result = buildKeyJson([meta(null)]);
  assert.equal(result.valid, true, result.issues.join(", "));
  assert.equal(result.keyJson.getPwdData.pattern, "<null>");
  const entry = makeImportedKey({
    name: "null-mode.json",
    raw: JSON.stringify(result.keyJson),
  });
  assert.equal(entry.valid, true);
  for (const value of ["null", "", undefined]) {
    const invalid = meta(value);
    if (value === undefined) {
      const body = JSON.parse(invalid.response.body);
      delete body.data.pattern;
      invalid.response.body = JSON.stringify(body);
    }
    assert.equal(buildKeyJson([invalid, slot(1)]).valid, false);
  }
});

test("26.10.56 rejects placeholder slots and cannot infer pmn from num", () => {
  assert.equal(
    buildKeyJson([meta(), slot(1, "4dbe89a69865bf2adb092e567a1b5b56")]).valid,
    false,
  );
  const missing = row("getPerinutePwd", {
    data: {
      pwdMaps: { 第一个密码: { 密码: "cipher" } },
      pattern: "3",
      whenLong: 10,
    },
  });
  assert.equal(buildKeyJson([missing, slot(1)]).valid, false);
});

test("26.10.56 direct RC4 imports require the explicit key and no AES metadata", () => {
  const file = {
    passwords: {},
    getPwdData: {
      softwareName: "MAC_sz_26.10.56",
      pattern: "4",
      mimatuokeqian: "direct-rc4-key",
    },
  };
  const entry = makeImportedKey({
    name: "direct.json",
    raw: JSON.stringify(file),
  });
  assert.equal(entry.valid, true);
  assert.equal(entry.keyJson.getPwdData.mimatuokeqian, "direct-rc4-key");
  delete (file.getPwdData as Partial<typeof file.getPwdData>).mimatuokeqian;
  assert.throws(
    () => makeImportedKey({ name: "invalid.json", raw: JSON.stringify(file) }),
    /不完整/,
  );
});

test("26.10.56 ambiguous slots and mixed files remain invalid after history reload", () => {
  for (const rows of [
    [meta(), slot(1), slot(1, other)],
    [meta(), row("getPwd3V2", { data: { pwd: key } }, { den: 10, num: 1 })],
    [
      meta(),
      row("getPwd3V2", { data: { pwd: key } }, { den: 10, dts: 1, num: 1 }),
    ],
  ]) {
    const result = buildKeyJson(rows);
    assert.equal(result.valid, false);
    const entry = makeHistoryEntry(result, rows)!;
    assert.equal(
      readKeyHistory(JSON.stringify({ version: 1, items: [entry] }))[0]!.valid,
      false,
    );
  }
  const one = meta();
  one.request.body = JSON.stringify({
    encryptionMd5: "file-one",
    userId: "same-user",
  });
  const two = slot(1);
  two.request.body = JSON.stringify({
    den: 10,
    dts: 0,
    num: 1,
    encryptionMd5: "file-two",
  });
  assert.equal(buildKeyJson([one, two]).valid, false);
});

test("26.10.56 conflicting incremental updates never become usable history", () => {
  const rows = [meta(), slot(1)];
  const old = makeHistoryEntry(buildKeyJson(rows), rows)!;
  const nextRows = [meta(), slot(1, other)];
  const incoming = makeHistoryEntry(buildKeyJson(nextRows), nextRows)!;
  incoming.id = old.id;
  const merged = upsertKeyHistory([old], incoming)[0]!;
  assert.equal(merged.valid, false);
  assert.equal(
    readKeyHistory(JSON.stringify({ version: 1, items: [merged] }))[0]!.valid,
    false,
  );
});

test("26.10.56 old compacted archives recover real slots only from covering traffic", () => {
  const rows = [
    meta(),
    slot(7),
    meta("3", "request-two", "cipher-two"),
    slot(2, other),
  ];
  const entry = makeHistoryEntry(buildKeyJson(rows), rows)!;
  entry.keyJson.passwords = { "1": key, "2": other };
  entry.issues = ["检测到不同 uit，请清空后重新采集"];
  entry.valid = false;
  const restored = readKeyHistory(
    JSON.stringify({ version: 1, items: [entry] }),
  )[0]!;
  assert.deepEqual(restored.keyJson.passwords, { "2": other, "7": key });
  assert.equal(restored.valid, true);
  entry.traces = [meta(), slot(7)];
  const partial = readKeyHistory(
    JSON.stringify({ version: 1, items: [entry] }),
  )[0]!;
  assert.deepEqual(partial.keyJson.passwords, { "1": key, "2": other });
  assert.equal(partial.valid, false);
  entry.keyJson.passwords = { "2": key, "7": key };
  entry.traces = [meta(), slot(2)];
  const repeated = readKeyHistory(
    JSON.stringify({ version: 1, items: [entry] }),
  )[0]!;
  assert.deepEqual(repeated.keyJson.passwords, { "2": key, "7": key });
});
