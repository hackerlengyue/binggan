import { t } from "../i18n";

/** Display local time consistently without altering stored timestamps. */
export function formatDateTime(value?: string | number | Date | null): string {
  if (value === undefined || value === null || value === "") return t("暂无");
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return t("暂无");
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
}
