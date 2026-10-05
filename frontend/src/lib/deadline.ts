// A UI deadline for MyGo calls. The Go operation can finish later; callers must
// still ignore stale results after a newer request or component unmount.
export async function withDeadline<T>(
  work: Promise<T>,
  milliseconds: number,
): Promise<T> {
  let timeout: ReturnType<typeof setTimeout> | undefined;
  try {
    return await Promise.race([
      work,
      new Promise<never>((_, reject) => {
        timeout = setTimeout(
          () => reject(new DOMException("操作超时，请重试", "TimeoutError")),
          milliseconds,
        );
      }),
    ]);
  } finally {
    clearTimeout(timeout);
  }
}
