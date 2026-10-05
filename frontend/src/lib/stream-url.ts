import { isMyGo } from "mygo-runtime";

/** Resolve an app-owned file URL through MyGo Protocol. */
export function streamURL(path: string): string {
  if (!path.startsWith("/api/")) throw new Error("无效的文件地址");
  return isMyGo() ? `binggan-stream://localhost${path}` : path;
}
