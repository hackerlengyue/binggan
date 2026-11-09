import { Channel, isMyGo } from "mygo-runtime";
import { CaptureService } from "@/mygo";
import { withDeadline } from "@/lib/deadline";

export type QueueRoutingSession = {
  readonly owner: string;
  assertConnected(): void;
  release(): Promise<void>;
};

// One native channel owns the whole queue. Page destruction cancels its native
// context too, so forwarding does not depend on JavaScript finally executing.
export async function retainQueueRouting(): Promise<QueueRoutingSession> {
  if (!isMyGo())
    return { owner: "", assertConnected() {}, release: async () => {} };
  const source = new Channel<string>();
  let accept!: () => void;
  let reject!: (error: Error) => void;
  const ready = new Promise<void>((resolve, fail) => {
    accept = resolve;
    reject = fail;
  });
  let owner = "";
  let closing = false;
  let failure: Error | undefined;
  const asError = (value: unknown) =>
    value instanceof Error ? value : new Error(String(value));
  source.onmessage = (value) => {
    if (typeof value === "string" && value.length > 0) {
      owner = value;
      accept();
    } else reject(new Error("队列分流连接未确认就绪"));
  };
  // Observe rejection immediately, including before the ready message arrives.
  const done = CaptureService.routingSession(source).then(
    () => ({ error: undefined }),
    (error: unknown) => ({ error: asError(error) }),
  );
  void done.then((result) => {
    if (!closing) {
      failure = result.error ?? new Error("队列分流连接已中断");
      reject(failure);
    }
  });
  let released: Promise<void> | undefined;
  const release = () => {
    if (!released) {
      closing = true;
      source.close();
      released = withDeadline(done, 12000).then(({ error }) => {
        if (error) throw error;
      });
    }
    return released;
  };
  try {
    await withDeadline(ready, 12000);
    if (failure) throw failure;
    return {
      owner,
      assertConnected() {
        if (failure) throw failure;
      },
      release,
    };
  } catch (error) {
    await release().catch(() => {});
    throw error;
  }
}
