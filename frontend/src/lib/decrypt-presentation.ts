import { parseLogLine } from "./logs";
export function defaultTaskName(names: string[], time: string): string {
  const name =
    names.length > 1
      ? `多任务 ${time}`
      : names[0]?.replace(/\.sz$/i, "").trim() || `解密任务 ${time}`;
  return Array.from(name).slice(0, 100).join("");
}
export function conciseDecryptError(message: string): string {
  return message.split(/；FFmpeg 解码诊断：|\n|\[aac @/)[0]!.trim();
}
export interface DecryptLogEvent {
  time: string;
  level: string;
  message: string;
}
export function decryptLogEvents(text: string): DecryptLogEvent[] {
  const events: DecryptLogEvent[] = [];
  for (const line of text.split("\n")) {
    const parsed = parseLogLine(line);
    if (!parsed.level || !parsed.time) continue;
    const { time, level } = parsed;
    if (!["INFO", "WARN", "WARNING", "ERROR", "FATAL"].includes(level))
      continue;
    let message = parsed.message;
    if (
      level === "INFO" &&
      !/^(?:子任务已创建|第 \d+ 次尝试|开始视频解密|视频解密完成|开始音频检查|音频检查完成|开始音频修复|音频修复完成|开始最终校验|最终校验完成|校验结果|处理结果)/.test(
        message,
      )
    )
      continue;
    if (message.startsWith("子任务已创建")) message = "已加入队列";
    if (
      message.startsWith("校验结果：") &&
      message.includes("异常 0 秒") &&
      message.includes("丢失 0 秒")
    )
      message = "视频与音频校验通过";
    if (message.startsWith("处理结果："))
      message = message.replace(/ · 修复音频 \d+ 帧/, "");
    message = conciseDecryptError(message);
    events.push({ time, level, message });
  }
  return events;
}
