import { buildKeyJson, isKeyRequest, validatePayload } from "@/lib/keyjson";
import type {
  KeyJsonPayload,
  KeyHistoryEntry,
  KeyResult,
  TrafficRow,
  CaptureSession,
  TracePart,
} from "@/types/trace";

export function defaultKeyName(timestamp: string) {
  const date = new Date(timestamp);
  return `秘钥提取 ${date.toLocaleString("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false })}`;
}
export function keyFilename(name: string) {
  return (
    (name
      .trim()
      .replace(/\.json$/i, "")
      .replace(/[\\/:*?"<>|\x00-\x1f]/g, "_")
      .slice(0, 100) || "key") + ".json"
  );
}
export const KEY_HISTORY_STORAGE = "shz_key_history_v1";

export function readCaptureSession(raw: string | null): CaptureSession | null {
  if (!raw) return null;
  const document = JSON.parse(raw);
  const session = validateCaptureSession(document.currentCapture);
  return session &&
    !validateCaptureIds(document.retiredCaptureIds).includes(session.id)
    ? session
    : null;
}

function validateCaptureSession(
  session: CaptureSession | null | undefined,
): CaptureSession | null {
  if (!session) return null;
  if (
    typeof session.id !== "string" ||
    !session.id ||
    !Number.isFinite(Date.parse(session.createdAt)) ||
    (session.stoppedAt !== undefined &&
      !Number.isFinite(Date.parse(session.stoppedAt)))
  )
    throw new Error("提取记录无法读取");
  return session;
}

export function readDeletedCaptureIds(raw: string | null): string[] {
  if (!raw) return [];
  return validateCaptureIds(JSON.parse(raw).deletedCaptureIds);
}

export function readRetiredCaptureIds(raw: string | null): string[] {
  if (!raw) return [];
  return validateCaptureIds(JSON.parse(raw).retiredCaptureIds);
}

function validateCaptureIds(value: unknown): string[] {
  const ids = value ?? [];
  if (
    !Array.isArray(ids) ||
    ids.length > 20000 ||
    !ids.every((id) => typeof id === "string" && id && id.length <= 200)
  )
    throw new Error("提取记录无法读取");
  return ids;
}

function validTrace(value: TrafficRow): boolean {
  return (
    !!value &&
    typeof value.id === "string" &&
    !!value.id &&
    typeof value.url === "string" &&
    typeof value.path === "string" &&
    typeof value.method === "string" &&
    Number.isFinite(Date.parse(value.timestamp)) &&
    Number.isFinite(value.status) &&
    Number.isFinite(value.durationMs) &&
    typeof value.hasRequest === "boolean" &&
    typeof value.hasResponse === "boolean" &&
    typeof value.requestTruncated === "boolean" &&
    typeof value.responseTruncated === "boolean" &&
    [value.request, value.response].every(
      (part) =>
        !!part &&
        typeof part.body === "string" &&
        !!part.headers &&
        typeof part.headers === "object" &&
        !Array.isArray(part.headers) &&
        Object.values(part.headers).every(
          (header) =>
            typeof header === "string" ||
            (Array.isArray(header) &&
              header.every((item) => typeof item === "string")),
        ),
    )
  );
}

function validPayload(value: unknown): value is KeyJsonPayload {
  if (!value || typeof value !== "object") return false;
  const { passwords, getPwdData: data } = value as KeyJsonPayload;
  return (
    !!passwords &&
    typeof passwords === "object" &&
    !Array.isArray(passwords) &&
    Object.entries(passwords).every(
      ([key, value]) =>
        /^[1-9]\d*$/.test(key) &&
        typeof value === "string" &&
        /^[a-f0-9]{32}$/i.test(value),
    ) &&
    !!data &&
    typeof data.密码 === "string" &&
    typeof data.uit === "string" &&
    Number.isFinite(data.den) &&
    data.den >= 0 &&
    Number.isFinite(data.pmn) &&
    data.pmn >= 0 &&
    ["", "1", "3"].includes(data.pattern) &&
    Number.isFinite(data.whenLong) &&
    data.whenLong >= 0
  );
}

export function readKeyHistory(raw: string | null): KeyHistoryEntry[] {
  if (!raw) return [];
  return validateHistoryItems(JSON.parse(raw));
}

function validateHistoryItems(value: unknown): KeyHistoryEntry[] {
  const document = value as {
    version?: unknown;
    items?: KeyHistoryEntry[];
  } | null;
  if (document?.version !== 1 || !Array.isArray(document.items))
    throw new Error("密钥历史无法读取");
  const ids = new Set<string>();
  for (const entry of document.items) {
    if (
      !entry ||
      typeof entry.id !== "string" ||
      !entry.id ||
      ids.has(entry.id) ||
      !Number.isFinite(Date.parse(entry.createdAt)) ||
      !Number.isFinite(Date.parse(entry.updatedAt)) ||
      !validPayload(entry.keyJson)
    )
      throw new Error("密钥历史无法读取");
    if (
      entry.traces !== undefined &&
      (!Array.isArray(entry.traces) || !entry.traces.every(validTrace))
    )
      throw new Error("提取记录无法读取");
    if (
      entry.issues !== undefined &&
      (!Array.isArray(entry.issues) ||
        !entry.issues.every((v: unknown) => typeof v === "string"))
    )
      throw new Error("密钥历史无法读取");
    entry.issues = [
      ...validatePayload(entry.keyJson),
      ...(entry.issues ?? []).filter((issue: string) =>
        /截断|多个 videoId|不同 uit|不同播放器参数/.test(issue),
      ),
    ];
    entry.valid = entry.issues.length === 0;
    entry.name =
      typeof entry.name === "string" && entry.name.trim()
        ? entry.name
        : defaultKeyName(entry.createdAt);
    ids.add(entry.id);
  }
  return document.items;
}

export function readHistoryDocument(raw: string | null): {
  items: KeyHistoryEntry[];
  currentCapture: CaptureSession | null;
  deletedCaptureIds: string[];
  retiredCaptureIds: string[];
} {
  if (!raw)
    return {
      items: [],
      currentCapture: null,
      deletedCaptureIds: [],
      retiredCaptureIds: [],
    };
  const document = JSON.parse(raw);
  const retiredCaptureIds = validateCaptureIds(document.retiredCaptureIds);
  const currentCapture = validateCaptureSession(document.currentCapture);
  return {
    items: validateHistoryItems(document),
    currentCapture:
      currentCapture && !retiredCaptureIds.includes(currentCapture.id)
        ? currentCapture
        : null,
    deletedCaptureIds: validateCaptureIds(document.deletedCaptureIds),
    retiredCaptureIds,
  };
}

export function findHistorySource(
  traces: TrafficRow[],
): TrafficRow | undefined {
  let first: TrafficRow | undefined;
  let firstKey: TrafficRow | undefined;
  let firstTime = Infinity;
  let firstKeyTime = Infinity;
  for (const trace of traces) {
    const time = Date.parse(trace.timestamp);
    if (!first || time < firstTime) {
      first = trace;
      firstTime = time;
    }
    if ((!firstKey || time < firstKeyTime) && isKeyRequest(trace.url)) {
      firstKey = trace;
      firstKeyTime = time;
    }
  }
  return firstKey ?? first;
}

function snapshotPart(part: TracePart): TracePart {
  return {
    ...part,
    headers: Object.fromEntries(
      Object.entries(part.headers).map(([name, value]) => [
        name,
        Array.isArray(value) ? [...value] : value,
      ]),
    ),
  };
}

export function makeHistoryEntry(
  result: KeyResult,
  traces: TrafficRow[],
  session?: CaptureSession | null,
): KeyHistoryEntry | null {
  const source = session ? undefined : findHistorySource(traces);
  if (!session && !source) return null;
  const createdAt = session?.createdAt ?? source!.timestamp;
  let latest: TrafficRow | undefined;
  let latestTime = -Infinity;
  if (!session?.stoppedAt) {
    for (const trace of traces) {
      const time = Date.parse(trace.timestamp);
      if (!latest || time >= latestTime) {
        latest = trace;
        latestTime = time;
      }
    }
  }
  return {
    name: defaultKeyName(createdAt),
    valid: result.valid,
    issues: [...result.issues],
    id: session?.id ?? source!.id,
    createdAt,
    updatedAt: session?.stoppedAt ?? latest?.timestamp ?? createdAt,
    stoppedAt: session?.stoppedAt,
    traces: traces.map((trace) => ({
      ...trace,
      request: snapshotPart(trace.request),
      response: snapshotPart(trace.response),
    })),
    keyJson: {
      passwords: { ...result.keyJson.passwords },
      getPwdData: { ...result.keyJson.getPwdData },
    },
  };
}

function mergeSavedTraces(savedRows: TrafficRow[], incomingRows: TrafficRow[]) {
  const rows = new Map(savedRows.map((row) => [row.id, row]));
  for (const row of incomingRows) {
    const saved = rows.get(row.id);
    rows.set(
      row.id,
      saved
        ? {
            ...row,
            request: row.hasRequest ? row.request : saved.request,
            response: row.hasResponse ? row.response : saved.response,
            hasRequest: row.hasRequest || saved.hasRequest,
            hasResponse: row.hasResponse || saved.hasResponse,
            requestTruncated: row.hasRequest
              ? row.requestTruncated
              : saved.requestTruncated,
            responseTruncated: row.hasResponse
              ? row.responseTruncated
              : saved.responseTruncated,
            status: row.hasResponse ? row.status : saved.status,
            durationMs: row.durationMs || saved.durationMs,
          }
        : row,
    );
  }
  return [...rows.values()].sort(
    (a, b) => Date.parse(b.timestamp) - Date.parse(a.timestamp),
  );
}

// Refreshes may return less traffic than a previous live capture. Keep saved data.
export function upsertKeyHistory(
  entries: KeyHistoryEntry[],
  incoming: KeyHistoryEntry,
): KeyHistoryEntry[] {
  const existing = entries.find((e) => e.id === incoming.id);
  let values = [
    ...new Set(
      [
        ...Object.values(existing?.keyJson.passwords ?? {}),
        ...Object.values(incoming.keyJson.passwords),
      ].map((value) => value.toLowerCase()),
    ),
  ];
  const traces = mergeSavedTraces(
    existing?.traces ?? [],
    incoming.traces ?? [],
  );
  // A partial capture may see a later segment first. Once the raw responses
  // cover every saved key, rebuild their order instead of appending earlier
  // segments at the end. Legacy keys without raw traffic must still be retained.
  const captured = Object.values(buildKeyJson(traces).keyJson.passwords);
  const capturedValues = new Set(captured);
  if (values.every((value) => capturedValues.has(value))) values = captured;
  const parameters = { ...incoming.keyJson.getPwdData };
  const oldPlayer = existing?.keyJson.getPwdData;
  const playerConflict =
    !!oldPlayer &&
    ((!!parameters.softwareName &&
      !!oldPlayer.softwareName &&
      parameters.softwareName !== oldPlayer.softwareName) ||
      (!!parameters.appMd5 &&
        !!oldPlayer.appMd5 &&
        parameters.appMd5 !== oldPlayer.appMd5));
  if (existing) {
    if (
      !playerConflict &&
      !parameters.softwareName &&
      existing.keyJson.getPwdData.softwareName
    )
      parameters.softwareName = existing.keyJson.getPwdData.softwareName;
    if (
      !playerConflict &&
      !parameters.appMd5 &&
      existing.keyJson.getPwdData.appMd5
    )
      parameters.appMd5 = existing.keyJson.getPwdData.appMd5;
    if (
      parameters.audio === undefined &&
      existing.keyJson.getPwdData.audio !== undefined
    )
      parameters.audio = existing.keyJson.getPwdData.audio;
    for (const name of Object.keys(parameters) as Array<
      keyof typeof parameters
    >) {
      if (
        !["audio", "softwareName", "appMd5"].includes(name) &&
        !parameters[name]
      )
        Object.assign(parameters, {
          [name]: existing.keyJson.getPwdData[name],
        });
    }
  }
  const next: KeyHistoryEntry = {
    ...incoming,
    name: existing?.name ?? incoming.name,
    createdAt: existing?.createdAt ?? incoming.createdAt,
    updatedAt:
      existing && existing.updatedAt > incoming.updatedAt
        ? existing.updatedAt
        : incoming.updatedAt,
    stoppedAt: incoming.stoppedAt ?? existing?.stoppedAt,
    traces,
    keyJson: {
      getPwdData: parameters,
      passwords: Object.fromEntries(
        values.map((value, i) => [String(i + 1), value]),
      ),
    },
  };
  next.issues = [
    ...validatePayload(next.keyJson),
    ...(playerConflict ? ["检测到不同播放器参数，请重新采集"] : []),
    ...[...incoming.issues, ...(existing?.issues ?? [])].filter((issue) =>
      /截断|多个 videoId|不同 uit|不同播放器参数/.test(issue),
    ),
  ];
  next.valid = next.issues.length === 0;
  return [next, ...entries.filter((e) => e.id !== incoming.id)].sort(
    (a, b) => Date.parse(b.createdAt) - Date.parse(a.createdAt),
  );
}
