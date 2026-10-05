import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { join } from "node:path";
import { computed } from "vue";
import { LOCALE_KEY, locale, readLocale, setLocale, t } from "../src/i18n";
import en from "../src/i18n/en.json";
import { formatDateTime } from "../src/lib/datetime";

test("language preference falls back safely when storage is absent or blocked", () => {
  assert.equal(readLocale(), "zh-CN");
  assert.equal(readLocale({ getItem: () => "fr" }), "zh-CN");
  assert.equal(
    readLocale({ getItem: (key) => (key === LOCALE_KEY ? "en" : null) }),
    "en",
  );
  assert.equal(
    readLocale({
      getItem: () => {
        throw new Error("Blocked storage");
      },
    }),
    "zh-CN",
  );
});

test("switching languages updates existing computed labels and preserves raw data", () => {
  const original = locale.value;
  const label = computed(() => t("共 {count} 条", { count: 20 }));
  try {
    setLocale("zh-CN");
    assert.equal(label.value, "共 20 条");
    setLocale("en");
    assert.equal(label.value, "Total: 20");
    assert.equal(t("操作"), "Actions");
    assert.equal(t("课程 1.sz"), "课程 1.sz");
    assert.equal(t("[ERROR] 未知诊断信息"), "[ERROR] 未知诊断信息");
    assert.equal(formatDateTime(null), t("暂无"));
    assert.match(
      formatDateTime(new Date(2026, 8, 23, 17, 0, 1)),
      /^2026-09-23 17:00:01$/,
    );
    setLocale("invalid");
    assert.equal(locale.value, "en");
    setLocale("zh-CN");
    assert.equal(label.value, "共 20 条");
  } finally {
    setLocale(original);
  }
});

test("all messages preserve interpolation arguments and compile in both languages", () => {
  const original = locale.value;
  const argumentsIn = (value: string) =>
    [...value.matchAll(/\{(\w+)\}/g)].map((m) => m[1]!).sort();
  try {
    for (const [key, value] of Object.entries(en)) {
      assert.ok(value.trim(), key);
      assert.deepEqual(argumentsIn(value), argumentsIn(key), key);
      const params = Object.fromEntries(
        argumentsIn(key).map((name) => [name, "7"]),
      );
      setLocale("en");
      assert.equal(t(key, params), value.replace(/\{\w+\}/g, "7"), key);
      setLocale("zh-CN");
      assert.equal(t(key, params), key.replace(/\{\w+\}/g, "7"), key);
    }
  } finally {
    setLocale(original);
  }
});

test("literal UI translation keys have an English message", () => {
  const missing: string[] = [];
  function scan(dir: string) {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      const path = join(dir, entry.name);
      if (entry.isDirectory()) scan(path);
      else if (/\.(vue|ts)$/.test(entry.name)) {
        for (const match of readFileSync(path, "utf8").matchAll(
          /\bt\(\s*(["'])([^"'\n]*[\p{Script=Han}][^"'\n]*)\1/gu,
        )) {
          if (!Object.hasOwn(en, match[2]!))
            missing.push(`${path}: ${match[2]}`);
        }
      }
    }
  }
  scan(fileURLToPath(new URL("../src", import.meta.url)));
  assert.deepEqual(missing, []);
});
