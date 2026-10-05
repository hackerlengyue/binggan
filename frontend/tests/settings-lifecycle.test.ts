import test, { afterEach, beforeEach } from "node:test";
import assert from "node:assert/strict";
import { createRenderer, h, nextTick } from "vue";
import { createPinia, setActivePinia } from "pinia";
import { useCertificateManager } from "../src/composables/useCertificateManager";
import { usePlayerSettings } from "../src/composables/usePlayerSettings";
import { useEnvironmentInfo } from "../src/composables/useEnvironmentInfo";
import { useTraceStore } from "../src/stores/trace";
import type { CertificateState, Diagnosis, PlayerSettings } from "../src/mygo";
const renderer = createRenderer<object, object>({
  patchProp() {},
  insert() {},
  remove() {},
  createElement: () => ({}),
  createText: () => ({}),
  createComment: () => ({}),
  setText() {},
  setElementText() {},
  parentNode: () => null,
  nextSibling: () => null,
});
function mount<T>(use: () => T) {
  let state!: T;
  const app = renderer.createApp({
    setup() {
      state = use();
      return () => h("div");
    },
  });
  app.mount({});
  let active = true;
  const unmount = () => {
    if (active) {
      active = false;
      app.unmount();
    }
  };
  cleanups.push(unmount);
  return { state, unmount };
}
function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((a, b) => {
    resolve = a;
    reject = b;
  });
  return { promise, resolve, reject };
}
const tick = async () => {
  await new Promise((resolve) => setImmediate(resolve));
  await nextTick();
};
const certificateSample: CertificateState = {
  installed: false,
  trusted: false,
  name: "Fixture Root CA",
  fingerprint: "a".repeat(64),
  expiresAt: "2036-09-30T02:00:00Z",
};
const originalWindow = globalThis.window;
const originalMyGo = Object.getOwnPropertyDescriptor(globalThis, "mygo");
function mockMyGo(
  call: (method: string, ...args: unknown[]) => Promise<unknown>,
) {
  Object.defineProperty(globalThis, "mygo", {
    configurable: true,
    value: { call },
  });
}
const environment = {
  checkedAt: "2026-09-30T02:00:00Z",
  go: "fixture",
  checks: [],
  player: null,
  knownPlayers: {},
  environmentAppMd5Configured: false,
};
let cleanups: (() => void)[] = [];
beforeEach(() => {
  setActivePinia(createPinia());
  globalThis.localStorage = {
    getItem: () => null,
    setItem() {},
    removeItem() {},
    clear() {},
    key: () => null,
    length: 0,
  };
  cleanups = [];
});
afterEach(() => {
  cleanups.reverse().forEach((fn) => fn());
  globalThis.window = originalWindow;
  if (originalMyGo) Object.defineProperty(globalThis, "mygo", originalMyGo);
  else Reflect.deleteProperty(globalThis, "mygo");
});
test("certificate page reads certificate once without capture polling", async () => {
  const actions: string[] = [];
  mockMyGo(async (method) => {
    actions.push(method.split(".")[1]!.toLowerCase());
    return certificateSample;
  });
  const page = mount(useCertificateManager);
  await tick();
  assert.deepEqual(actions, ["certificate"]);
  assert.deepEqual(page.state.certificate.value, certificateSample);
});
test("certificate operations deduplicate clicks and refresh partial failures", async () => {
  const installation = deferred<CertificateState>();
  const actions: string[] = [];
  let reads = 0;
  mockMyGo(async (method) => {
    const action = method.split(".")[1]!.toLowerCase();
    actions.push(action);
    if (action === "install") return installation.promise;
    if (action === "trust")
      return { ...certificateSample, installed: true, trusted: true };
    return ++reads === 1
      ? certificateSample
      : { ...certificateSample, installed: true };
  });
  const page = mount(useCertificateManager);
  await tick();
  assert.equal(await page.state.act("trust"), false);
  const active = page.state.act("install");
  assert.equal(await page.state.act("install"), false);
  installation.reject(new Error("Error: native install failed"));
  assert.equal(await active, false);
  assert.deepEqual(actions, ["certificate", "install", "certificate"]);
  assert.equal(page.state.certificate.value?.installed, true);
  assert.equal(page.state.error.value, "native install failed");
  assert.equal(page.state.busy.value, "");
  assert.equal(await page.state.act("trust"), true);
  assert.equal(page.state.certificate.value?.trusted, true);
  assert.equal(page.state.error.value, "");
});
test("leaving certificates discards late native responses", async () => {
  const pending = deferred<CertificateState>();
  mockMyGo(async () => pending.promise);
  const page = mount(useCertificateManager);
  page.unmount();
  pending.resolve(certificateSample);
  await tick();
  assert.equal(page.state.certificate.value, null);
  assert.equal(page.state.error.value, "");
});
test("decryption settings load configuration and player information once", async () => {
  const methods: string[] = [];
  mockMyGo(async (method) => {
    methods.push(method);
    return method === "WorkspaceService.Diagnose"
      ? environment
      : { mode: "auto", softwareName: "", appMd5: "" };
  });
  const page = mount(usePlayerSettings);
  const store = useTraceStore();
  cleanups.push(() => store.dispose());
  await tick();
  assert.deepEqual(methods, [
    "WorkspaceService.Settings",
    "WorkspaceService.Diagnose",
  ]);
  assert.equal(page.state.loaded.value, true);
  assert.deepEqual(page.state.diagnosis.value, environment);
});
test("environment information reads diagnosis without loading or changing decryption settings", async () => {
  const methods: string[] = [];
  mockMyGo(async (method) => {
    methods.push(method);
    return environment;
  });
  const page = mount(useEnvironmentInfo);
  await page.state.refresh();
  assert.deepEqual(methods, ["WorkspaceService.Diagnose"]);
  assert.deepEqual(page.state.diagnosis.value, environment);
});
test("environment refresh deduplicates pending requests and discards results after leaving", async () => {
  const pending = deferred<Diagnosis>();
  let requests = 0;
  mockMyGo(async () => {
    requests++;
    return pending.promise;
  });
  const page = mount(useEnvironmentInfo);
  const refreshing = page.state.refresh();
  await page.state.refresh();
  assert.equal(requests, 1);
  assert.equal(page.state.testing.value, true);
  page.unmount();
  pending.resolve(environment);
  await refreshing;
  assert.equal(page.state.diagnosis.value, undefined);
  assert.equal(page.state.error.value, "");
});
test("environment refresh preserves existing information on failure and recovers on retry", async () => {
  let fail = false;
  mockMyGo(async () => {
    if (fail) throw new Error("diagnosis unavailable");
    return environment;
  });
  const page = mount(useEnvironmentInfo);
  await page.state.refresh();
  fail = true;
  await page.state.refresh();
  assert.equal(page.state.error.value, "diagnosis unavailable");
  assert.deepEqual(page.state.diagnosis.value, environment);
  fail = false;
  await page.state.refresh();
  assert.equal(page.state.error.value, "");
});
test("environment detection preserves edits made while waiting and save failure preserves the draft", async () => {
  const diagnosis = deferred<Diagnosis>();
  let detections = 0;
  mockMyGo(async (method) => {
    if (method === "WorkspaceService.Diagnose")
      return ++detections === 1 ? environment : diagnosis.promise;
    if (method === "WorkspaceService.SaveSettings")
      throw new Error("storage unavailable");
    return { mode: "auto", softwareName: "", appMd5: "" };
  });
  const page = mount(usePlayerSettings);
  const store = useTraceStore();
  cleanups.push(() => store.dispose());
  await tick();
  Object.assign(page.state.draft, {
    mode: "manual",
    softwareName: "initial",
    appMd5: "a".repeat(32),
  });
  const detection = page.state.testEnvironment(true);
  page.state.draft.softwareName = "my-edit";
  diagnosis.resolve({
    checkedAt: new Date().toISOString(),
    go: "fixture",
    checks: [],
    player: { softwareName: "detected", appMd5: "b".repeat(32) },
    knownPlayers: {},
    environmentAppMd5Configured: false,
  });
  await detection;
  assert.equal(page.state.draft.softwareName, "my-edit");
  assert.equal(await page.state.save(), false);
  assert.match(page.state.settingsError.value, /storage unavailable/);
  assert.equal(page.state.draft.softwareName, "my-edit");
  assert.equal(page.state.dirty.value, true);
});
test("leaving settings discards an in-flight load without reporting success", async () => {
  const pending = deferred<PlayerSettings>();
  mockMyGo(async (method) =>
    method === "WorkspaceService.Settings" ? pending.promise : environment,
  );
  const page = mount(usePlayerSettings);
  const store = useTraceStore();
  cleanups.push(() => store.dispose());
  page.unmount();
  pending.resolve({
    mode: "manual",
    softwareName: "late",
    appMd5: "a".repeat(32),
  });
  await tick();
  assert.equal(page.state.loaded.value, false);
  assert.equal(page.state.draft.mode, "auto");
});

test("located Windows player without verified parameters preserves manual values", async () => {
  mockMyGo(async (method) =>
    method === "WorkspaceService.Diagnose"
      ? {
          ...environment,
          checks: [
            {
              name: "播放器安装位置",
              ok: true,
              message: "D:\\SZPlayer 26.06.551\\MainPlayer.exe",
            },
            {
              name: "播放器自动识别",
              ok: false,
              message: "已找到播放器，但校验参数尚未取得",
            },
          ],
        }
      : {
          mode: "manual",
          softwareName: "my-record-version",
          appMd5: "a".repeat(32),
        },
  );
  const page = mount(usePlayerSettings);
  const store = useTraceStore();
  cleanups.push(() => store.dispose());
  await tick();
  assert.equal(page.state.playerLocated.value, true);
  await page.state.testEnvironment(true);
  assert.equal(page.state.draft.softwareName, "my-record-version");
  assert.equal(page.state.draft.appMd5, "a".repeat(32));
  assert.equal(page.state.dirty.value, false);
});

test("failed installation check does not claim a Windows player was found", async () => {
  mockMyGo(async (method) =>
    method === "WorkspaceService.Diagnose"
      ? {
          ...environment,
          checks: [{ name: "播放器安装位置", ok: false, message: "not found" }],
        }
      : { mode: "auto", softwareName: "", appMd5: "" },
  );
  const page = mount(usePlayerSettings);
  const store = useTraceStore();
  cleanups.push(() => store.dispose());
  await tick();
  assert.equal(page.state.playerLocated.value, false);
});

test("saving settings reports success only after persistence and deduplicates clicks", async () => {
  const pending = deferred<PlayerSettings>();
  let writes = 0;
  mockMyGo(async (method) => {
    if (method === "WorkspaceService.Diagnose") return environment;
    if (method === "WorkspaceService.SaveSettings") {
      writes++;
      return pending.promise;
    }
    return { mode: "auto", softwareName: "", appMd5: "" };
  });
  const page = mount(usePlayerSettings);
  const store = useTraceStore();
  cleanups.push(() => store.dispose());
  await tick();
  Object.assign(page.state.draft, {
    mode: "manual",
    softwareName: "WINDOWS_fixture",
    appMd5: "b".repeat(32),
  });
  const active = page.state.save();
  assert.equal(page.state.saving.value, true);
  assert.equal(await page.state.save(), false);
  assert.equal(writes, 1);
  pending.resolve({ ...page.state.draft });
  assert.equal(await active, true);
  assert.equal(page.state.dirty.value, false);
  assert.equal(page.state.saving.value, false);
});

test("power setting waits for persistence, deduplicates clicks and preserves state on failure", async () => {
  const { usePowerSettings } =
    await import("../src/composables/usePowerSettings");
  const pending = deferred<{ keepAwake: boolean }>();
  let writes = 0;
  mockMyGo(async (method) => {
    if (method === "WorkspaceService.PowerSettings")
      return { keepAwake: false };
    writes++;
    return pending.promise;
  });
  const page = mount(usePowerSettings);
  await tick();
  const save = page.state.save(true);
  await page.state.save(true);
  assert.equal(writes, 1);
  assert.equal(page.state.enabled.value, false);
  pending.reject(new Error("write failed"));
  await save;
  assert.equal(page.state.enabled.value, false);
  assert.equal(page.state.error.value, "write failed");
  mockMyGo(async () => ({ keepAwake: true }));
  await page.state.save(true);
  assert.equal(page.state.enabled.value, true);
  assert.equal(page.state.error.value, "");
});

test("power settings discard responses after leaving the page", async () => {
  const { usePowerSettings } =
    await import("../src/composables/usePowerSettings");
  const pending = deferred<{ keepAwake: boolean }>();
  mockMyGo(async () => pending.promise);
  const page = mount(usePowerSettings);
  page.unmount();
  pending.resolve({ keepAwake: true });
  await tick();
  assert.equal(page.state.loaded.value, false);
  assert.equal(page.state.enabled.value, false);
});
