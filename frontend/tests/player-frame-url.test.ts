import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";

const frame = readFileSync(
  new URL("../public/player/frame.js", import.meta.url),
  "utf8",
);

for (const scenario of [
  { name: "Windows frame runtime", own: "win32", parent: undefined },
  { name: "Windows parent runtime", own: undefined, parent: "win32" },
  { name: "macOS frame runtime", own: "darwin", parent: undefined },
  { name: "Linux parent runtime", own: undefined, parent: "linux" },
  { name: "browser preview", own: undefined, parent: undefined },
]) {
  test(`player media and poster URLs work with ${scenario.name}`, () => {
    const video = { src: "", poster: "", addEventListener() {} };
    const elements = { video, error: {}, loading: {} };
    let enhanced = false;
    const window = {
      mygo: scenario.own ? { platform: scenario.own } : undefined,
      parent: {
        mygo: scenario.parent ? { platform: scenario.parent } : undefined,
      },
      AppleTVLikePlayer: {
        enhance(target: unknown) {
          assert.equal(target, video);
          enhanced = true;
        },
      },
      addEventListener() {},
    };
    runInNewContext(frame, {
      window,
      document: {
        documentElement: {},
        getElementById: (id: keyof typeof elements) => elements[id],
        querySelector: () => null,
        addEventListener() {},
      },
      location: {
        search: "?id=video-1&v=revision-2",
        origin: "http://mygo.localhost",
      },
      URLSearchParams,
    });
    const platform = scenario.own || scenario.parent;
    const origin = platform
      ? platform === "win32"
        ? "http://binggan-stream.localhost"
        : "binggan-stream://localhost"
      : "";
    assert.equal(video.src, origin + "/api/resources/video-1/stream");
    assert.equal(
      video.poster,
      origin + "/api/resources/video-1/thumbnail?v=revision-2",
    );
    assert.equal(enhanced, true);
  });
}
