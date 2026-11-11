import test, { afterEach } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { makeImportedKey, prepareImportedKeys } from "../src/lib/key-import";

const raw = readFileSync(
  new URL(
    "../../internal/engine/testdata/key-legacy-windows.json",
    import.meta.url,
  ),
  "utf8",
);
const originalRuntime = Object.getOwnPropertyDescriptor(globalThis, "mygo");
afterEach(() => {
  if (originalRuntime)
    Object.defineProperty(globalThis, "mygo", originalRuntime);
  else Reflect.deleteProperty(globalThis, "mygo");
});
function mock(call: (method: string, ...args: unknown[]) => Promise<unknown>) {
  Object.defineProperty(globalThis, "mygo", {
    configurable: true,
    value: { call },
  });
}

test("uploaded legacy files retain their verified hash in imported and exported history", async () => {
  mock(async (method, args) => {
    assert.equal(method, "WorkspaceService.PrepareKeys");
    const keys = args as string[];
    assert.equal(keys.length, 1);
    const key = JSON.parse(keys[0]!);
    assert.equal(key.getPwdData.appMd5, undefined);
    key.getPwdData.appMd5 = "a93b82a7f6277f8a30a48f460dcb87c2";
    return [
      { keyJson: JSON.stringify(key), adjusted: true, source: "historical" },
    ];
  });
  const prepared = await prepareImportedKeys([{ name: "1.json", raw }]);
  assert.equal(prepared.adjusted, 1);
  const imported = makeImportedKey(prepared.files[0]!);
  assert.equal(
    imported.keyJson.getPwdData.appMd5,
    "a93b82a7f6277f8a30a48f460dcb87c2",
  );
  const { appMd5, ...data } = imported.keyJson.getPwdData;
  const original = makeImportedKey({ name: "1.json", raw });
  assert.deepEqual(data, original.keyJson.getPwdData);
  assert.deepEqual(imported.keyJson.passwords, original.keyJson.passwords);
  const reimported = makeImportedKey({
    name: "again.json",
    raw: JSON.stringify(imported.keyJson),
  });
  assert.deepEqual(reimported.keyJson, imported.keyJson);
});

test("all files validate before compatibility calls and a failed batch cannot report success", async () => {
  let calls = 0;
  mock(async () => {
    calls++;
    throw new Error("unknown historical checksum");
  });
  await assert.rejects(
    prepareImportedKeys([
      { name: "good.json", raw },
      { name: "bad.json", raw: "{" },
    ]),
    /JSON/u,
  );
  assert.equal(calls, 0);
  await assert.rejects(
    prepareImportedKeys([{ name: "good.json", raw }]),
    /unknown historical checksum/u,
  );
  assert.equal(calls, 1);
});

test("malformed compatibility responses never become imported records", async () => {
  for (const value of [
    null,
    {},
    [],
    [{ keyJson: raw }],
    [{ keyJson: "{", adjusted: true, source: "historical" }],
  ]) {
    mock(async () => value);
    await assert.rejects(prepareImportedKeys([{ name: "1.json", raw }]));
  }
});
