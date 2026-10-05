import { createI18n } from "vue-i18n";
import { watch } from "vue";
import en from "./en.json";

export const LOCALE_KEY = "szjm.locale";
export const locales = ["zh-CN", "en"] as const;
export type Locale = (typeof locales)[number];
export function resolveLocale(value: unknown): Locale {
  return value === "en" ? "en" : "zh-CN";
}
export function readLocale(storage?: Pick<Storage, "getItem">): Locale {
  try {
    return resolveLocale(storage?.getItem(LOCALE_KEY));
  } catch {
    return "zh-CN";
  }
}
function browserLocale(): Locale {
  try {
    return readLocale(window.localStorage);
  } catch {
    return "zh-CN";
  }
}
// Source-language keys keep existing Chinese copy easy to find in components.
// Resolve the complete key so punctuation and periods are not treated as paths.
export const i18n = createI18n({
  legacy: false,
  locale: browserLocale(),
  fallbackLocale: "zh-CN",
  missingWarn: false,
  fallbackWarn: false,
  messageResolver: (messages, key) =>
    Object.prototype.hasOwnProperty.call(messages, key)
      ? (messages as Record<string, string>)[key]!
      : null,
  messages: {
    "zh-CN": Object.fromEntries(Object.keys(en).map((key) => [key, key])),
    en,
  },
});
export const locale = i18n.global.locale;
export function t(
  key: string | undefined,
  values: Record<string, unknown> = {},
): string {
  if (!key) return "";
  // Unknown text can be a file name or a diagnostic from the backend. Keep it intact.
  if (!Object.prototype.hasOwnProperty.call(en, key)) return key;
  return i18n.global.t(key, values);
}
export function setLocale(value: unknown): void {
  if (!locales.includes(value as Locale)) return;
  locale.value = value as Locale;
  try {
    window.localStorage.setItem(LOCALE_KEY, locale.value);
  } catch {
    // Storage can be disabled. Switching still works for the current page.
  }
}
export function startLocaleSync(): () => void {
  const stop = watch(
    locale,
    (value) => {
      document.documentElement.lang = value;
      document.title = t("饼干大小姐");
    },
    { immediate: true },
  );
  const sync = (event: StorageEvent) => {
    if (event.key === LOCALE_KEY || event.key === null)
      locale.value = resolveLocale(event.newValue);
  };
  window.addEventListener("storage", sync);
  return () => {
    stop();
    window.removeEventListener("storage", sync);
  };
}
