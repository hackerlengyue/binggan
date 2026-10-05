import { execFileSync } from "node:child_process";
import { lstatSync, readFileSync } from "node:fs";
import { dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const maxBytes = 50 * 1024 * 1024;
const forbiddenDirs = new Set(["node_modules", "dist", "build", ".cache", ".mygo", ".local", ".build", ".swiftpm"]);
const runtimeRoots = new Set(["data", "input", "output", "downloads", "keys", "capture", "logs"]);
const forbiddenSuffixes = new Set([".db", ".sqlite", ".sqlite3", ".log", ".key", ".pem", ".p12", ".pfx", ".dmg", ".pkg", ".exe", ".msi", ".syso"]);
const secretPatterns = [
  /-----BEGIN (?:RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----/,
  /\bgh[pousr]_[A-Za-z0-9]{30,}\b/,
  /\bgithub_pat_[A-Za-z0-9_]{40,}\b/,
  /\bAKIA[0-9A-Z]{16}\b/,
  /\bsk-(?:proj-)?[A-Za-z0-9_-]{40,}\b/,
];
const executableMagic = new Set(["7f454c46", "cffaedfe", "cefaedfe", "feedfacf", "cafebabe"]);

function main() {
  const paths = [...new Set(execFileSync("git", ["ls-files", "-z", "--cached", "--others", "--exclude-standard"], {
    cwd: root, encoding: "utf8", maxBuffer: 8 * 1024 * 1024,
  }).split("\0").filter(Boolean))].sort();
  const issues = [];
  let totalBytes = 0;
  for (const name of paths) {
    const path = resolve(root, name);
    let info;
    try { info = lstatSync(path); } catch (error) {
      if (error.code === "ENOENT") continue;
      throw error;
    }
    if (info.isSymbolicLink()) {
      issues.push([name, "symlink must be reviewed"]);
      continue;
    }
    if (!info.isFile()) {
      issues.push([name, "unexpected non-file entry"]);
      continue;
    }
    const parts = name.split("/");
    const filename = parts.at(-1);
    totalBytes += info.size;
    if (parts.some((part) => forbiddenDirs.has(part)) || runtimeRoots.has(parts[0])) {
      issues.push([name, "generated output, dependency, or runtime data"]);
    }
    if (forbiddenSuffixes.has(extname(name).toLowerCase()) || /\.(?:sqlite3?|db)(?:-(?:wal|shm|journal))?$/.test(name)) {
      issues.push([name, "runtime database, credential, log, or executable"]);
    }
    if (filename.startsWith(".env") && filename !== ".env.example") issues.push([name, "local environment configuration"]);
    if ([".DS_Store", "Thumbs.db", "Desktop.ini"].includes(filename)) issues.push([name, "operating-system metadata"]);
    if (info.size > maxBytes) {
      issues.push([name, "file exceeds repository policy of 50 MiB"]);
      continue;
    }
    const data = readFileSync(path);
    if (executableMagic.has(data.subarray(0, 4).toString("hex")) || data.subarray(0, 2).toString() === "MZ") {
      issues.push([name, "compiled executable"]);
    }
    if (!data.subarray(0, 8192).includes(0)) {
      const text = data.toString("utf8");
      if (secretPatterns.some((pattern) => pattern.test(text))) issues.push([name, "possible credential; value omitted"]);
      if (/\/Users\/(?!test\/|person\/|example\/|shared\/)[^/\s]+\//.test(text)) issues.push([name, "personal absolute path"]);
    }
  }
  if (issues.length) {
    for (const [name, reason] of issues) console.error(`FAIL ${name}: ${reason}`);
    process.exitCode = 1;
  } else {
    console.log(`Repository check passed: ${paths.length} files, ${(totalBytes / 1024 / 1024).toFixed(2)} MiB.`);
  }
}

try { main(); } catch (error) {
  console.error(error.message);
  process.exitCode = 1;
}
