import test, { afterEach, beforeEach } from "node:test";
import assert from "node:assert/strict";
import { importVideo, IMPORT_IDLE_TIMEOUT } from "../src/lib/video-import";
import type { LocalVideo, VideoImportProgress } from "../src/mygo";

class FakeChannel {
  closed = false;
  onClose?: () => void;
  constructor(readonly onmessage?: (value: VideoImportProgress) => void) {}
  emit(value: VideoImportProgress) {
    if (!this.closed) this.onmessage?.(value);
  }
  close() {
    if (this.closed) return;
    this.closed = true;
    this.onClose?.();
  }
}

const choice: LocalVideo = {
  path: "/tmp/fixture.sz",
  name: "fixture.sz",
  size: 100,
  modifiedTime: 1,
};
const before = Object.getOwnPropertyDescriptor(globalThis, "mygo");
let channels: FakeChannel[];
let calls: { method: string; args: unknown[] }[];
let settle: ((value: unknown) => void) | undefined;
let fail: ((error: Error) => void) | undefined;

beforeEach(() => {
  channels = [];
  calls = [];
  settle = undefined;
  fail = undefined;
  Object.defineProperty(globalThis, "mygo", {
    configurable: true,
    value: {
      channel: (onmessage?: (value: VideoImportProgress) => void) => {
        const channel = new FakeChannel(onmessage);
        channels.push(channel);
        return channel;
      },
      call: (method: string, ...args: unknown[]) => {
        calls.push({ method, args });
        return new Promise((resolve, reject) => {
          settle = resolve;
          fail = reject;
          (args[1] as FakeChannel).onClose = () =>
            reject(new Error("channel closed"));
        });
      },
    },
  });
});
afterEach(() => {
  if (before) Object.defineProperty(globalThis, "mygo", before);
  else Reflect.deleteProperty(globalThis, "mygo");
});

test("native import receives progress and accepts only a valid result", async () => {
  const seen: number[] = [];
  const result = importVideo(choice, {
    signal: new AbortController().signal,
    onProgress: (value) => seen.push(value ?? -1),
  });
  assert.equal(calls[0]?.method, "WorkspaceService.ImportVideo");
  assert.deepEqual(calls[0]?.args[0], choice);
  channels[0]!.emit({ copied: 0, total: 100 });
  channels[0]!.emit({ copied: 43, total: 100 });
  channels[0]!.emit({ copied: 100, total: 100 });
  settle?.({ id: "fresh", name: "fixture.sz" });
  assert.deepEqual(await result, { id: "fresh", name: "fixture.sz" });
  assert.deepEqual(seen, [0, 43, 100]);
  assert.equal(channels[0]!.closed, true);

  const invalid = importVideo(choice, { signal: new AbortController().signal });
  settle?.({ ok: true });
  await assert.rejects(invalid, /导入响应异常/);
});

test("cancel closes the MyGo channel, removes the listener and allows retry", async () => {
  const controller = new AbortController();
  const active = importVideo(choice, { signal: controller.signal });
  const rejected = assert.rejects(active, { name: "AbortError" });
  controller.abort();
  await rejected;
  assert.equal(channels[0]!.closed, true);
  const retry = importVideo(choice, { signal: new AbortController().signal });
  settle?.({ id: "fresh", name: "fixture.sz" });
  assert.equal((await retry).id, "fresh");

  const already = new AbortController();
  already.abort();
  await assert.rejects(importVideo(choice, { signal: already.signal }), {
    name: "AbortError",
  });
  assert.equal(calls.length, 2);
});

test("progress resets idle timeout; a stalled import closes its channel", async (context) => {
  context.mock.timers.enable({ apis: ["setTimeout"] });
  const active = importVideo(choice, { signal: new AbortController().signal });
  const rejected = assert.rejects(active, { name: "TimeoutError" });
  context.mock.timers.tick(IMPORT_IDLE_TIMEOUT - 1);
  assert.equal(channels[0]!.closed, false);
  channels[0]!.emit({ copied: 50, total: 100 });
  context.mock.timers.tick(IMPORT_IDLE_TIMEOUT - 1);
  assert.equal(channels[0]!.closed, false);
  context.mock.timers.tick(1);
  await rejected;
  assert.equal(channels[0]!.closed, true);
  assert.equal(typeof fail, "function");
});
