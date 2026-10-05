import { i18n, startLocaleSync } from "@/i18n";
import {
  journalConnection,
  startConnectionJournal,
} from "@/lib/connection-journal";
import { createApp } from "vue";
import { createPinia } from "pinia";
import App from "./App.vue";
import { isMyGo, runtime } from "mygo-runtime";
import { router } from "./router";
import "./style.css";

if (isMyGo())
  document.documentElement.dataset.desktopPlatform = runtime().platform;
const app = createApp(App).use(createPinia()).use(i18n).use(router);
function reportError(error: unknown, context: string) {
  const message =
    error instanceof Error ? error.stack || error.message : String(error);
  journalConnection({
    at: new Date().toISOString(),
    level: "error",
    source: "frontend",
    message: `${context}\n${message}`,
  });
  console.error(error);
}
app.config.errorHandler = (error, _instance, info) => reportError(error, info);
const onUnhandled = (event: PromiseRejectionEvent) =>
  reportError(event.reason, "未处理的异步错误");
const onError = (event: ErrorEvent) =>
  reportError(event.error || event.message, "页面运行错误");
window.addEventListener("unhandledrejection", onUnhandled);
window.addEventListener("error", onError);
// Packaged builds drop the web page context menu; text fields and selections keep
// the native one so copy and paste still work. Development keeps Inspect.
const onContextMenu = (event: MouseEvent) => {
  const target = event.target as HTMLElement | null;
  if (target?.closest("input, textarea, [contenteditable='true']")) return;
  if (window.getSelection()?.toString()) return;
  event.preventDefault();
};
if (import.meta.env.PROD) window.addEventListener("contextmenu", onContextMenu);
const stopLocaleSync = startLocaleSync();
app.mount("#app");

const stopJournal = startConnectionJournal();
if (import.meta.hot)
  import.meta.hot.dispose(() => {
    stopJournal();
    stopLocaleSync();
    window.removeEventListener("contextmenu", onContextMenu);
    window.removeEventListener("unhandledrejection", onUnhandled);
    window.removeEventListener("error", onError);
  });
