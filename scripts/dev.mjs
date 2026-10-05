import { spawn, spawnSync } from "node:child_process";
import { randomUUID } from "node:crypto";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { delimiter, dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const dataDir = resolve(root, process.env.BINGGAN_USER_DATA_DIR || ".local/user-data");
mkdirSync(dataDir, { recursive: true, mode: 0o700 });
const identityFile = resolve(dataDir, ".workspace-id");
const identity = existsSync(identityFile)
  ? readFileSync(identityFile, "utf8").trim()
  : randomUUID().replaceAll("-", "");
if (!/^[a-f0-9]{32}$/.test(identity)) {
  console.error("Invalid development workspace identity. Select another data directory.");
  process.exit(1);
}
if (!existsSync(identityFile)) writeFileSync(identityFile, identity + "\n", { mode: 0o600 });

const devEnv = {
  ...process.env,
  BINGGAN_USER_DATA_DIR: dataDir,
  BINGGAN_RECEIVER_PORT: process.env.BINGGAN_RECEIVER_PORT || "18788",
  CAPTURE_PROXY_PORT: process.env.CAPTURE_PROXY_PORT || "18789",
  BINGGAN_DEV_IDENTIFIER: "time.binggan.desktop.workspace" + identity,
};
// MyGo 0.2.5 disables cgo in child builds. The macOS AX driver needs it.
if (process.platform === "darwin") {
  const probe = spawnSync("go", ["env", "GOROOT"], { encoding: "utf8" });
  if (probe.status !== 0) {
    console.error(probe.stderr || "Go is required. Install the version in go.mod.");
    process.exit(1);
  }
  const realGo = resolve(probe.stdout.trim(), "bin/go");
  const wrapperDir = resolve(root, ".local/toolchain");
  mkdirSync(wrapperDir, { recursive: true });
  const quote = (value) => "'" + value.replaceAll("'", "'\\''") + "'";
  writeFileSync(
    resolve(wrapperDir, "go"),
    `#!/bin/sh\nexec env CGO_ENABLED=1 ${quote(realGo)} "$@"\n`,
    { mode: 0o755 },
  );
  devEnv.PATH = wrapperDir + delimiter + (process.env.PATH || "");
}

const child = spawn(
  process.execPath,
  [resolve(root, "node_modules/mygo-cli/bin/mygo.js"), "dev", ...process.argv.slice(2)],
  {
    cwd: root,
    stdio: "inherit",
    env: devEnv,
  },
);
child.on("error", (error) => {
  console.error(error.message);
  process.exitCode = 1;
});
child.on("exit", (code, signal) => {
  process.exitCode = code ?? (signal ? 1 : 0);
});
for (const signal of ["SIGINT", "SIGTERM"]) {
  process.on(signal, () => child.kill(signal));
}
