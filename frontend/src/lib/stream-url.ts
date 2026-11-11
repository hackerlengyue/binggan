import { isMyGo, runtime } from "mygo-runtime";

/** Resolve an app-owned file URL through MyGo Protocol. */
export function streamURL(path: string): string {
  if (!path.startsWith("/api/")) throw new Error("无效的文件地址");
  if (!isMyGo()) return path;
  // WebView2 serves MyGo protocols through virtual HTTP hosts. DOM links,
  // images and media sources must use that host; only native LoadURL and
  // window.open translate custom scheme URLs on Windows.
  const origin =
    runtime().platform === "win32"
      ? "http://binggan-stream.localhost"
      : "binggan-stream://localhost";
  return origin + path;
}
