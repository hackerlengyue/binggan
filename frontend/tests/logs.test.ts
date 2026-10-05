import test from "node:test";
import assert from "node:assert/strict";
import {
  formatLogTimes,
  parseLogChunk,
  logSearchContext,
  parseSystemLogs,
  logLineLevel,
} from "../src/lib/logs";
test("explicit log severity prevents successful checks and progress from appearing as errors", () => {
  assert.equal(
    logLineLevel("[2026-09-23 16:00:00] [INFO] 高频异常 0 秒"),
    "info",
  );
  assert.equal(
    logLineLevel("[2026-09-23 16:00:00] [ERROR] 输入文件不存在"),
    "error",
  );
  assert.equal(
    logLineLevel("[2026-09-23 16:00:00] [WARN] 编码器回退"),
    "warning",
  );
  assert.equal(logLineLevel('  "error_count": 0,'), "info");
  assert.equal(logLineLevel("Traceback (most recent call last):"), "error");
});
test("log chunks reject nonadvancing pages, gaps and malformed bodies", () => {
  const data = Buffer.from("中文").toString("base64");
  assert.equal(parseLogChunk({ data, offset: 6, hasMore: false }, 0).offset, 6);
  for (const value of [
    null,
    { data: "", offset: 0, hasMore: true },
    { data, offset: 7, hasMore: false },
    { data, offset: 3, hasMore: false },
  ])
    assert.throws(() => parseLogChunk(value, 0));
});
test("log search preserves matching context and unfiltered full text", () => {
  const text = Array.from({ length: 30 }, (_, i) =>
    i === 10 ? "ERROR failure" : `line ${i}`,
  ).join("\n");
  assert.equal(logSearchContext(text, "").text, text);
  const found = logSearchContext(text, "error");
  assert.equal(found.matches, 1);
  assert.ok(found.text.includes("11  ERROR failure"));
  assert.ok(found.text.includes("8  line 7"));
  assert.ok(found.text.includes("14  line 13"));
  assert.equal(logSearchContext(text, "not found").matches, 0);
});
test("system log parser validates full record shape", () => {
  assert.throws(() => parseSystemLogs({ items: [null], total: 1 }));
  assert.throws(() => parseSystemLogs({ items: [], total: -1 }));
  assert.deepEqual(parseSystemLogs({ items: [], total: 0 }), {
    items: [],
    total: 0,
  });
});

test("log timestamps display local time while preserving surrounding content", () => {
  const original = "[2026-09-23T04:47:47.006Z] [INFO] finished";
  const d = new Date("2026-09-23T04:47:47.006Z");
  const pad = (n: number) => String(n).padStart(2, "0");
  assert.equal(
    formatLogTimes(original),
    `[${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}] [INFO] finished`,
  );
  assert.equal(formatLogTimes("ordinary log"), "ordinary log");
  assert.equal(
    formatLogTimes("2026-09-23T99:47:47.006Z"),
    "2026-09-23T99:47:47.006Z",
  );
});
