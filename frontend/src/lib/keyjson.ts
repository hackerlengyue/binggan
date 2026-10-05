import type { KeyJsonPayload, KeyResult, TraceEntry } from "@/types/trace";

interface KeyAccumulator {
  sourceCount: number;
  updatedAt: string;
  cipherPassword: string;
  uit: string;
  den: number;
  pmn: number;
  pattern: string;
  whenLong: number;
  audio?: number;
  softwareName?: string;
  appMd5?: string;
  playerVersions: Set<string>;
  playerHashes: Set<string>;
  passwords: Record<string, string>;
  nextIndex: number;
  dtsIndexMap: Record<string, string>;
}

type PasswordCandidate = { index: string; value: unknown };

function assignCandidates(
  acc: KeyAccumulator,
  candidates: PasswordCandidate[],
  fallbackIndex = "",
): void {
  const unique = new Map<string, string>();
  for (const { index, value } of candidates) {
    const password = normalizePasswordValue(value);
    if (!password) continue;
    const previous = unique.get(password);
    if (previous === undefined || (!previous && index))
      unique.set(password, index);
  }
  // A request index describes a single segment, not every key in a batch.
  for (const [password, index] of unique)
    assignPassword(
      acc,
      index || (unique.size === 1 ? fallbackIndex : ""),
      password,
    );
}

const PERINUTE_PATH = "/api/courselocal/getperinutepwd";

function decodeMaybe(value: string): string {
  if (!value) {
    return "";
  }
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
}

function getStateHash(acc: KeyAccumulator): string {
  const passwords = Object.entries(acc.passwords).sort(
    (a, b) => Number(a[0]) - Number(b[0]),
  );
  return JSON.stringify([
    acc.cipherPassword,
    acc.uit,
    acc.den,
    acc.pmn,
    acc.pattern,
    acc.whenLong,
    acc.audio,
    acc.nextIndex,
    passwords,
  ]);
}

function toNonEmptyString(value: unknown): string {
  if (value === null || typeof value === "undefined") {
    return "";
  }
  return String(value).trim();
}

function toPositiveInt(value: unknown): number {
  const n = Number(value);
  if (!Number.isFinite(n)) {
    return 0;
  }
  const v = Math.floor(n);
  return v > 0 ? v : 0;
}

function toPattern(value: unknown): string {
  const text = toNonEmptyString(value);
  if (text === "1" || text === "3") {
    return text;
  }
  return "";
}

function normalizeIndex(value: unknown): string {
  const n = Number(value);
  if (!Number.isFinite(n)) {
    return "";
  }
  const i = Math.floor(n);
  return i > 0 ? String(i) : "";
}

function nextAutoIndex(acc: KeyAccumulator): string {
  while (acc.passwords[String(acc.nextIndex)]) {
    acc.nextIndex += 1;
  }
  const index = String(acc.nextIndex);
  acc.nextIndex += 1;
  return index;
}

function assignPassword(
  acc: KeyAccumulator,
  index: string,
  passwordRaw: unknown,
): boolean {
  const password = normalizePasswordValue(passwordRaw);
  if (!password || Object.values(acc.passwords).includes(password))
    return false;

  const requestedIndex = normalizeIndex(index);
  // Retain every distinct captured key, even if the server reuses a segment index.
  const targetIndex =
    requestedIndex && !acc.passwords[requestedIndex]
      ? requestedIndex
      : nextAutoIndex(acc);
  acc.passwords[targetIndex] = password;
  acc.nextIndex = Math.max(acc.nextIndex, Number(targetIndex) + 1);
  return true;
}

function parseJsonLoose(raw: string): unknown {
  const text = raw.trim();
  if (!text) {
    return null;
  }

  const candidates = [text];
  const objStart = text.indexOf("{");
  const objEnd = text.lastIndexOf("}");
  if (objStart >= 0 && objEnd > objStart) {
    candidates.push(text.slice(objStart, objEnd + 1));
  }

  const arrStart = text.indexOf("[");
  const arrEnd = text.lastIndexOf("]");
  if (arrStart >= 0 && arrEnd > arrStart) {
    candidates.push(text.slice(arrStart, arrEnd + 1));
  }

  for (const candidate of candidates) {
    try {
      const parsed = JSON.parse(candidate);
      if (typeof parsed === "string") {
        const nested = parsed.trim();
        if (nested.startsWith("{") || nested.startsWith("[")) {
          try {
            return JSON.parse(nested);
          } catch {
            return parsed;
          }
        }
      }
      return parsed;
    } catch {
      // keep trying
    }
  }

  return null;
}

function normalizePasswordValue(value: unknown): string {
  const normalizeHex = (input: unknown): string => {
    const text = toNonEmptyString(input);
    if (!text) {
      return "";
    }
    if (/^[a-f0-9]{32}$/i.test(text)) {
      return text.toLowerCase();
    }
    return "";
  };

  if (typeof value === "string") {
    return normalizeHex(value);
  }

  if (typeof value === "number" && Number.isFinite(value)) {
    return normalizeHex(value);
  }

  if (value && typeof value === "object" && !Array.isArray(value)) {
    const record = value as Record<string, unknown>;
    const candidates = [
      record.password,
      record.pwd,
      record.pass,
      record["密码"],
      record.value,
    ];
    for (const item of candidates) {
      const direct = normalizeHex(item);
      if (direct) {
        return direct;
      }
    }
  }

  return "";
}

function parseIntFromText(raw: string, keys: string[]): number {
  for (const key of keys) {
    const quoted = new RegExp(
      `["']?${key}["']?\\s*[:=]\\s*["']?(\\d+)["']?`,
      "i",
    );
    const foundQuoted = raw.match(quoted)?.[1];
    if (foundQuoted) {
      const val = toPositiveInt(foundQuoted);
      if (val > 0) {
        return val;
      }
    }

    const query = new RegExp(`(?:^|[?&])${key}=(\\d+)`, "i");
    const foundQuery = raw.match(query)?.[1];
    if (foundQuery) {
      const val = toPositiveInt(foundQuery);
      if (val > 0) {
        return val;
      }
    }
  }
  return 0;
}

function parseStringFromText(raw: string, keys: string[]): string {
  for (const key of keys) {
    const quoted = new RegExp(
      `["']?${key}["']?\\s*[:=]\\s*["']([^"'&\\s]+)["']`,
      "i",
    );
    const foundQuoted = raw.match(quoted)?.[1];
    if (foundQuoted) {
      return decodeMaybe(foundQuoted).trim();
    }

    const query = new RegExp(`(?:^|[?&])${key}=([^&\\s]+)`, "i");
    const foundQuery = raw.match(query)?.[1];
    if (foundQuery) {
      return decodeMaybe(foundQuery).trim();
    }
  }
  return "";
}

function isPerinuteUrl(url: string): boolean {
  return url.toLowerCase().includes(PERINUTE_PATH);
}

function headerValue(
  headers: Record<string, string | string[]>,
  name: string,
): string {
  const wanted = name.toLowerCase();
  for (const [k, v] of Object.entries(headers)) {
    if (k.toLowerCase() !== wanted) {
      continue;
    }
    if (Array.isArray(v)) {
      return toNonEmptyString(v[0]);
    }
    return toNonEmptyString(v);
  }
  return "";
}

function extractMimaCipherFromPwdMapsRecord(
  record: Record<string, unknown>,
): string {
  const rootPwdMaps = record.pwdMaps;
  const data =
    record.data &&
    typeof record.data === "object" &&
    !Array.isArray(record.data)
      ? (record.data as Record<string, unknown>)
      : null;
  const dataPwdMaps = data?.pwdMaps;

  const source = ((rootPwdMaps &&
  typeof rootPwdMaps === "object" &&
  !Array.isArray(rootPwdMaps)
    ? rootPwdMaps
    : null) ??
    (dataPwdMaps &&
    typeof dataPwdMaps === "object" &&
    !Array.isArray(dataPwdMaps)
      ? dataPwdMaps
      : null)) as Record<string, unknown> | null;

  if (!source) {
    return "";
  }

  const readPasswordFromNode = (node: unknown): string => {
    if (!node || typeof node !== "object" || Array.isArray(node)) {
      return "";
    }
    const item = node as Record<string, unknown>;
    return toNonEmptyString(
      item["密码"] ?? item.pwd ?? item.password ?? item.pass ?? item.value,
    );
  };

  const preferredFirst = readPasswordFromNode(source["第一个密码"]);
  if (preferredFirst) {
    return preferredFirst;
  }

  for (const node of Object.values(source)) {
    const found = readPasswordFromNode(node);
    if (found) {
      return found;
    }
  }

  return "";
}

function extractMimaCipherFromRawText(raw: string): string {
  const firstPwdInPwdMaps =
    /["']?pwdMaps["']?[\s\S]{0,400}?["']?第一个密码["']?[\s\S]{0,260}?["']?密码["']?\s*[:=]\s*["']([^"']+)["']/i;
  const matched = raw.match(firstPwdInPwdMaps)?.[1];
  if (matched) {
    return decodeMaybe(matched).trim();
  }

  return "";
}

function extractMimaCipherFromBody(rawBody: string): string {
  const body = rawBody.trim();
  if (!body) {
    return "";
  }

  const parsed = parseJsonLoose(body);
  if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
    const found = extractMimaCipherFromPwdMapsRecord(
      parsed as Record<string, unknown>,
    );
    if (found) {
      return found;
    }
  }

  return extractMimaCipherFromRawText(body);
}

function extractObjectAfterKey(raw: string, key: string): string {
  const keyRegex = new RegExp(`["']?${key}["']?\\s*[:=]\\s*\\{`, "i");
  const m = keyRegex.exec(raw);
  if (!m) {
    return "";
  }

  const openIdx = raw.indexOf("{", m.index);
  if (openIdx < 0) {
    return "";
  }

  let depth = 0;
  for (let i = openIdx; i < raw.length; i += 1) {
    const ch = raw[i];
    if (ch === "{") {
      depth += 1;
    } else if (ch === "}") {
      depth -= 1;
      if (depth === 0) {
        return raw.slice(openIdx, i + 1);
      }
    }
  }

  return raw.slice(openIdx);
}

function inferPatternFromUrl(url: string): string {
  const u = url.toLowerCase();
  if (u.includes("pwd3")) {
    return "3";
  }
  if (u.includes("pwd")) {
    return "1";
  }
  return "";
}

function applyFromRawText(
  raw: string,
  acc: KeyAccumulator,
  options: { fallbackIndex?: string; url?: string } = {},
): void {
  applyPwdData(
    {
      softwareName: parseStringFromText(raw, ["softwareName", "software_name"]),
      appMd5: parseStringFromText(raw, ["appMd5", "app_md5"]),
    },
    acc,
  );
  const fallbackIndex = normalizeIndex(options.fallbackIndex ?? "");
  const candidates: PasswordCandidate[] = [];

  const inferredPattern = options.url ? inferPatternFromUrl(options.url) : "";
  if (!acc.pattern && inferredPattern) {
    acc.pattern = inferredPattern;
  }

  const den = parseIntFromText(raw, ["den"]);
  if (den > 0) {
    acc.den = den;
  }

  const pmn = parseIntFromText(raw, ["pmn"]);
  if (pmn > 0) {
    acc.pmn = pmn;
  }
  const num = parseIntFromText(raw, ["num"]);
  if (acc.pmn <= 0 && num > 0) {
    acc.pmn = num;
  }

  const whenLong = parseIntFromText(raw, ["whenLong", "whenlong"]);
  if (whenLong > 0) {
    acc.whenLong = whenLong;
  }

  const patternValue = parseStringFromText(raw, ["pattern"]);
  const pattern = toPattern(patternValue);
  if (pattern) {
    acc.pattern = pattern;
  }

  const indexedPairRegex =
    /["']?(?:index|idx|segmentIndex|segment|order|no|num)["']?\s*[:=]\s*["']?(\d+)["']?[\s\S]{0,120}?["']?(?:password|pwd|pass)["']?\s*[:=]\s*["']([^"']+)["']/gi;
  for (const match of raw.matchAll(indexedPairRegex)) {
    const idx = normalizeIndex(match[1]);
    const pwd = decodeMaybe((match[2] || "").trim());
    candidates.push({ index: idx, value: pwd });
  }

  const reverseIndexedPairRegex =
    /["']?(?:password|pwd|pass)["']?\s*[:=]\s*["']([^"']+)["'][\s\S]{0,120}?["']?(?:index|idx|segmentIndex|segment|order|no|num)["']?\s*[:=]\s*["']?(\d+)["']?/gi;
  for (const match of raw.matchAll(reverseIndexedPairRegex)) {
    const idx = normalizeIndex(match[2]);
    const pwd = decodeMaybe((match[1] || "").trim());
    candidates.push({ index: idx, value: pwd });
  }

  const namedPwdRegex =
    /["']?(?:pwd|password)(\d+)["']?\s*[:=]\s*["']([^"']+)["']/gi;
  for (const match of raw.matchAll(namedPwdRegex)) {
    const idx = normalizeIndex(match[1]);
    const pwd = decodeMaybe((match[2] || "").trim());
    candidates.push({ index: idx, value: pwd });
  }

  const queryPwdRegex = /(?:^|[?&])(?:pwd|password)(\d+)=([^&\s]+)/gi;
  for (const match of raw.matchAll(queryPwdRegex)) {
    const idx = normalizeIndex(match[1]);
    const pwd = decodeMaybe((match[2] || "").trim());
    candidates.push({ index: idx, value: pwd });
  }

  const passwordsBlock = extractObjectAfterKey(raw, "passwords");
  if (passwordsBlock) {
    const pairRegex = /["'](\d+)["']\s*[:=]\s*["']([^"']+)["']/g;
    for (const match of passwordsBlock.matchAll(pairRegex)) {
      const idx = normalizeIndex(match[1]);
      const pwd = decodeMaybe((match[2] || "").trim());
      candidates.push({ index: idx, value: pwd });
    }
  }

  const plainPwdRegex =
    /["']?(?:pwd|password|密码)["']?\s*[:=]\s*["']([^"']+)["']/gi;
  for (const match of raw.matchAll(plainPwdRegex)) {
    const pwd = decodeMaybe((match[1] || "").trim());
    candidates.push({ index: "", value: pwd });
  }
  assignCandidates(acc, candidates, fallbackIndex);
}

function applyPwdData(
  record: Record<string, unknown>,
  acc: KeyAccumulator,
): void {
  const software =
    record.softwareName ?? record.softwarename ?? record.software_name;
  const appMd5 = record.appMd5 ?? record.appmd5 ?? record.app_md5;
  if (typeof software === "string" && software.trim()) {
    acc.softwareName = software.trim();
    acc.playerVersions.add(acc.softwareName);
  }
  if (typeof appMd5 === "string" && /^[a-f0-9]{32}$/i.test(appMd5.trim())) {
    acc.appMd5 = appMd5.trim().toLowerCase();
    acc.playerHashes.add(acc.appMd5);
  }
  if (
    typeof record.audio === "number" &&
    Number.isInteger(record.audio) &&
    record.audio >= 0
  )
    acc.audio = record.audio;
  const den = toPositiveInt(record.den);
  if (den > 0) {
    acc.den = den;
  }

  const pmn = toPositiveInt(record.pmn);
  if (pmn > 0) {
    acc.pmn = pmn;
  } else if (acc.pmn <= 0) {
    const num = toPositiveInt(record.num);
    if (num > 0) {
      acc.pmn = num;
    }
  }

  const pattern = toPattern(record.pattern);
  if (pattern) {
    acc.pattern = pattern;
  }

  const whenLong = toPositiveInt(record.whenLong ?? record.whenlong);
  if (whenLong > 0) {
    acc.whenLong = whenLong;
  }
}

function walkAndCollect(
  node: unknown,
  acc: KeyAccumulator,
  candidates: PasswordCandidate[],
): void {
  if (!node || typeof node !== "object") {
    return;
  }

  if (Array.isArray(node)) {
    for (const item of node) {
      walkAndCollect(item, acc, candidates);
    }
    return;
  }

  const record = node as Record<string, unknown>;
  applyPwdData(record, acc);

  const getPwdData = record.getPwdData;
  if (
    getPwdData &&
    typeof getPwdData === "object" &&
    !Array.isArray(getPwdData)
  ) {
    applyPwdData(getPwdData as Record<string, unknown>, acc);
  }

  const passwords = record.passwords;
  if (passwords && typeof passwords === "object") {
    for (const [key, value] of Object.entries(passwords)) {
      const index = Array.isArray(passwords)
        ? String(Number(key) + 1)
        : normalizeIndex(key);
      if (index) candidates.push({ index, value });
    }
  }

  const index = normalizeIndex(
    record.index ??
      record.idx ??
      record.segmentIndex ??
      record.segment ??
      record.order ??
      record.no,
  );
  const passwordValue =
    record.password ?? record.pwd ?? record.pass ?? record["密码"];
  if (passwordValue) {
    candidates.push({ index, value: passwordValue });
  }

  if (!index) {
    const numberedPairs = Object.entries(record).filter(([k]) =>
      /^(?:pwd|password)\d+$/i.test(k),
    );
    for (const [k, v] of numberedPairs) {
      const matchIndex = k.match(/(\d+)$/)?.[1] ?? "";
      candidates.push({ index: matchIndex, value: v });
    }
  }

  for (const [name, value] of Object.entries(record)) {
    if (name !== "passwords") walkAndCollect(value, acc, candidates);
  }
}

function sortPasswords(
  passwords: Record<string, string>,
): Record<string, string> {
  const sortedValues = Object.entries(passwords)
    .filter(([, value]) => Boolean(value.trim()))
    .sort((a, b) => Number(a[0]) - Number(b[0]));

  const reindexed = sortedValues.map(([, value], idx) => [
    String(idx + 1),
    value,
  ]);
  return Object.fromEntries(reindexed);
}

function buildPayload(acc: KeyAccumulator): KeyJsonPayload {
  const den = acc.den > 0 ? acc.den : 0;
  const pmn = acc.pmn > 0 ? acc.pmn : 0;
  const whenLong = acc.whenLong > 0 ? acc.whenLong : den;

  return {
    passwords: sortPasswords(acc.passwords),
    getPwdData: {
      密码: acc.cipherPassword,
      uit: acc.uit,
      den,
      pmn,
      pattern: acc.pattern,
      whenLong,
      ...(acc.softwareName ? { softwareName: acc.softwareName } : {}),
      ...(acc.appMd5 ? { appMd5: acc.appMd5 } : {}),
      ...(acc.audio !== undefined ? { audio: acc.audio } : {}),
    },
  };
}

export function validatePayload(payload: KeyJsonPayload): string[] {
  const issues: string[] = [];

  if (!payload.getPwdData["密码"]) {
    issues.push("密码 为空");
  }
  if (!payload.getPwdData.uit) {
    issues.push("uit 为空");
  }
  if (!(payload.getPwdData.den > 0)) {
    issues.push("den 必须大于 0");
  }
  if (!(payload.getPwdData.pmn > 0)) {
    issues.push("pmn 必须大于 0");
  }
  if (!(
    payload.getPwdData.pattern === "1" || payload.getPwdData.pattern === "3"
  )) {
    issues.push("pattern 必须是 1 或 3");
  }
  if (Object.keys(payload.passwords).length < 1) {
    issues.push("passwords 至少 1 条");
  }

  return issues;
}

function createAccumulator(): KeyAccumulator {
  return {
    playerVersions: new Set(),
    playerHashes: new Set(),
    sourceCount: 0,
    updatedAt: "",
    cipherPassword: "",
    uit: "",
    den: 0,
    pmn: 0,
    pattern: "",
    whenLong: 0,
    passwords: {},
    nextIndex: 1,
    dtsIndexMap: {},
  };
}

function collectFromBody(
  rawBody: string,
  acc: KeyAccumulator,
  options: { fallbackIndex?: string; url?: string } = {},
): boolean {
  const body = rawBody.trim();
  if (!body) {
    return false;
  }

  const before = getStateHash(acc);
  const inferredPattern = options.url ? inferPatternFromUrl(options.url) : "";
  if (!acc.pattern && inferredPattern) acc.pattern = inferredPattern;

  const parsed = parseJsonLoose(body);
  if (parsed && typeof parsed === "object") {
    const candidates: PasswordCandidate[] = [];
    walkAndCollect(parsed, acc, candidates);
    assignCandidates(acc, candidates, options.fallbackIndex);
  } else applyFromRawText(body, acc, options);
  const after = getStateHash(acc);
  return before !== after;
}

function resolveFallbackIndexFromRequest(
  requestBody: string,
  acc: KeyAccumulator,
): string {
  const dts = parseIntFromText(requestBody, ["dts", "startTime", "start"]);
  if (dts > 0) {
    const key = String(dts);
    const existed = acc.dtsIndexMap[key];
    if (existed) {
      return existed;
    }
    const created = nextAutoIndex(acc);
    acc.dtsIndexMap[key] = created;
    return created;
  }

  const explicit = parseIntFromText(requestBody, [
    "index",
    "idx",
    "segmentIndex",
    "segment",
    "order",
    "no",
    "num",
  ]);
  if (explicit > 0) {
    return String(explicit);
  }

  return "";
}

export function isKeyRequest(url: string): boolean {
  try {
    return /^\/api\/courselocal\/(getperinutepwd|getpwd[^/]*)$/i.test(
      new URL(url).pathname,
    );
  } catch {
    return false;
  }
}

// Collect only successful key responses from the complete capture.
export function buildKeyJson(traces: TraceEntry[]): KeyResult {
  const acc = createAccumulator();
  let truncatedCount = 0;
  const videoIds = new Set<string>();
  const users = new Set<string>();
  const ordered = traces
    .filter((t) => isKeyRequest(t.url))
    .sort((a, b) => Date.parse(a.timestamp) - Date.parse(b.timestamp));
  for (const trace of ordered) {
    if (trace.status < 200 || trace.status >= 300) continue;
    if (trace.responseTruncated || trace.requestTruncated) {
      truncatedCount++;
      continue;
    }
    // Recognized source IDs prevent combining two explicit videos into one file.
    const request = parseJsonLoose(trace.request.body);
    const bodyId =
      request && typeof request === "object"
        ? (request as Record<string, unknown>).videoId
        : undefined;
    const queryId = new URL(trace.url).searchParams.get("videoId");
    const videoId = toNonEmptyString(bodyId ?? queryId);
    if (videoId) videoIds.add(videoId);
    const query = Object.fromEntries(new URL(trace.url).searchParams);
    applyPwdData(query, acc);
    applyPwdData(
      {
        softwareName: headerValue(trace.request.headers, "softwarename"),
        appMd5: headerValue(trace.request.headers, "appmd5"),
      },
      acc,
    );
    acc.sourceCount++;
    acc.updatedAt = trace.timestamp;
    const oldPasswords = { ...acc.passwords };
    const oldIndex = acc.nextIndex;
    // Request bodies provide parameters; key values must come from a response.
    collectFromBody(trace.request.body, acc, {
      url: isPerinuteUrl(trace.url) ? undefined : trace.url,
    });
    acc.passwords = { ...oldPasswords };
    acc.nextIndex = oldIndex;
    if (isPerinuteUrl(trace.url)) {
      const uit = headerValue(trace.request.headers, "uit");
      if (uit) {
        acc.uit = uit;
        users.add(uit);
      }
      const cipher = extractMimaCipherFromBody(trace.response.body);
      if (cipher) acc.cipherPassword = cipher;
      collectFromBody(trace.response.body, acc);
      // The encrypted password is not a segment key, even if it looks like hex.
      acc.passwords = { ...oldPasswords };
      acc.nextIndex = oldIndex;
    } else {
      const fallbackIndex = resolveFallbackIndexFromRequest(
        trace.request.body,
        acc,
      );
      collectFromBody(trace.response.body, acc, {
        fallbackIndex,
        url: trace.url,
      });
    }
  }
  const keyJson = buildPayload(acc);
  const issues = validatePayload(keyJson);
  if (acc.playerVersions.size > 1 || acc.playerHashes.size > 1)
    issues.push("检测到不同播放器参数，请清空后使用同一版本重新采集");
  if (truncatedCount)
    issues.push(`${truncatedCount} 条密钥请求或响应被截断，请重新采集完整内容`);
  if (videoIds.size > 1)
    issues.push("检测到多个 videoId，请清空后只采集一个视频");
  if (users.size > 1) issues.push("检测到不同 uit，请清空后重新采集");
  return {
    sourceCount: acc.sourceCount,
    updatedAt: acc.updatedAt,
    valid: issues.length === 0,
    issues,
    passwordCount: Object.keys(keyJson.passwords).length,
    keyJson,
  };
}
