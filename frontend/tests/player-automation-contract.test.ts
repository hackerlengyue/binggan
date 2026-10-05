import test from "node:test";
import assert from "node:assert/strict";
import { playerAutomation } from "../src/lib/player-automation";
import { FakeMyGo } from "./mygo-fixture";

test("path clicks serialize only path identity, never legacy duplicate ordinals", async () => {
  const native = new FakeMyGo().install();
  native.handler = async () => {};
  try {
    for (const count of [1, 2]) {
      await playerAutomation.click("第一讲", 2, {
        ordinal: count - 1,
        count,
        path: ["目录", "第一讲"],
        expectedPid: 123,
      });
      assert.deepEqual(native.calls.at(-1), {
        method: "PlayerService.Click",
        args: [
          {
            text: "第一讲",
            clickCount: 2,
            path: ["目录", "第一讲"],
            expectedPid: 123,
          },
        ],
      });
    }
  } finally {
    native.restore();
  }
});
