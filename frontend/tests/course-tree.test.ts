import test from "node:test";
import assert from "node:assert/strict";
import { buildCourseTree } from "../src/lib/course-tree";

const courses = [
  { id: "a", name: "一 / 文件甲 / 第一讲" },
  { id: "b", name: "一 / 文件乙 / 第一讲" },
  { id: "c", name: "二 / 第三讲" },
  { id: "d", name: "散课" },
];

test("course tree keeps source order and separates same-named lessons by folder", () => {
  const tree = buildCourseTree(courses);
  assert.deepEqual(
    tree.map((node) => node.label),
    ["一", "二", "散课"],
  );
  const first = tree[0]!;
  assert.equal(first.kind, "folder");
  if (first.kind !== "folder") return;
  assert.deepEqual(
    first.courses.map((course) => course.id),
    ["a", "b"],
  );
  assert.deepEqual(
    first.children.map((node) => node.label),
    ["文件甲", "文件乙"],
  );
  assert.equal(first.children[0]?.kind, "folder");
  const nested = first.children[0]!;
  if (nested.kind !== "folder") return;
  assert.equal(nested.children[0]?.kind, "lesson");
  assert.equal(nested.children[0]?.key, "lesson:a");
});

test("filtered lessons retain their ancestor folders without unrelated siblings", () => {
  const tree = buildCourseTree([courses[1]!]);
  assert.equal(tree.length, 1);
  const first = tree[0]!;
  assert.equal(first.kind, "folder");
  if (first.kind !== "folder") return;
  assert.deepEqual(
    first.children.map((node) => node.label),
    ["文件乙"],
  );
});

test("same-named roots stay separate and display their source locations", () => {
  const tree = buildCourseTree([
    { id: "a", name: "课程 / 第一讲", sourcePath: "/A/课程/第一讲.sz" },
    { id: "b", name: "课程 / 第一讲", sourcePath: "/B/课程/第一讲.sz" },
  ]);
  assert.equal(tree.length, 2);
  assert.notEqual(tree[0]!.key, tree[1]!.key);
  assert.equal(tree[0]!.kind === "folder" && tree[0]!.hint, "/A/课程");
  assert.equal(tree[1]!.kind === "folder" && tree[1]!.hint, "/B/课程");
});
