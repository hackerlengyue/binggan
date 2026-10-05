import test from "node:test";
import assert from "node:assert/strict";
import { streamURL } from "../src/lib/stream-url";

test("file URLs stay relative in browser preview and use MyGo Protocol in the app", () => {
  const before = Object.getOwnPropertyDescriptor(globalThis, "mygo");
  try {
    Reflect.deleteProperty(globalThis, "mygo");
    assert.equal(
      streamURL("/api/resources/a/stream"),
      "/api/resources/a/stream",
    );
    Object.defineProperty(globalThis, "mygo", {
      configurable: true,
      value: {},
    });
    assert.equal(
      streamURL("/api/resources/a/stream?start=1"),
      "binggan-stream://localhost/api/resources/a/stream?start=1",
    );
    assert.throws(() => streamURL("https://example.com/"), /无效/u);
  } finally {
    if (before) Object.defineProperty(globalThis, "mygo", before);
    else Reflect.deleteProperty(globalThis, "mygo");
  }
});
