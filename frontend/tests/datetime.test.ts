import test from "node:test";
import assert from "node:assert/strict";
import { formatDateTime } from "../src/lib/datetime";

test("timestamps use local calendar fields, padding and second precision", () => {
  assert.equal(
    formatDateTime(new Date(2026, 0, 2, 3, 4, 5, 999)),
    "2026-01-02 03:04:05",
  );
  assert.equal(formatDateTime("invalid"), "暂无");
  assert.equal(formatDateTime(null), "暂无");
  assert.equal(formatDateTime(""), "暂无");
  assert.notEqual(formatDateTime(0), "暂无");
});
