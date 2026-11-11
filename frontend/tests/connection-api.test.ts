import test from "node:test";
import assert from "node:assert/strict";
import {
  parseSettings,
  parseDiagnosis,
  playerVersionMatches,
} from "../src/lib/connection-api";
import { decryptResponse } from "../src/types/decrypt";
test("settings API rejects malformed success responses instead of reporting saved", async () => {
  for (const value of [null, [], "ok", {}, { settings: { mode: "manual" } }])
    assert.throws(() => parseSettings(value));
  assert.throws(() => parseDiagnosis({ checks: "ready" }));
  for (const value of [null, [], true])
    await assert.rejects(
      () => decryptResponse(Response.json(value)),
      /格式无效/,
    );
  assert.equal(
    parseSettings({ settings: { mode: "auto", softwareName: "", appMd5: "" } })
      .settings.mode,
    "auto",
  );
});

test("Windows protocol labels can use executable hashes without disabling real version checks", () => {
  const hash = "a".repeat(32);
  const local = { softwareName: hash, appMd5: hash };
  assert.equal(playerVersionMatches("ShenZaoLocalPlayer42", local), true);
  assert.equal(playerVersionMatches("b".repeat(32), local), false);
  assert.equal(playerVersionMatches("MAC_sz_other", local), false);
  assert.equal(
    playerVersionMatches("ShenZaoLocalPlayer42", {
      softwareName: "ShenZaoLocalPlayer43",
      appMd5: hash,
    }),
    false,
  );
});

test("environment diagnosis preserves configuration warnings without accepting malformed statuses", () => {
  const check = {
    group: "解密",
    name: "播放器解密参数",
    ok: false,
    warning: true,
    message: "missing verified parameters",
  };
  const diagnosis = {
    checkedAt: "2026-11-11T12:00:00Z",
    go: "fixture",
    player: null,
    knownPlayers: {},
    checks: [check],
    environmentAppMd5Configured: false,
  };
  assert.deepEqual(parseDiagnosis(diagnosis).checks[0], check);
  assert.equal(parseDiagnosis(diagnosis).player, null);
  for (const invalid of [
    { ...check, warning: "true" },
    { ...check, ok: true },
  ])
    assert.throws(() => parseDiagnosis({ ...diagnosis, checks: [invalid] }));
  const failure = { ...check, warning: false };
  assert.deepEqual(
    parseDiagnosis({ ...diagnosis, checks: [failure] }).checks[0],
    failure,
  );
});
