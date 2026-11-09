import { spawnSync } from "node:child_process";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const env = { ...process.env };
// MyGo respects CGO_ENABLED; the macOS Accessibility driver requires cgo.
if (process.platform === "darwin") env.CGO_ENABLED = "1";
const child = spawnSync(
  process.execPath,
  [resolve(root, "node_modules/mygo-cli/bin/mygo.js"), "build", ...process.argv.slice(2)],
  { cwd: root, env, stdio: "inherit" },
);
if (child.error) console.error(child.error.message);
process.exitCode = child.status ?? 1;
