import type { Course } from "./player-orchestration";

export type CourseTreeNode =
  | {
      kind: "folder";
      key: string;
      label: string;
      hint?: string;
      children: CourseTreeNode[];
      courses: Course[];
    }
  | {
      kind: "lesson";
      key: string;
      label: string;
      course: Course;
    };

export function buildCourseTree(courses: Course[]): CourseTreeNode[] {
  const roots: CourseTreeNode[] = [];
  const folders = new Map<
    string,
    Extract<CourseTreeNode, { kind: "folder" }>
  >();

  for (const course of courses) {
    const parts = course.name
      .split(" / ")
      .map((part) => part.trim())
      .filter(Boolean);
    const sourceParts = course.sourcePath?.startsWith("/")
      ? course.sourcePath.split("/").filter(Boolean)
      : [];
    const sourceRoot =
      sourceParts.length >= parts.length
        ? "/" +
          sourceParts.slice(0, sourceParts.length - parts.length + 1).join("/")
        : "";
    const label = parts.pop() || course.targetText || course.name;
    let children = roots;
    let path = "";

    for (const part of parts) {
      path = path ? `${path} / ${part}` : part;
      const folderKey = `${sourceRoot}:${path}`;
      let folder = folders.get(folderKey);
      if (!folder) {
        folder = {
          kind: "folder",
          key: `folder:${folderKey}`,
          label: part,
          children: [],
          courses: [],
        };
        folders.set(folderKey, folder);
        if (children === roots && sourceRoot) folder.hint = sourceRoot;
        children.push(folder);
      }
      folder.courses.push(course);
      children = folder.children;
    }

    children.push({
      kind: "lesson",
      key: `lesson:${course.id}`,
      label,
      course,
    });
  }

  for (const root of roots) {
    if (
      root.kind === "folder" &&
      roots.filter((other) => other.label === root.label).length < 2
    )
      root.hint = undefined;
  }
  return roots;
}
