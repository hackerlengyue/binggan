import { execFileSync, spawnSync } from "node:child_process";
import { chmodSync, cpSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const args = ["build", ...process.argv.slice(2)];
let temporary;

function run(command, arguments_, cwd = root) {
  const child = spawnSync(command, arguments_, { cwd, stdio: "inherit" });
  if (child.error) throw child.error;
  if (child.status !== 0) throw new Error(`${command} exited with ${child.status ?? child.signal}`);
}

try {
  if (process.platform !== "win32") {
    run(process.execPath, [join(root, "node_modules/mygo-cli/bin/mygo.js"), ...args]);
  } else {
    // MyGo 0.2.5 writes UTF-8 NSIS scripts without a BOM but does not pass
    // INPUTCHARSET. Build an isolated CLI copy with that argument so Chinese
    // app names and paths work on Windows. Never modify the Go module cache.
    const version = JSON.parse(readFileSync(join(root, "package.json"), "utf8")).devDependencies["mygo-cli"];
    if (version !== "0.2.5") throw new Error("Review the NSIS compatibility patch before changing MyGo CLI versions.");
    const module = JSON.parse(execFileSync("go", ["mod", "download", "-json", `github.com/egoist/mygo@v${version}`], { cwd: root, encoding: "utf8" }));
    if (!module.Dir || module.Error) throw new Error("MyGo CLI source is unavailable.");
    temporary = mkdtempSync(join(tmpdir(), "binggan-mygo-nsis-"));
    const source = join(temporary, "source");
    cpSync(module.Dir, source, { recursive: true });
    const nsis = join(source, "cmd/mygo/nsis.go");
    const contents = readFileSync(nsis, "utf8");
    const original = 'exec.Command(tool, "-V2", nsi)';
    if (contents.split(original).length !== 2) throw new Error("MyGo NSIS source changed; compatibility patch was not applied.");
    chmodSync(nsis, 0o644);
    writeFileSync(nsis, contents.replace(original, 'exec.Command(tool, "-V2", "-INPUTCHARSET", "UTF8", nsi)'));
    const cli = join(temporary, "mygo.exe");
    console.log("[binggan] building MyGo CLI with UTF-8 Windows installer support");
    run("go", ["build", "-o", cli, "./cmd/mygo"], source);
    run(cli, args);
  }
} catch (error) {
  console.error(error.message);
  process.exitCode = 1;
} finally {
  if (temporary) rmSync(temporary, { recursive: true, force: true });
}
