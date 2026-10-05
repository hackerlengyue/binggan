import test from "node:test";
import assert from "node:assert/strict";
import {
  navigationGroups,
  navigationSection,
  routeMatches,
  isWorkspacePath,
} from "../src/lib/navigation";
test("menu groups cover workflow and system routes exactly once", () => {
  const paths = navigationGroups.flatMap((group) =>
    group.items.map((item) => item.url),
  );
  assert.deepEqual(paths, [
    "/capture",
    "/history",
    "/player-orchestration",
    "/decrypt",
    "/resources",
    "/certificates",
    "/environment",
    "/decrypt-settings",
    "/power-settings",
    "/notifications",
    "/logs",
  ]);
  assert.equal(new Set(paths).size, paths.length);
  assert.deepEqual(
    navigationGroups[1]!.items.map((item) => item.title),
    ["证书管理", "环境信息", "解密设置", "电源管理", "通知管理", "日志信息"],
  );
  assert.equal(navigationSection("/history/record-1"), "视频处理");
  assert.equal(navigationSection("/logs"), "系统管理");
  assert.equal(navigationSection("/certificates"), "系统管理");
  assert.equal(navigationSection("/environment"), "系统管理");
  assert.equal(navigationSection("/decrypt-settings"), "系统管理");
  assert.equal(navigationSection("/notifications"), "系统管理");
  assert.equal(navigationSection("/overview"), "概览");
  assert.equal(routeMatches("/history-other", "/history"), false);
});

test("native menu navigation stays within known workspaces", () => {
  assert.equal(isWorkspacePath("/overview"), true);
  for (const group of navigationGroups)
    for (const item of group.items)
      assert.equal(isWorkspacePath(item.url), true);
  for (const value of [
    "https://example.com",
    "//example.com",
    "/api/export",
    "/history/unknown",
    undefined,
    { path: "/logs" },
  ])
    assert.equal(isWorkspacePath(value), false);
});
