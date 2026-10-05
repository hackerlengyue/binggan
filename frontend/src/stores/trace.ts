import { journalConnection } from "@/lib/connection-journal";
import { CaptureService, WorkspaceService, type CaptureNotice } from "@/mygo";
import { Channel, isMyGo, type Channel as MyGoChannel } from "mygo-runtime";
import { withDeadline } from "@/lib/deadline";
import { computed, ref } from "vue";
import type { ConnectionLog } from "@/lib/connection-state";
import { buildKeyJson, isKeyRequest } from "@/lib/keyjson";
import { defineStore } from "pinia";
import {
  keyFilename,
  KEY_HISTORY_STORAGE,
  readHistoryDocument,
  findHistorySource,
  makeHistoryEntry,
  upsertKeyHistory,
} from "@/lib/key-history";
import type { KeyHistoryEntry, CaptureSession } from "@/types/trace";
import type { CaptureEvent } from "@/types/trace";
import { mergeEvent, saveBlob, type TrafficRow } from "@/lib/traffic";

const MAX_EVENTS = 5000;

export const useTraceStore = defineStore("trace-v2", () => {
  const traces = ref<TrafficRow[]>([]);
  const connected = ref(false);
  const channelError = ref("");
  const channelLogs = ref<ConnectionLog[]>([]);
  function logChannel(level: "info" | "error", message: string) {
    const entry = { at: new Date().toISOString(), level, message };
    channelLogs.value.push(entry);
    if (channelLogs.value.length > 500) channelLogs.value.shift();
    journalConnection(entry);
  }
  const captureEnabled = ref(false);
  const loading = ref(false);
  const busy = ref<"start" | "stop" | "clear" | "">("");
  const error = ref("");
  const selectedId = ref("");
  let stream: MyGoChannel<CaptureNotice> | null = null;
  let reconnectTimer: ReturnType<typeof setTimeout> | undefined;
  let events: CaptureEvent[] = [];
  const eventIds = new Set<string>();
  let started = false;
  let clearVersion = 0;
  let stateRevision = 0;
  const captureClientId = crypto.randomUUID();
  const captureSession = ref<CaptureSession | null>(null);
  const captureArchived = ref(false);
  const pendingFinishId = ref("");
  const keyHistory = ref<KeyHistoryEntry[]>([]);
  const deletedCaptureIds = ref<string[]>([]);
  const currentDeleted = computed(
    () =>
      !!captureSession.value &&
      deletedCaptureIds.value.includes(captureSession.value.id),
  );
  function discardDeletedCapture() {
    if (!currentDeleted.value) return;
    pendingFinishId.value = "";
    clearVersion++;
    resetEvents();
    traces.value = [];
    selectedId.value = "";
  }
  const historyError = ref("");
  const historyPending = ref(0);
  const historyReady = ref(false);
  let durableHistory = false;
  let durableHistoryRequired = false;
  let historyRaw: string | null = null;
  let historyWrites = Promise.resolve();
  let pendingHistoryRaw: string | null = null;
  let writingHistory = false;
  let historyWriteError: unknown = null;
  function getHistoryRaw() {
    return durableHistory
      ? historyRaw
      : localStorage.getItem(KEY_HISTORY_STORAGE);
  }
  function cacheHistory(raw: string) {
    historyRaw = raw;
    try {
      localStorage.setItem(KEY_HISTORY_STORAGE, raw);
    } catch {
      /* SQLite is authoritative. */
    }
  }
  function setHistoryRaw(raw: string) {
    if (!durableHistory) {
      if (durableHistoryRequired)
        throw new Error("密钥历史尚未从数据库加载，请重试");
      localStorage.setItem(KEY_HISTORY_STORAGE, raw);
      return;
    }
    cacheHistory(raw);
    pendingHistoryRaw = raw;
    if (writingHistory) return;
    writingHistory = true;
    historyPending.value = 1;
    // Each queued document is a complete snapshot, including deletion markers.
    // Keep the in-flight write and replace only snapshots not yet sent.
    historyWrites = Promise.resolve().then(async () => {
      try {
        while (pendingHistoryRaw !== null) {
          const snapshot = pendingHistoryRaw;
          pendingHistoryRaw = null;
          try {
            await withDeadline(
              WorkspaceService.saveKeyHistory(snapshot),
              12000,
            );
            historyWriteError = null;
            historyError.value = "";
          } catch (e) {
            historyWriteError = e;
            historyError.value = "密钥历史未写入数据库，请保持软件打开并重试";
          }
        }
      } finally {
        writingHistory = false;
        historyPending.value = 0;
      }
    });
  }
  async function flushKeyHistory() {
    await historyWrites;
    if (historyWriteError) throw new Error(historyError.value);
  }
  async function retryKeyHistory() {
    if (durableHistoryRequired && !durableHistory) {
      await initKeyHistory();
      return;
    }
    saveCurrentKey(true);
    if (durableHistory && historyRaw !== null) setHistoryRaw(historyRaw);
    await flushKeyHistory();
  }
  async function initKeyHistory() {
    durableHistoryRequired = true;
    // SQLite is authoritative; refresh the browser cache from the database.
    const server = JSON.parse(
      await withDeadline(WorkspaceService.keyHistory(), 12000),
    );
    readHistoryDocument(JSON.stringify(server));
    durableHistory = true;
    cacheHistory(JSON.stringify(server));
    loadKeyHistory();
    historyReady.value = true;
  }
  const currentHistoryId = computed(() =>
    currentDeleted.value || (captureArchived.value && !pendingFinishId.value)
      ? ""
      : (captureSession.value?.id ?? findHistorySource(traces.value)?.id ?? ""),
  );
  const currentSaved = computed(
    () =>
      !historyError.value &&
      !historyPending.value &&
      !!currentHistoryId.value &&
      keyHistory.value.some((e) => e.id === currentHistoryId.value),
  );

  function loadKeyHistory() {
    try {
      const document = readHistoryDocument(getHistoryRaw());
      keyHistory.value = document.items;
      captureSession.value = document.currentCapture;
      captureArchived.value = Boolean(document.currentCapture?.stoppedAt);
      if (
        !captureArchived.value ||
        pendingFinishId.value !== document.currentCapture?.id
      )
        pendingFinishId.value = "";
      deletedCaptureIds.value = document.deletedCaptureIds;
      discardDeletedCapture();
      if (captureArchived.value && !pendingFinishId.value)
        resetArchivedCapture();
      historyError.value = "";
    } catch {
      historyError.value = "历史读取失败，请勿清除浏览器数据";
    }
  }
  function saveCurrentKey(strict = false) {
    if (captureArchived.value) return;
    const entry = makeHistoryEntry(
      keyResult.value,
      traces.value,
      captureSession.value,
    );
    if (!entry) return;
    try {
      if (!captureSession.value)
        captureSession.value = { id: entry.id, createdAt: entry.createdAt };
      const raw = getHistoryRaw();
      const document = readHistoryDocument(raw);
      const previous = document.items;
      deletedCaptureIds.value = document.deletedCaptureIds;
      // The receiver may still return its last buffer, or another tab may save
      // stale traffic. Deletion must survive both without touching the receiver.
      if (deletedCaptureIds.value.includes(entry.id)) {
        keyHistory.value = previous;
        discardDeletedCapture();
        return;
      }
      // Another tab may finish this session before its storage event arrives.
      // Never overwrite the committed boundary with stale live traffic.
      const saved = previous.find((item) => item.id === entry.id);
      if (saved?.stoppedAt) {
        keyHistory.value = previous;
        captureSession.value = {
          id: saved.id,
          createdAt: saved.createdAt,
          stoppedAt: saved.stoppedAt,
        };
        captureArchived.value = true;
        resetArchivedCapture();
        historyError.value = "";
        return;
      }
      const next = upsertKeyHistory(previous, entry);
      const payload = JSON.stringify({
        version: 1,
        items: next,
        currentCapture: captureSession.value,
        deletedCaptureIds: deletedCaptureIds.value,
        retiredCaptureIds: document.retiredCaptureIds,
      });
      if (payload !== raw || historyWriteError) setHistoryRaw(payload);
      keyHistory.value = next;
      if (!historyWriteError) historyError.value = "";
    } catch {
      historyError.value = "本次提取未保存，请释放浏览器存储空间后重试";
      if (strict) throw new Error(historyError.value);
    }
  }
  function onStorage(event: StorageEvent) {
    if (event.key === KEY_HISTORY_STORAGE || event.key === null) {
      if (durableHistory) {
        void flushKeyHistory()
          .then(initKeyHistory)
          .catch(() => {
            historyError.value = "密钥历史同步失败，请重试";
          });
        return;
      }
      const previousId = captureSession.value?.id;
      loadKeyHistory();
      if (previousId !== captureSession.value?.id) {
        clearVersion++;
        resetEvents();
        traces.value = [];
        selectedId.value = "";
        void refresh();
      }
    }
  }
  function renameHistory(id: string, name: string) {
    const trimmed = name.trim();
    if (!trimmed || trimmed.length > 80)
      throw new Error("名称需为 1–80 个字符");
    try {
      const document = readHistoryDocument(getHistoryRaw());
      const entries = document.items;
      if (!entries.some((e) => e.id === id)) throw new Error("找不到这份文件");
      const next = entries.map((e) =>
        e.id === id
          ? {
              ...e,
              name: trimmed,
              updatedAt: durableHistory
                ? new Date().toISOString()
                : e.updatedAt,
            }
          : e,
      );
      setHistoryRaw(
        JSON.stringify({
          version: 1,
          items: next,
          currentCapture: document.currentCapture,
          deletedCaptureIds: document.deletedCaptureIds,
          retiredCaptureIds: document.retiredCaptureIds,
        }),
      );
      keyHistory.value = next;
      historyError.value = "";
    } catch {
      throw new Error("名称保存失败，请重试");
    }
  }
  function deleteHistories(ids: string[]) {
    if (busy.value) throw new Error("请等待当前操作完成");
    const targets = new Set(ids);
    if (!targets.size) return;
    const document = readHistoryDocument(getHistoryRaw());
    const entries = document.items;
    for (const id of targets) {
      if (
        !entries.some((entry) => entry.id === id) &&
        !document.deletedCaptureIds.includes(id)
      )
        throw new Error("找不到这条记录");
    }
    const session = document.currentCapture;
    if (
      captureEnabled.value &&
      (targets.has(currentHistoryId.value) ||
        (!!session && targets.has(session.id)))
    )
      throw new Error("请先停止提取再删除本次记录");
    const next = entries.filter((entry) => !targets.has(entry.id));
    const deletedIds = [
      ...new Set([...document.deletedCaptureIds, ...targets]),
    ];
    try {
      setHistoryRaw(
        JSON.stringify({
          version: 1,
          items: next,
          currentCapture: session,
          deletedCaptureIds: deletedIds,
          retiredCaptureIds: document.retiredCaptureIds,
        }),
      );
    } catch {
      throw new Error("删除失败，记录仍保留，请重试");
    }
    keyHistory.value = next;
    deletedCaptureIds.value = deletedIds;
    discardDeletedCapture();
  }
  function deleteHistory(id: string) {
    deleteHistories([id]);
  }

  function downloadHistory(id: string) {
    const entry = keyHistory.value.find((e) => e.id === id);
    if (!entry) throw new Error("找不到这份文件");
    if (!entry.valid) throw new Error("密钥尚未补全");
    saveBlob(
      new Blob([JSON.stringify(entry.keyJson, null, 2)], {
        type: "application/json",
      }),
      keyFilename(entry.name),
    );
  }

  const total = computed(() => traces.value.length);
  const selected = computed(
    () => traces.value.find((r) => r.id === selectedId.value) ?? null,
  );
  const keyResult = computed(() => buildKeyJson(traces.value));
  const keyRequestCount = computed(
    () => traces.value.filter((t) => isKeyRequest(t.url)).length,
  );
  const keyJsonText = computed(() =>
    JSON.stringify(keyResult.value.keyJson, null, 2),
  );
  function downloadKey() {
    if (!keyResult.value.valid)
      throw new Error("密钥信息尚不完整，请补全后导出");
    saveCurrentKey();
    saveBlob(
      new Blob([keyJsonText.value], { type: "application/json" }),
      "key.json",
    );
  }

  function captureEvent(value: unknown): CaptureEvent {
    const item = value as Record<string, unknown> | null;
    const headers = item?.headers as Record<string, unknown> | null;
    if (
      !item ||
      typeof item.id !== "string" ||
      !item.id ||
      (item.phase !== "request" && item.phase !== "response") ||
      typeof item.ts !== "string" ||
      !Number.isFinite(Date.parse(item.ts)) ||
      typeof item.method !== "string" ||
      typeof item.url !== "string" ||
      typeof item.host !== "string" ||
      typeof item.body !== "string" ||
      typeof item.bodyTruncated !== "boolean" ||
      typeof item.source !== "string" ||
      !headers ||
      Array.isArray(headers) ||
      Object.values(headers).some(
        (header) =>
          typeof header !== "string" &&
          (!Array.isArray(header) ||
            header.some((part) => typeof part !== "string")),
      )
    )
      throw new Error("收到无法识别的流量记录");
    return item as unknown as CaptureEvent;
  }
  async function setReceiverEnabled(enabled: boolean) {
    const actual = await withDeadline(
      WorkspaceService.setCaptureState(enabled),
      12000,
    );
    if (actual !== enabled) throw new Error("接收服务没有确认提取状态");
    reconcile(actual);
  }
  function resetEvents() {
    events = [];
    eventIds.clear();
  }
  function resetArchivedCapture() {
    // Keep the persisted stopped session as a boundary, so receiver replays
    // and reloads cannot restore a completed capture into the working view.
    clearVersion++;
    pendingFinishId.value = "";
    resetEvents();
    traces.value = [];
    selectedId.value = "";
    reconcile(false);
  }
  function completeCapture(id: string) {
    if (
      captureArchived.value &&
      pendingFinishId.value === id &&
      captureSession.value?.id === id
    )
      resetArchivedCapture();
  }
  function ingest(event: CaptureEvent) {
    if (currentDeleted.value || captureArchived.value) return;
    if (eventIds.has(event.id)) return;
    events.push(event);
    eventIds.add(event.id);
    if (events.length > MAX_EVENTS) eventIds.delete(events.shift()!.id);
    traces.value = mergeEvent(traces.value, event);
    saveCurrentKey();
  }
  function rebuild() {
    // Publish a complete snapshot once; intermediate rows need no Vue proxies.
    let rows: TrafficRow[] = [];
    for (const event of events) rows = mergeEvent(rows, event);
    traces.value = rows;
    saveCurrentKey();
  }
  function reconcile(enabled: boolean) {
    stateRevision++;
    captureEnabled.value = enabled;
  }
  async function refresh(strict = false) {
    loading.value = true;
    error.value = "";
    const version = clearVersion;
    const stateVersion = stateRevision;
    try {
      const [history, enabled] = await withDeadline(
        Promise.all([
          WorkspaceService.history(2500),
          WorkspaceService.captureState(),
        ]),
        12000,
      );
      if (!Array.isArray(history) || typeof enabled !== "boolean")
        throw new Error("接收服务返回的数据格式无效");
      if (version !== clearVersion) return;
      if (stateVersion === stateRevision) reconcile(enabled);
      if (currentDeleted.value || captureArchived.value) return;
      const combined = new Map<string, CaptureEvent>();
      for (const item of history) {
        const event = captureEvent(item);
        combined.set(event.id, event);
      }
      for (const event of events) combined.set(event.id, event);
      events = [...combined.values()]
        .sort((a, b) => Date.parse(a.ts) - Date.parse(b.ts))
        .slice(-MAX_EVENTS);
      eventIds.clear();
      for (const event of events) eventIds.add(event.id);
      rebuild();
    } catch (e) {
      error.value = e instanceof Error ? e.message : "无法连接接收服务";
      if (strict) throw e;
    } finally {
      loading.value = false;
    }
  }
  function connect() {
    if (!started) return;
    clearTimeout(reconnectTimer);
    reconnectTimer = undefined;
    stream?.close();
    if (!isMyGo()) {
      channelError.value = "请在桌面应用中使用实时采集";
      return;
    }
    logChannel("info", "正在建立 MyGo 实时通道");
    const source = new Channel<CaptureNotice>();
    stream = source;
    source.onmessage = (notice) => {
      if (source !== stream || !started) return;
      try {
        switch (notice.kind) {
          case "ready":
            connected.value = true;
            channelError.value = "";
            logChannel("info", "实时通道已连接 · 可以接收提取状态和新记录");
            void refresh();
            break;
          case "capture_state":
            if (typeof notice.enabled !== "boolean")
              throw new Error("接收状态数据解析失败");
            reconcile(notice.enabled);
            break;
          case "clear":
            if (notice.clientId === captureClientId) break;
            if (captureSession.value)
              captureSession.value.stoppedAt = new Date().toISOString();
            saveCurrentKey();
            clearVersion++;
            traces.value = [];
            resetEvents();
            selectedId.value = "";
            captureSession.value = null;
            captureArchived.value = false;
            pendingFinishId.value = "";
            break;
          case "message":
            ingest(captureEvent(notice.capture));
            break;
          default:
            throw new Error("收到无法识别的实时事件");
        }
      } catch (cause) {
        error.value =
          cause instanceof Error ? cause.message : "实时通道数据异常";
        channelError.value = error.value;
        logChannel("error", channelError.value);
      }
    };
    void WorkspaceService.subscribe(source).then(
      () => lostChannel(source),
      (cause) => lostChannel(source, cause),
    );
  }
  function lostChannel(source: MyGoChannel<CaptureNotice>, cause?: unknown) {
    if (source !== stream || !started) return;
    connected.value = false;
    channelError.value = "实时通道连接中断";
    const detail = cause instanceof Error ? `：${cause.message}` : "";
    logChannel("error", `实时通道连接中断${detail}，3 秒后自动重连。`);
    source.close();
    clearTimeout(reconnectTimer);
    reconnectTimer = setTimeout(connect, 3000);
  }
  async function retry() {
    if (started && !connected.value) connect();
    await refresh();
  }
  async function init() {
    if (started) return;
    started = true;
    loadKeyHistory();
    window.addEventListener("storage", onStorage);
    await refresh();
    if (started && !stream) connect();
  }
  function dispose() {
    clearTimeout(reconnectTimer);
    reconnectTimer = undefined;
    stream?.close();
    stream = null;
    connected.value = false;
    started = false;
    if (typeof window !== "undefined")
      window.removeEventListener("storage", onStorage);
  }
  async function resetReceiver() {
    clearVersion++;
    await withDeadline(WorkspaceService.clearTraces(captureClientId), 12000);
    traces.value = [];
    resetEvents();
    selectedId.value = "";
    captureSession.value = null;
    captureArchived.value = false;
    pendingFinishId.value = "";
  }
  async function startCapture(onCreated?: (id: string) => void) {
    if (busy.value || captureEnabled.value) return;
    busy.value = "start";
    try {
      // Recovery belongs to the capture action, rather than the certificate page.
      // Restart only a failed/offline monitor; a healthy TUN must keep running.
      if (isMyGo()) {
        const state = await CaptureService.state();
        if (["error", "offline"].includes(state.stage))
          await CaptureService.retry();
      }
      // A new manual capture produces a new file. Preserve the previous file
      // before resetting the receiver's working buffer.
      saveCurrentKey(true);
      await flushKeyHistory();
      await resetReceiver();
      captureSession.value = {
        id: crypto.randomUUID(),
        createdAt: new Date().toISOString(),
      };
      // An orchestrated task journals ownership before the receiver can accept
      // traffic. A failed journal must not leave an unowned active capture.
      onCreated?.(captureSession.value.id);
      // Persist the boundary before enabling capture, including an empty capture.
      saveCurrentKey(true);
      await flushKeyHistory();
      await setReceiverEnabled(true);
    } finally {
      busy.value = "";
    }
  }
  async function pauseCapture() {
    if (busy.value || !captureEnabled.value)
      throw new Error("当前没有可暂停的提取任务");
    await setReceiverEnabled(false);
  }
  async function resumeCapture() {
    if (
      busy.value ||
      captureEnabled.value ||
      !captureSession.value ||
      captureArchived.value
    )
      throw new Error("没有可继续的提取记录");
    await setReceiverEnabled(true);
  }
  async function stopCapture(options: { deferReset?: boolean } = {}) {
    if (
      busy.value ||
      (!captureEnabled.value &&
        (!captureSession.value || captureArchived.value))
    )
      return null;
    busy.value = "stop";
    try {
      await setReceiverEnabled(false);
      await refresh(true);
      const id = currentHistoryId.value;
      const session = captureSession.value && { ...captureSession.value };
      if (captureSession.value)
        captureSession.value.stoppedAt = new Date().toISOString();
      try {
        saveCurrentKey(true);
        await flushKeyHistory();
      } catch (error) {
        // A failed archive must not leak its completion marker into a later
        // automatic save while the user is still recovering unsaved content.
        captureSession.value = session;
        throw error;
      }
      const entry = keyHistory.value.find((entry) => entry.id === id) ?? null;
      if (entry) {
        captureArchived.value = true;
        if (options.deferReset) pendingFinishId.value = entry.id;
        else resetArchivedCapture();
      }
      return entry;
    } finally {
      busy.value = "";
    }
  }
  async function clear() {
    if (busy.value) return;
    if (captureEnabled.value) throw new Error("请先停止提取再清空记录");
    busy.value = "clear";
    try {
      saveCurrentKey(true);
      await flushKeyHistory();
      const document = readHistoryDocument(getHistoryRaw());
      const retiredCaptureIds = document.currentCapture
        ? [
            ...new Set([
              ...document.retiredCaptureIds,
              document.currentCapture.id,
            ]),
          ]
        : document.retiredCaptureIds;
      await resetReceiver();
      setHistoryRaw(
        JSON.stringify({
          version: 1,
          items: keyHistory.value,
          currentCapture: null,
          deletedCaptureIds: deletedCaptureIds.value,
          retiredCaptureIds,
        }),
      );
      await flushKeyHistory();
    } finally {
      busy.value = "";
    }
  }
  return {
    keyHistory,
    historyReady,
    historyError,
    currentHistoryId,
    currentSaved,
    loadKeyHistory,
    initKeyHistory,
    flushKeyHistory,
    retryKeyHistory,
    saveCurrentKey,
    downloadHistory,
    renameHistory,
    deleteHistory,
    deleteHistories,
    keyResult,
    keyRequestCount,
    keyJsonText,
    downloadKey,
    traces,
    connected,
    channelError,
    channelLogs,
    captureEnabled,
    loading,
    busy,
    error,
    selectedId,
    total,
    selected,
    init,
    dispose,
    refresh,
    retry,
    startCapture,
    pauseCapture,
    resumeCapture,
    stopCapture,
    completeCapture,
    clear,
  };
});
