import test from "node:test";
import assert from "node:assert/strict";
import { parseSettings, parseDiagnosis } from "../src/lib/connection-api";
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
