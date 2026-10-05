export interface DecryptChoice {
  id: string;
  name: string;
  source?: string;
}
export function videoStem(name: string): string {
  return name
    .split(/[\\/]/)
    .pop()!
    .replace(/\.(sz|json)$/i, "")
    .normalize("NFKC")
    .replace(/\s+/g, "")
    .toLocaleLowerCase();
}
export function matchingKeys(
  name: string,
  keys: DecryptChoice[],
): DecryptChoice[] {
  const stem = videoStem(name);
  return stem ? keys.filter((key) => videoStem(key.name) === stem) : [];
}
export function videoFileError(file: Pick<File, "name" | "size">): string {
  if (!/\.sz$/i.test(file.name)) return "请选择 .sz 格式的视频文件。";
  if (!file.size) return "这个文件是空的，请重新选择。";
  return "";
}
