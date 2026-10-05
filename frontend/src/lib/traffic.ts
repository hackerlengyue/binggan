import type { CaptureEvent, TrafficRow } from "@/types/trace";
export type { TrafficRow } from "@/types/trace";
function responseDuration(
  headers: CaptureEvent["headers"],
): number | undefined {
  for (const name of ["x-duration-ms", "x-response-time"]) {
    const raw = Object.entries(headers).find(
      ([key]) => key.toLowerCase() === name,
    )?.[1];
    if (raw === undefined || String(raw).trim() === "") continue;
    const value = Number(raw);
    if (Number.isFinite(value) && value >= 0) return Math.floor(value);
  }
}
export function mergeEvent(
  rows: TrafficRow[],
  event: CaptureEvent,
): TrafficRow[] {
  if (
    !event.id ||
    !event.url ||
    !["request", "response"].includes(event.phase) ||
    !Number.isFinite(Date.parse(event.ts))
  )
    return rows;
  const method = event.method.toUpperCase();
  const phase = event.phase === "request" ? "hasRequest" : "hasResponse";
  const target = rows.find(
    (r) =>
      r.url === event.url &&
      r.method === method &&
      (event.requestId ? r.requestId === event.requestId : !r.requestId) &&
      !r[phase] &&
      Math.abs(Date.parse(r.timestamp) - Date.parse(event.ts)) < 60_000,
  );
  const duration =
    event.phase === "response" ? responseDuration(event.headers) : undefined;
  if (target) {
    const updated = {
      ...target,
      [phase]: true,
      [event.phase]: { headers: event.headers, body: event.body },
      [`${event.phase}Truncated`]: event.bodyTruncated,
    };
    if (event.phase === "response") updated.status = event.status ?? 0;
    const elapsed =
      event.phase === "response"
        ? Date.parse(event.ts) - Date.parse(target.timestamp)
        : Date.parse(target.timestamp) - Date.parse(event.ts);
    updated.durationMs =
      responseDuration(updated.response.headers) ??
      (elapsed >= 0 ? elapsed : 0);
    if (event.phase === "request") updated.timestamp = event.ts;
    return rows.map((r) => (r.id === target.id ? updated : r));
  }
  let path = event.url;
  try {
    const url = new URL(path);
    path = url.pathname + url.search;
  } catch {
    /* Display the original address. */
  }
  return [
    {
      id: event.id,
      ...(event.requestId ? { requestId: event.requestId } : {}),
      method,
      path,
      url: event.url,
      status: event.phase === "response" ? (event.status ?? 0) : 0,
      durationMs: duration ?? 0,
      timestamp: event.ts,
      request: {
        headers: event.phase === "request" ? event.headers : {},
        body: event.phase === "request" ? event.body : "",
      },
      response: {
        headers: event.phase === "response" ? event.headers : {},
        body: event.phase === "response" ? event.body : "",
      },
      requestTruncated: event.phase === "request" && event.bodyTruncated,
      responseTruncated: event.phase === "response" && event.bodyTruncated,
      hasRequest: event.phase === "request",
      hasResponse: event.phase === "response",
    },
    ...rows,
  ].slice(0, 2500);
}
export function formatTime(value: string | number) {
  return new Date(value).toLocaleTimeString("zh-CN", { hour12: false });
}
export function formatBody(value: string) {
  try {
    return JSON.stringify(JSON.parse(value), null, 2);
  } catch {
    return value;
  }
}
export function saveBlob(blob: Blob, name: string) {
  const href = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = href;
  link.download = name;
  document.body.append(link);
  link.click();
  link.remove();
  setTimeout(() => URL.revokeObjectURL(href), 1000);
}
