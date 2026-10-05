import type { Capture, CaptureNotice } from "../src/mygo";

const emptyHistory = () =>
  JSON.stringify({
    version: 1,
    items: [],
    currentCapture: null,
    deletedCaptureIds: [],
  });

export class FakeChannel {
  onmessage: ((notice: CaptureNotice) => void) | null = null;
  closed = false;
  private finish: ((error?: Error) => void) | null = null;

  emit(notice: CaptureNotice) {
    if (!this.closed) this.onmessage?.(notice);
  }

  receive(capture: Capture) {
    this.emit({ kind: "message", capture });
  }

  ready(enabled = false) {
    this.emit({ kind: "ready" });
    this.emit({ kind: "capture_state", enabled });
  }

  subscribe(): Promise<void> {
    return new Promise<void>((resolve, reject) => {
      this.finish = (error) => (error ? reject(error) : resolve());
    });
  }

  fail(error = new Error("channel closed")) {
    if (this.closed) return;
    this.closed = true;
    this.finish?.(error);
  }

  close() {
    if (this.closed) return;
    this.closed = true;
    this.finish?.();
  }
}

export class FakeMyGo {
  calls: { method: string; args: unknown[] }[] = [];
  channels: FakeChannel[] = [];
  events: Capture[] = [];
  enabled = false;
  document = emptyHistory();
  handler: ((method: string, ...args: unknown[]) => Promise<unknown>) | null =
    null;
  private original = Object.getOwnPropertyDescriptor(globalThis, "mygo");

  install() {
    Object.defineProperty(globalThis, "mygo", {
      configurable: true,
      value: {
        call: (method: string, ...args: unknown[]) =>
          this.call(method, ...args),
        channel: () => {
          const source = new FakeChannel();
          this.channels.push(source);
          return source;
        },
      },
    });
    return this;
  }

  restore() {
    if (this.original) Object.defineProperty(globalThis, "mygo", this.original);
    else Reflect.deleteProperty(globalThis, "mygo");
  }

  async call(method: string, ...args: unknown[]): Promise<unknown> {
    this.calls.push({ method, args });
    if (this.handler) return this.handler(method, ...args);
    return this.defaultCall(method, ...args);
  }

  async defaultCall(method: string, ...args: unknown[]): Promise<unknown> {
    switch (method) {
      case "PlayerService.Wait":
        await new Promise((resolve) => setTimeout(resolve, Number(args[0])));
        return;
      case "CaptureService.State":
        return { stage: "ready" };
      case "CaptureService.Retry":
        return { stage: "ready" };
      case "WorkspaceService.History":
        return this.events.slice(-(Number(args[0]) || 2500));
      case "WorkspaceService.CaptureState":
        return this.enabled;
      case "WorkspaceService.SetCaptureState":
        this.enabled = args[0] === true;
        for (const source of this.channels)
          source.emit({ kind: "capture_state", enabled: this.enabled });
        return this.enabled;
      case "WorkspaceService.ClearTraces":
        this.events = [];
        for (const source of this.channels)
          source.emit({ kind: "clear", clientId: String(args[0]) });
        return;
      case "WorkspaceService.KeyHistory":
        return this.document;
      case "WorkspaceService.SaveKeyHistory":
        this.document = String(args[0]);
        return this.document;
      case "WorkspaceService.Subscribe": {
        const source = args[0] as FakeChannel;
        queueMicrotask(() => source.ready(this.enabled));
        return source.subscribe();
      }
      default:
        throw new Error(`Unexpected MyGo call: ${method}`);
    }
  }

  methods() {
    return this.calls.map((entry) => entry.method);
  }
}
