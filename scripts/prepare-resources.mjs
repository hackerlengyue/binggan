import { spawn, execFile } from "node:child_process";
import { createHash } from "node:crypto";
import { createReadStream, createWriteStream } from "node:fs";
import { access, chmod, copyFile, mkdir, mkdtemp, open, readFile, rename, rm, stat } from "node:fs/promises";
import { constants } from "node:fs";
import { dirname, join, posix, resolve } from "node:path";
import { Readable } from "node:stream";
import { pipeline } from "node:stream/promises";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const resources = join(root, "resources");
const cache = join(root, ".cache/resources");
const platforms = ["darwin-arm64", "darwin-amd64", "windows-amd64", "windows-arm64"];
const architectures = {
  "darwin-arm64": "Mach-O arm64",
  "darwin-amd64": "Mach-O x86_64",
  "windows-amd64": "PE32+ x86-64",
  "windows-arm64": "PE32+ Aarch64",
};
const execute = promisify(execFile);

async function digest(path) {
  const hash = createHash("sha256");
  for await (const chunk of createReadStream(path)) hash.update(chunk);
  return hash.digest("hex");
}

async function binaryArchitecture(path) {
  const file = await open(path, "r");
  try {
    const header = Buffer.alloc(64);
    const { bytesRead } = await file.read(header, 0, header.length, 0);
    if (bytesRead >= 8 && header.subarray(0, 4).equals(Buffer.from("cffaedfe", "hex"))) {
      const cpu = header.readUInt32LE(4);
      return { [0x0100000c]: "Mach-O arm64", [0x01000007]: "Mach-O x86_64" }[cpu]
        ?? `Mach-O CPU 0x${cpu.toString(16)}`;
    }
    if (bytesRead >= 64 && header.subarray(0, 2).toString() === "MZ") {
      const pe = Buffer.alloc(26);
      const result = await file.read(pe, 0, pe.length, header.readUInt32LE(0x3c));
      if (result.bytesRead === pe.length && pe.subarray(0, 4).equals(Buffer.from("50450000", "hex"))
        && pe.readUInt16LE(24) === 0x020b) {
        const machine = pe.readUInt16LE(4);
        return { [0x8664]: "PE32+ x86-64", [0xaa64]: "PE32+ Aarch64" }[machine]
          ?? `PE32+ machine 0x${machine.toString(16)}`;
      }
    }
    return "unknown executable format";
  } finally {
    await file.close();
  }
}

async function manifest(platform) {
  const entries = JSON.parse(await readFile(join(resources, `FFMPEG-SOURCES-${platform}.json`), "utf8"));
  const destinations = new Set();
  for (const entry of entries) {
    const parts = typeof entry.destination === "string" ? entry.destination.split("/") : [];
    if (!parts.length || parts.some((part) => !part || part === "." || part === "..")
      || /[\\:]/.test(entry.destination) || posix.isAbsolute(entry.destination)) {
      throw new Error(`Unsafe destination: ${entry.destination}`);
    }
    if (destinations.has(entry.destination)) throw new Error(`Duplicate destination: ${entry.destination}`);
    destinations.add(entry.destination);
    if (new URL(entry.url).protocol !== "https:") throw new Error(`Source must use HTTPS: ${entry.url}`);
    for (const field of ["sha256", "output_sha256"]) {
      if (!/^[a-f0-9]{64}$/.test(entry[field])) throw new Error(`Invalid ${field}`);
    }
    if (!["raw", "zip-file"].includes(entry.format)) throw new Error(`Unsupported source format: ${entry.format}`);
  }
  const extension = platform.startsWith("windows") ? ".exe" : "";
  for (const tool of ["ffmpeg", "ffprobe"]) {
    if (!destinations.has(`tools/${tool}${extension}`)) throw new Error(`Missing ${tool} in manifest`);
  }
  return entries;
}

async function checkedSource(entry) {
  await mkdir(cache, { recursive: true });
  const source = join(cache, entry.sha256);
  try {
    if (await digest(source) === entry.sha256) return source;
  } catch (error) {
    if (error.code !== "ENOENT") throw error;
  }
  const temp = await mkdtemp(join(cache, ".download-"));
  try {
    const staged = join(temp, "source");
    const response = await fetch(entry.url, { signal: AbortSignal.timeout(120_000) });
    if (!response.ok || !response.body) throw new Error(`Download failed (${response.status}): ${entry.url}`);
    await pipeline(Readable.fromWeb(response.body), createWriteStream(staged));
    if (await digest(staged) !== entry.sha256) throw new Error(`Source checksum mismatch: ${entry.url}`);
    await rename(staged, source);
    return source;
  } finally {
    await rm(temp, { recursive: true, force: true });
  }
}

async function extractZip(entry, source, destination) {
  if (process.platform === "win32") {
    // The Windows runtime supplies .NET's ZIP reader; no npm ZIP package is needed.
    const command = `
      $ErrorActionPreference = 'Stop'
      Add-Type -AssemblyName System.IO.Compression.FileSystem
      $archive = [System.IO.Compression.ZipFile]::OpenRead($env:BINGGAN_RESOURCE_ARCHIVE)
      try {
        $entries = @($archive.Entries | Where-Object {
          if ($env:BINGGAN_RESOURCE_MEMBER) { $_.FullName -ceq $env:BINGGAN_RESOURCE_MEMBER }
          else { [System.IO.Path]::GetFileName($_.FullName) -ceq $env:BINGGAN_RESOURCE_NAME }
        })
        if ($entries.Count -ne 1) { throw 'Archive must contain exactly one requested resource' }
        $inputStream = $entries[0].Open()
        try {
          $outputStream = [System.IO.File]::Create($env:BINGGAN_RESOURCE_OUTPUT)
          try { $inputStream.CopyTo($outputStream) } finally { $outputStream.Dispose() }
        } finally { $inputStream.Dispose() }
      } finally { $archive.Dispose() }
    `;
    await execute("powershell.exe", ["-NoProfile", "-NonInteractive", "-Command", command], {
      env: {
        ...process.env,
        BINGGAN_RESOURCE_ARCHIVE: source,
        BINGGAN_RESOURCE_OUTPUT: destination,
        BINGGAN_RESOURCE_MEMBER: entry.member ?? "",
        BINGGAN_RESOURCE_NAME: posix.basename(entry.destination),
      },
    });
    return;
  }
  const { stdout } = await execute("unzip", ["-Z1", source], { maxBuffer: 8 * 1024 * 1024 });
  const matches = stdout.split(/\r?\n/).filter((name) => name && (entry.member
    ? name === entry.member : posix.basename(name) === posix.basename(entry.destination)));
  if (matches.length !== 1 || /[?*\[\]]/.test(matches[0])) {
    throw new Error(`Archive must contain exactly one ${entry.member ?? posix.basename(entry.destination)}`);
  }
  const child = spawn("unzip", ["-p", source, matches[0]], { stdio: ["ignore", "pipe", "inherit"] });
  const completed = new Promise((resolve, reject) => {
    child.once("error", reject);
    child.once("close", (code) => code === 0 ? resolve() : reject(new Error(`unzip exited with ${code}`)));
  });
  await Promise.all([pipeline(child.stdout, createWriteStream(destination)), completed]);
}

async function copyEntry(entry, source, target) {
  const destination = join(target, ...entry.destination.split("/"));
  await mkdir(dirname(destination), { recursive: true });
  const temp = await mkdtemp(join(dirname(destination), ".resource-"));
  try {
    const staged = join(temp, "resource");
    if (entry.format === "raw") await copyFile(source, staged);
    else await extractZip(entry, source, staged);
    if (await digest(staged) !== entry.output_sha256) {
      throw new Error(`Extracted resource checksum mismatch: ${entry.destination}`);
    }
    await chmod(staged, entry.destination.startsWith("tools/") ? 0o755 : 0o644);
    await rename(staged, destination);
  } finally {
    await rm(temp, { recursive: true, force: true });
  }
}

async function verify(platform, entries) {
  const target = join(resources, platform);
  for (const entry of entries) {
    const path = join(target, ...entry.destination.split("/"));
    if (!(await stat(path)).isFile() || await digest(path) !== entry.output_sha256) {
      throw new Error(`Resource checksum mismatch: ${path}`);
    }
  }
  const tools = platform.startsWith("darwin") ? ["ffmpeg", "ffprobe", "capture-service"] : ["ffmpeg", "ffprobe"];
  const extension = platform.startsWith("windows") ? ".exe" : "";
  for (const tool of tools) {
    const path = join(target, "tools", tool + extension);
    if (platform.startsWith("darwin")) await access(path, constants.X_OK);
    const actual = await binaryArchitecture(path);
    if (actual !== architectures[platform]) {
      throw new Error(`Incorrect architecture for ${path}: ${actual}; expected ${architectures[platform]}`);
    }
  }
  console.log(`${platform} resources present, licensed and architecture-checked`);
}

async function prepare(platform, entries) {
  if (platform.startsWith("darwin") && process.platform !== "darwin") {
    throw new Error("macOS resources require a macOS build host");
  }
  const target = join(resources, platform);
  for (const entry of entries) {
    console.log("preparing", platform, entry.destination);
    await copyEntry(entry, await checkedSource(entry), target);
  }
  if (platform.startsWith("darwin")) {
    await execute("go", ["build", "-trimpath", "-ldflags=-s -w", "-o", join(target, "tools/capture-service"), "./cmd/capture-service"], {
      cwd: root,
      env: { ...process.env, GOOS: "darwin", GOARCH: platform.slice("darwin-".length), CGO_ENABLED: "0" },
      maxBuffer: 8 * 1024 * 1024,
    });
  }
  await verify(platform, entries);
}

async function main() {
  let platform = "darwin-arm64";
  let verifyOnly = false;
  const args = process.argv.slice(2);
  for (let i = 0; i < args.length; i++) {
    if (args[i] === "--platform") platform = args[++i];
    else if (args[i] === "--verify") verifyOnly = true;
    else if (["--help", "-h"].includes(args[i])) {
      console.log("Usage: npm run prepare:resources -- [--platform darwin-arm64|darwin-amd64|windows-amd64|windows-arm64|all] [--verify]");
      return;
    } else throw new Error(`Unknown argument: ${args[i]}`);
  }
  if (![...platforms, "all"].includes(platform)) throw new Error(`Unsupported platform: ${platform}`);
  for (const target of platform === "all" ? platforms : [platform]) {
    const entries = await manifest(target);
    if (verifyOnly) await verify(target, entries);
    else await prepare(target, entries);
  }
}

main().catch((error) => {
  console.error(error.message);
  process.exitCode = 1;
});
