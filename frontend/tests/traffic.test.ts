import test from "node:test";
import assert from "node:assert/strict";
import { mergeEvent } from "../src/lib/traffic";
import type { CaptureEvent } from "../src/types/trace";
const request: CaptureEvent = {
  id: "request",
  phase: "request",
  ts: "2026-09-21T10:00:01Z",
  method: "GET",
  url: "https://example.shenzaokeji.com/api/check",
  host: "example.shenzaokeji.com",
  headers: {},
  body: "",
  bodyTruncated: false,
  source: "sing-box",
};
test("empty request bodies remain separate requests, and a response merges with one request only", () => {
  let rows = mergeEvent([], request);
  rows = mergeEvent(rows, {
    ...request,
    id: "request-2",
    ts: "2026-09-21T10:00:02Z",
  });
  assert.equal(rows.length, 2);
  rows = mergeEvent(rows, {
    ...request,
    id: "response",
    phase: "response",
    status: 200,
    ts: "2026-09-21T10:00:03Z",
    headers: { "x-duration-ms": "60" },
  });
  assert.equal(rows.length, 2);
  assert.equal(rows.filter((r) => r.hasResponse).length, 1);
  assert.equal(rows[0]?.durationMs, 60);
});

test("all valid requests remain visible, including ordinary demo URLs", () => {
  const rows = mergeEvent([], {
    ...request,
    url: "https://example.test/demo/status",
  });
  assert.equal(rows.length, 1);
  assert.equal(rows[0]?.path, "/demo/status");
});

test("sing-box pairs concurrent requests with the same URL by requestId", () => {
  let rows = mergeEvent([], {
    ...request,
    id: "a-request",
    requestId: "a",
    source: "sing-box",
  });
  rows = mergeEvent(rows, {
    ...request,
    id: "b-request",
    requestId: "b",
    source: "sing-box",
  });
  rows = mergeEvent(rows, {
    ...request,
    id: "a-response",
    requestId: "a",
    source: "sing-box",
    phase: "response",
    body: "a-result",
    status: 200,
  });
  assert.equal(rows.length, 2);
  assert.equal(
    rows.find((r) => r.requestId === "a")?.response.body,
    "a-result",
  );
  assert.equal(rows.find((r) => r.requestId === "b")?.hasResponse, false);
  rows = mergeEvent(rows, {
    ...request,
    id: "capture-response",
    phase: "response",
    body: "capture-result",
  });
  assert.equal(rows.length, 3);
});

test("missing duration headers use paired timestamps in either arrival order", () => {
  const response = {
    ...request,
    id: "response",
    phase: "response" as const,
    status: 200,
    ts: "2026-09-21T10:00:01.309Z",
  };
  for (const events of [
    [request, response],
    [response, request],
  ]) {
    const rows = events.reduce(
      (rows, event) => mergeEvent(rows, event),
      [] as ReturnType<typeof mergeEvent>,
    );
    assert.equal(rows.length, 1);
    assert.equal(rows[0]?.durationMs, 309);
    assert.equal(rows[0]?.timestamp, request.ts);
  }
  assert.equal(mergeEvent([], response)[0]?.durationMs, 0);
});

test("response duration headers are case insensitive and override timestamp estimates", () => {
  const rows = mergeEvent(mergeEvent([], request), {
    ...request,
    id: "response",
    phase: "response",
    ts: "2026-09-21T10:00:02Z",
    headers: { "X-Duration-Ms": "42" },
  });
  assert.equal(rows[0]?.durationMs, 42);
});
