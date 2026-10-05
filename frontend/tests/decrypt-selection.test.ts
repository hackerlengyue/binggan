import test from "node:test";
import assert from "node:assert/strict";
import { matchingKeys, videoFileError } from "../src/lib/decrypt-selection";
test("matches filenames with harmless spacing differences without confusing lesson numbers", () => {
  const keys = [
    { id: "a", name: "Lesson 1.1 业务介绍.json" },
    { id: "b", name: "Lesson 11 业务介绍.json" },
  ];
  assert.deepEqual(
    matchingKeys("课程/Lesson 1.1 业务介绍 .sz", keys).map((k) => k.id),
    ["a"],
  );
  assert.equal(matchingKeys("different.sz", keys).length, 0);
  assert.equal(
    matchingKeys("Lesson 1.1 业务介绍.sz", [...keys, { ...keys[0], id: "c" }])
      .length,
    2,
  );
});
test("file selection rejects wrong format and empty files before upload", () => {
  assert.ok(videoFileError({ name: "video.mp4", size: 2 }));
  assert.ok(videoFileError({ name: "video.sz", size: 0 }));
  assert.equal(videoFileError({ name: "video.SZ", size: 2 }), "");
});
