import { isKeyJsonPayload } from "@/lib/key-history";
import { validatePayload } from "@/lib/keyjson";
import type { KeyHistoryEntry } from "@/types/trace";

export interface KeyFile {
  name: string;
  raw: string;
}

export function keyFileError(file: Pick<File, "name" | "size">): string {
  if (!/\.json$/i.test(file.name)) return "请选择 .json 格式的密钥文件。";
  if (!file.size) return "这个文件是空的，请重新选择。";
  if (file.size > 1024 * 1024) return "密钥文件不能超过 1 MB。";
  return "";
}

export function makeImportedKey(file: KeyFile): KeyHistoryEntry {
  let value;
  try {
    value = JSON.parse(file.raw.replace(/^\uFEFF/, ""));
  } catch {
    throw new Error("密钥 JSON 格式无效。");
  }
  if (
    !value ||
    typeof value !== "object" ||
    Array.isArray(value) ||
    !value.getPwdData ||
    typeof value.getPwdData !== "object" ||
    Array.isArray(value.getPwdData)
  )
    throw new Error("密钥文件需要包含 passwords 和 getPwdData。");

  const noSlots = ["4", "<null>", "szpd"].includes(value.getPwdData.pattern);
  const keyJson = {
    ...value,
    getPwdData: {
      ...(noSlots ? { 密码: "", uit: "", den: 0, pmn: 0 } : {}),
      ...value.getPwdData,
      whenLong:
        value.getPwdData.whenLong ??
        value.getPwdData.den ??
        (noSlots ? 0 : undefined),
    },
  };
  if (!isKeyJsonPayload(keyJson))
    throw new Error("密钥文件格式无效，请检查 passwords 和 getPwdData。");
  if (Object.keys(keyJson.passwords).length > 10000)
    throw new Error("每个密钥文件最多包含 10000 条密码。");
  if (validatePayload(keyJson).length)
    throw new Error("密钥文件不完整，请补全密码和解密参数。");
  const data = keyJson.getPwdData;
  if (
    ![data.den, data.pmn, data.whenLong].every(
      (value) =>
        Number.isSafeInteger(value) &&
        (["4", "<null>", "szpd"].includes(data.pattern)
          ? value >= 0
          : value > 0),
    ) ||
    (data.audio !== undefined &&
      (!Number.isSafeInteger(data.audio) || data.audio < 0)) ||
    (data.softwareName !== undefined &&
      typeof data.softwareName !== "string") ||
    (data.appMd5 !== undefined && !/^[a-f0-9]{32}$/i.test(data.appMd5))
  )
    throw new Error("密钥文件中的解密参数无效。");

  const now = new Date().toISOString();
  return {
    id: crypto.randomUUID(),
    source: "upload",
    name:
      file.name
        .split(/[\\/]/)
        .pop()!
        .replace(/\.json$/i, "")
        .trim()
        .slice(0, 80) || "key",
    createdAt: now,
    updatedAt: now,
    valid: true,
    issues: [],
    keyJson,
  };
}
