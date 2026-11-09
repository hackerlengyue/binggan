import test from "node:test";
import assert from "node:assert/strict";
import { retainQueueRouting } from "../src/lib/queue-routing";
import { FakeMyGo, type FakeChannel } from "./mygo-fixture";
import type { CaptureNotice } from "../src/mygo";
const tick = () => new Promise<void>((resolve) => setImmediate(resolve));

test("queue routing waits for readiness and native cleanup confirmation", async () => {
  const native = new FakeMyGo().install();
  let source!: FakeChannel;
  let cleaned!: () => void;
  const cleanup = new Promise<void>((resolve) => {
    cleaned = resolve;
  });
  native.handler = async (method, ...args) => {
    assert.equal(method, "CaptureService.RoutingSession");
    source = args[0] as FakeChannel;
    await source.subscribe();
    await cleanup;
  };
  try {
    let ready = false;
    const pending = retainQueueRouting().then((value) => {
      ready = true;
      return value;
    });
    await tick();
    assert.equal(ready, false);
    source.emit("owner-one" as unknown as CaptureNotice);
    const session = await pending;
    assert.equal(session.owner, "owner-one");
    session.assertConnected();
    let released = false;
    const release = session.release().then(() => {
      released = true;
    });
    assert.equal(source.closed, true);
    await tick();
    assert.equal(released, false);
    cleaned();
    await release;
    await session.release();
    assert.equal(native.calls.length, 1);
  } finally {
    cleaned?.();
    native.restore();
  }
});
test("native session loss is observed before another course can start", async () => {
  const native = new FakeMyGo().install();
  try {
    const session = await retainQueueRouting();
    native.channels[0]!.fail(new Error("routing controller stopped"));
    await tick();
    assert.throws(
      () => session.assertConnected(),
      /routing controller stopped/,
    );
    await assert.rejects(session.release(), /routing controller stopped/);
  } finally {
    native.restore();
  }
});
test("native startup rejection closes its channel and does not become ready", async () => {
  const native = new FakeMyGo().install();
  native.handler = async () => {
    throw new Error("another capture owns routing");
  };
  try {
    await assert.rejects(retainQueueRouting(), /another capture owns routing/);
    assert.equal(native.channels[0]!.closed, true);
  } finally {
    native.restore();
  }
});
test("invalid queue readiness is rejected and its native session is released", async () => {
  const native = new FakeMyGo().install();
  native.handler = async (_, ...args) => {
    const source = args[0] as FakeChannel;
    queueMicrotask(() => source.emit(false as unknown as CaptureNotice));
    await source.subscribe();
  };
  try {
    await assert.rejects(retainQueueRouting(), /未确认就绪/);
    assert.equal(native.channels[0]!.closed, true);
  } finally {
    native.restore();
  }
});
