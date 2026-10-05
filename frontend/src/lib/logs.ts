export interface SystemLog {
  id: string;
  at: string;
  level: string;
  source: string;
  message: string;
  detail: string;
  requestId: string;
}

/** Explicit levels take precedence: a successful check may mention "异常 0 秒". */
export function logLineLevel(line: string): "error" | "warning" | "info" {
  const level = line
    .match(
      /^\[[^\]]+\]\s*\[(INFO|PROGRESS|RESULT|DEBUG|WARN(?:ING)?|ERROR|FATAL|STDERR)\]/i,
    )?.[1]
    ?.toUpperCase();
  if (level) {
    if (["ERROR", "FATAL", "STDERR"].includes(level)) return "error";
    if (["WARN", "WARNING"].includes(level)) return "warning";
    return "info";
  }
  if (/^\s*(?:ERROR\b|FATAL\b|Traceback\b|错误[：:]|失败[：:])/i.test(line))
    return "error";
  if (/^\s*(?:WARN(?:ING)?\b|警告[：:])/i.test(line)) return "warning";
  return "info";
}
export function parseSystemLogs(value: unknown): {
  items: SystemLog[];
  total: number;
} {
  const data = value as { items?: unknown; total?: unknown } | null;
  if (
    !data ||
    !Number.isSafeInteger(data.total) ||
    Number(data.total) < 0 ||
    !Array.isArray(data.items) ||
    data.items.some(
      (row) =>
        !row ||
        ["id", "at", "level", "source", "message", "detail", "requestId"].some(
          (key) => typeof row[key] !== "string",
        ),
    )
  )
    throw new Error("日志响应格式无效");
  return data as { items: SystemLog[]; total: number };
}
export function logSearchContext(text: string, query: string) {
  if (!query.trim()) return { text, matches: 0 };
  const needle = query.trim().toLocaleLowerCase(),
    lines = text.split("\n"),
    indices = new Set<number>();
  let matches = 0;
  lines.forEach((line, index) => {
    if (!line.toLocaleLowerCase().includes(needle)) return;
    matches++;
    for (
      let i = Math.max(0, index - 3);
      i <= Math.min(lines.length - 1, index + 3);
      i++
    )
      indices.add(i);
  });
  let previous = -1;
  const output: string[] = [];
  for (const index of [...indices].sort((a, b) => a - b)) {
    if (index > previous + 1) output.push("···");
    output.push(`${index + 1}  ${lines[index]}`);
    previous = index;
  }
  return { text: output.join("\n"), matches };
}
export function parseLogChunk(
  value: unknown,
  offset: number,
): { data: string; offset: number; hasMore: boolean } {
  const data = value as {
    data?: unknown;
    offset?: unknown;
    hasMore?: unknown;
  } | null;
  if (
    !data ||
    typeof data.data !== "string" ||
    !Number.isSafeInteger(data.offset) ||
    Number(data.offset) < offset ||
    typeof data.hasMore !== "boolean" ||
    (data.hasMore && Number(data.offset) === offset)
  )
    throw new Error("日志响应格式无效，请重试");
  const bytes = atob(data.data).length;
  if (Number(data.offset) - offset !== bytes)
    throw new Error("日志字节位置不一致，请重试");
  return data as { data: string; offset: number; hasMore: boolean };
}

/** Localize ISO timestamps for display only; stored/downloaded logs stay intact. */
export function formatLogTimes(text: string) {
  return text.replace(
    /\b\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})\b/g,
    (value) => {
      const date = new Date(value);
      if (!Number.isFinite(date.getTime())) return value;
      const pad = (number: number, length = 2) =>
        String(number).padStart(length, "0");
      return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
    },
  );
}

export interface LogLine {
  time: string;
  level: string;
  message: string;
}
/**
 * Reads one engine log line. The engine writes "[time] [LEVEL] message"; older
 * and external lines may be "time LEVEL message", "[LEVEL] message" or plain text.
 * Plain text returns an empty level so callers can render it as a continuation.
 */
export function parseLogLine(line: string): LogLine {
  const levels = "INFO|WARN|WARNING|ERROR|FATAL|DEBUG|PROGRESS|RESULT|STDERR";
  const stamp = String.raw`(\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}:\d{2}(?:[.,]\d+)?(?:Z|[+-]\d{2}:?\d{2})?)`;
  const withTime = line.match(
    new RegExp(String.raw`^\[?${stamp}\]?\s+\[?(${levels})\]?\s*(.*)$`, "i"),
  );
  if (withTime) {
    return {
      time: withTime[1]!.match(/\d{2}:\d{2}:\d{2}/)?.[0] ?? withTime[1]!,
      level: withTime[2]!.toUpperCase(),
      message: withTime[3]!.trim(),
    };
  }
  const bracket = line.match(/^\[([^\]]+)\]\s*\[(\w+)\]\s*(.*)$/);
  if (bracket && new RegExp(`^(${levels})$`, "i").test(bracket[2]!)) {
    return {
      time: bracket[1]!.match(/\d{2}:\d{2}:\d{2}/)?.[0] ?? bracket[1]!,
      level: bracket[2]!.toUpperCase(),
      message: bracket[3]!.trim(),
    };
  }
  const levelOnly = line.match(
    new RegExp(String.raw`^\[?(${levels})\]?[:\s]\s*(.*)$`, "i"),
  );
  if (levelOnly)
    return {
      time: "",
      level: levelOnly[1]!.toUpperCase(),
      message: levelOnly[2]!.trim(),
    };
  return { time: "", level: "", message: line.trim() };
}
