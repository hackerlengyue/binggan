import test, { beforeEach, afterEach } from "node:test";
import { memoryStorage } from "./memory-storage";
import { FakeMyGo } from "./mygo-fixture";
let mygo: FakeMyGo;
beforeEach(() => {
  globalThis.localStorage = memoryStorage();
  mygo = new FakeMyGo().install();
});
afterEach(() => mygo.restore());
import assert from "node:assert/strict";
import { buildKeyJson, isKeyRequest } from "../src/lib/keyjson";
import type { TrafficRow } from "../src/lib/traffic";
import { createPinia, setActivePinia } from "pinia";
import { useTraceStore } from "../src/stores/trace";
const first = "0123456789abcdef0123456789abcdef";
const second = "fedcba9876543210fedcba9876543210";
function trace(
  path: string,
  response: unknown,
  request: unknown = { videoId: "one" },
): TrafficRow {
  return {
    id: path,
    url: "https://learn.shenzaokeji.com/api/courseLocal/" + path,
    path,
    timestamp: "2026-09-21T10:00:00Z",
    method: "POST",
    status: 200,
    durationMs: 20,
    request: { headers: { UIT: "fixture-uit" }, body: JSON.stringify(request) },
    response: { headers: {}, body: JSON.stringify(response) },
    requestTruncated: false,
    responseTruncated: false,
    hasRequest: true,
    hasResponse: true,
  };
}
const metadata = trace("getPerinutePwd", {
  data: {
    pwdMaps: { 第一个密码: { 密码: "cipher-text" } },
    den: 1200,
    pmn: 20,
    pattern: "3",
  },
});
const password = trace(
  "getPwd3V2",
  { data: { pwd: first } },
  { videoId: "one", dts: 60 },
);

test("two real endpoint responses produce the original key.json schema", () => {
  const result = buildKeyJson([password, metadata]);
  assert.equal(result.valid, true);
  assert.deepEqual(result.keyJson, {
    passwords: { "1": first },
    getPwdData: {
      密码: "cipher-text",
      uit: "fixture-uit",
      den: 1200,
      pmn: 20,
      pattern: "3",
      whenLong: 1200,
    },
  });
});
test("duplicate responses and hex case variants do not duplicate keys", () => {
  const result = buildKeyJson([
    metadata,
    password,
    trace(
      "getPwd3V2",
      { data: { pwd: first.toUpperCase() } },
      { videoId: "one", dts: 60 },
    ),
    trace("getPwd3V2", { data: { pwd: second } }, { videoId: "one", dts: 120 }),
  ]);
  assert.deepEqual(result.keyJson.passwords, { "1": first, "2": second });
});
test("multiple passwords in one response survive a single request segment index", () => {
  for (const data of [
    [{ pwd: first }, { pwd: second }],
    [
      { index: 1, pwd: first },
      { index: 2, pwd: second },
    ],
    { passwords: { "1": { pwd: first }, "2": { pwd: second } } },
    { passwords: [first, second] },
  ]) {
    const response = trace("getPwd3V2", { data }, { videoId: "one", dts: 60 });
    const result = buildKeyJson([metadata, response, response]);
    assert.deepEqual(result.keyJson.passwords, { "1": first, "2": second });
    assert.equal(result.passwordCount, 2);
    assert.equal(result.valid, true);
  }
});
test("raw multi-password responses do not reuse the request index for every value", () => {
  const response = trace("getPwd3V2", {}, { videoId: "one", dts: 60 });
  response.response.body = `pwd='${first}'; pwd='${second}'`;
  const result = buildKeyJson([metadata, response]);
  assert.deepEqual(result.keyJson.passwords, { "1": first, "2": second });
});
test("different passwords from a repeated request index are both retained", () => {
  const result = buildKeyJson([
    metadata,
    password,
    trace("getPwd3V2", { data: { pwd: second } }, { videoId: "one", dts: 60 }),
    password,
  ]);
  assert.deepEqual(result.keyJson.passwords, { "1": first, "2": second });
});
test("unrelated URLs, non-hex values and unsuccessful responses never supply keys", () => {
  const result = buildKeyJson([
    metadata,
    trace("account", { data: { pwd: first } }),
    trace("getPwd3V2", { data: { pwd: "not-a-key" } }),
    { ...password, status: 500 },
  ]);
  assert.equal(result.passwordCount, 0);
  assert.equal(result.valid, false);
  assert.equal(
    isKeyRequest(
      "https://learn.shenzaokeji.com/unrelated?next=/api/courseLocal/getPwd3V2",
    ),
    false,
  );
});
test("ciphertext and request passwords are never counted as segment keys", () => {
  const meta = trace("getPerinutePwd", {
    data: {
      pwdMaps: { 第一个密码: { 密码: first } },
      den: 1200,
      pmn: 20,
      pattern: "3",
    },
  });
  const result = buildKeyJson([
    meta,
    trace("getPwd3V2", { data: {} }, { videoId: "one", password: second }),
  ]);
  assert.equal(result.keyJson.getPwdData.密码, first);
  assert.equal(result.passwordCount, 0);
});
test("truncated key content blocks export with a concrete issue", () => {
  const result = buildKeyJson([
    metadata,
    { ...password, responseTruncated: true },
  ]);
  assert.equal(result.valid, false);
  assert.ok(result.issues.some((v) => v.includes("截断")));
});
test("mixed explicit videos cannot export a merged key file", () => {
  const result = buildKeyJson([
    metadata,
    password,
    trace("getPwd3V2", { data: { pwd: second } }, { videoId: "two" }),
  ]);
  assert.equal(result.valid, false);
  assert.ok(result.issues.some((v) => v.includes("多个 videoId")));
});
test("missing uit blocks export, pending responses add no keys", () => {
  const result = buildKeyJson([
    { ...metadata, request: { ...metadata.request, headers: {} } },
    { ...password, status: 0, hasResponse: false },
  ]);
  assert.equal(result.valid, false);
  assert.ok(result.issues.some((v) => v.includes("uit")));
  assert.equal(result.passwordCount, 0);
});
test("selecting requests keeps all captured passwords, and empty output cannot download", () => {
  setActivePinia(createPinia());
  const store = useTraceStore();
  assert.throws(() => store.downloadKey(), /尚不完整/);
  store.traces = [metadata, password];
  const payload = store.keyJsonText;
  store.selectedId = password.id;
  assert.equal(store.total, 2);
  assert.equal(store.selected?.id, password.id);
  assert.equal(store.keyJsonText, payload);
  assert.equal(store.keyResult.valid, true);
});
test("clearing capture records clears the extracted result", async () => {
  setActivePinia(createPinia());
  const store = useTraceStore();
  store.traces = [metadata, password];
  await store.clear();
  assert.equal(store.keyResult.passwordCount, 0);
  assert.equal(store.keyResult.valid, false);
});
test("stopping synchronizes final responses and archives extracted keys", async () => {
  setActivePinia(createPinia());
  const store = useTraceStore();
  store.captureEnabled = true;
  const events = [metadata, password].flatMap((t, i) =>
    ["request", "response"].map((phase) => ({
      id: i + phase,
      url: t.url,
      ts: t.timestamp,
      method: t.method,
      phase,
      host: "learn.shenzaokeji.com",
      status: phase === "response" ? 200 : undefined,
      headers: t[phase as "request" | "response"].headers,
      body: t[phase as "request" | "response"].body,
      bodyTruncated: false,
      source: "sing-box",
    })),
  );
  mygo.events = events as typeof mygo.events;
  const saved = await store.stopCapture();
  assert.equal(store.captureEnabled, false);
  assert.equal(saved?.valid, true);
  assert.equal(Object.keys(saved!.keyJson.passwords).length, 1);
  assert.equal(store.keyResult.passwordCount, 0);
  assert.equal(store.total, 0);
});

test("perinute endpoint does not incorrectly force pattern 1 before getPwd3V2", () => {
  const meta = trace("getPerinutePwd", {
    data: { pwdMaps: { 第一个密码: { 密码: "cipher" } }, den: 1200, pmn: 20 },
  });
  const result = buildKeyJson([meta, password]);
  assert.equal(result.keyJson.getPwdData.pattern, "3");
  assert.equal(result.valid, true);
});
test("a history call started before clear cannot restore deleted keys", async () => {
  setActivePinia(createPinia());
  const store = useTraceStore();
  store.traces = [metadata, password];
  let finish!: (events: typeof mygo.events) => void;
  mygo.handler = async (method, ...args) =>
    method === "WorkspaceService.History"
      ? new Promise<typeof mygo.events>((resolve) => {
          finish = resolve;
        })
      : mygo.defaultCall(method, ...args);
  const pending = store.refresh();
  await store.clear();
  finish([
    {
      id: "stale",
      phase: "response",
      method: "POST",
      url: password.url,
      host: "learn.shenzaokeji.com",
      ts: password.timestamp,
      status: 200,
      headers: {},
      body: password.response.body,
      bodyTruncated: false,
      source: "sing-box",
    },
  ]);
  await pending;
  assert.equal(store.total, 0);
  assert.equal(store.keyResult.passwordCount, 0);
});

test("audio restoration interval is preserved, including zero", () => {
  for (const audio of [0, 385]) {
    const meta = trace("getPerinutePwd", {
      data: {
        pwdMaps: { 第一个密码: { 密码: "cipher-text" } },
        den: 1200,
        pmn: 20,
        pattern: "3",
        audio,
      },
    });
    assert.equal(
      buildKeyJson([password, meta]).keyJson.getPwdData.audio,
      audio,
    );
  }
});

test("player metadata survives JSON, query, header and form capture for automatic configuration", () => {
  const json = trace(
    "getPwd3V2",
    { data: { pwd: first } },
    { videoId: "one", softwareName: "MAC_sz_test", appMd5: "A".repeat(32) },
  );
  let data = buildKeyJson([json]).keyJson.getPwdData;
  assert.equal(data.softwareName, "MAC_sz_test");
  assert.equal(data.appMd5, "a".repeat(32));
  const form = trace("getPwd3V2", { data: { pwd: first } });
  form.request.body = "software_name=MAC_sz_form&app_md5=" + "b".repeat(32);
  data = buildKeyJson([form]).keyJson.getPwdData;
  assert.equal(data.softwareName, "MAC_sz_form");
  assert.equal(data.appMd5, "b".repeat(32));
  const headers = trace("getPwd3V2", { data: { pwd: first } });
  headers.request.headers = {
    SoftwareName: "MAC_sz_header",
    appMd5: "c".repeat(32),
  };
  data = buildKeyJson([headers]).keyJson.getPwdData;
  assert.equal(data.softwareName, "MAC_sz_header");
  assert.equal(data.appMd5, "c".repeat(32));
  const query = trace(
    "getPwd3V2?softwareName=MAC_sz_query&appMd5=" + "d".repeat(32),
    {
      data: { pwd: first },
    },
  );
  data = buildKeyJson([query]).keyJson.getPwdData;
  assert.equal(data.softwareName, "MAC_sz_query");
  assert.equal(data.appMd5, "d".repeat(32));
});

test("mixed player versions or checksums cannot export a combined key", () => {
  const one = trace(
    "getPwd3V2",
    { pwd: first },
    { softwareName: "MAC_sz_one", appMd5: "a".repeat(32) },
  );
  const two = trace(
    "getPwd3V2",
    { pwd: second },
    { softwareName: "MAC_sz_two", appMd5: "b".repeat(32) },
  );
  const result = buildKeyJson([metadata, one, two]);
  assert.equal(result.valid, false);
  assert.ok(result.issues.some((issue) => issue.includes("不同播放器参数")));
});
