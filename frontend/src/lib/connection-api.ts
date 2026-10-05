export interface Player {
  softwareName: string;
  appMd5: string;
}
export interface PlayerSettings extends Player {
  mode: "auto" | "manual";
}
export interface Diagnosis {
  checkedAt: string;
  go: string;
  player: Player | null;
  knownPlayers: Record<string, string>;
  checks: { group: string; name: string; ok: boolean; message: string }[];
  environmentAppMd5Configured: boolean;
}
function record(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}
function player(value: unknown): value is Player {
  return (
    record(value) &&
    typeof value.softwareName === "string" &&
    typeof value.appMd5 === "string"
  );
}
export function parseSettings(value: unknown): { settings: PlayerSettings } {
  if (
    !record(value) ||
    !player(value.settings) ||
    !record(value.settings) ||
    !["auto", "manual"].includes(String(value.settings.mode))
  )
    throw new Error("播放器配置响应格式无效");
  return value as unknown as { settings: PlayerSettings };
}
export function parseDiagnosis(value: unknown): Diagnosis {
  if (
    !record(value) ||
    typeof value.checkedAt !== "string" ||
    typeof value.go !== "string" ||
    !Array.isArray(value.checks) ||
    value.checks.some(
      (check) =>
        !record(check) ||
        typeof check.name !== "string" ||
        typeof check.ok !== "boolean" ||
        (check.group !== undefined && typeof check.group !== "string") ||
        typeof check.message !== "string",
    ) ||
    (value.player !== null && !player(value.player)) ||
    !record(value.knownPlayers) ||
    Object.values(value.knownPlayers).some(
      (hash) => typeof hash !== "string",
    ) ||
    typeof value.environmentAppMd5Configured !== "boolean"
  )
    throw new Error("环境检测响应格式无效");
  return value as unknown as Diagnosis;
}
