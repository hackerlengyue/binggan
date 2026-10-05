import test from "node:test";
import assert from "node:assert/strict";
import {
  journalConnection,
  flushConnectionJournal,
} from "../src/lib/connection-journal";
test("failed journal batches retry with stable IDs and oversized logs retain all text", async () => {
  const original = Object.getOwnPropertyDescriptor(globalThis, "mygo");
  const batches: { id: string; message: string }[][] = [];
  try {
    let success = false;
    Object.defineProperty(globalThis, "mygo", {
      configurable: true,
      value: {
        call: async (
          method: string,
          entries: { id: string; message: string }[],
        ) => {
          assert.equal(method, "WorkspaceService.AppendLogs");
          batches.push(entries);
          if (!success) throw new Error("storage unavailable");
        },
      },
    });
    const message = "中文错误".repeat(2500);
    journalConnection({
      at: new Date().toISOString(),
      level: "error",
      message,
    });
    await flushConnectionJournal();
    success = true;
    await flushConnectionJournal();
    assert.deepEqual(batches[0], batches[1]);
    assert.equal(
      batches[0]!
        .map((entry) => entry.message.replace(/^\[\d+\/\d+\] /, ""))
        .join(""),
      message,
    );
    await flushConnectionJournal();
    assert.equal(batches.length, 2);
  } finally {
    if (original) Object.defineProperty(globalThis, "mygo", original);
    else Reflect.deleteProperty(globalThis, "mygo");
  }
});
