import { spawnSync } from "node:child_process";
import { delimiter, dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const os = process.platform === "win32" ? "windows" : process.platform;
const arch = process.arch === "x64" ? "amd64" : process.arch;
const tools = resolve(root, "resources", `${os}-${arch}`, "tools");
const env = { ...process.env, PATH: tools + delimiter + (process.env.PATH || "") };

for (const tool of ["ffmpeg", "ffprobe"]) {
  if (spawnSync(tool, ["-version"], { env, stdio: "ignore" }).status !== 0) {
    console.error(`Missing ${tool}. Prepare ${os}-${arch} resources before running tests.`);
    process.exit(1);
  }
}
const go = spawnSync("go", ["test", ".", "./cmd/...", "./internal/..."], {
  cwd: root,
  env,
  stdio: "inherit",
});
if (go.error) console.error(go.error.message);
if (go.status !== 0) process.exit(go.status ?? 1);

if (!process.env.npm_execpath) {
  console.error("Run this script with npm test.");
  process.exit(1);
}
const frontend = spawnSync(
  process.execPath,
  [process.env.npm_execpath, "--prefix", "frontend", "test"],
  { cwd: root, env, stdio: "inherit" },
);
if (frontend.error) console.error(frontend.error.message);
process.exit(frontend.status ?? 1);
