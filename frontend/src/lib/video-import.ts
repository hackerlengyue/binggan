import {
  WorkspaceService,
  type ImportedVideo,
  type LocalVideo,
  type VideoImportProgress,
} from "@/mygo";
import { Channel } from "mygo-runtime";

export const IMPORT_IDLE_TIMEOUT = 60_000;

function canceled() {
  return new DOMException("导入已取消，可继续编辑或重试", "AbortError");
}

export async function importVideo(
  choice: LocalVideo,
  options: { signal: AbortSignal; onProgress?: (percent?: number) => void },
): Promise<ImportedVideo> {
  if (options.signal.aborted) throw canceled();
  let pendingError: DOMException | undefined;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let channel: Channel<VideoImportProgress>;
  const watch = () => {
    clearTimeout(timer);
    timer = setTimeout(() => {
      pendingError = new DOMException(
        "导入长时间没有进展，请重试",
        "TimeoutError",
      );
      channel.close();
    }, IMPORT_IDLE_TIMEOUT);
  };
  channel = new Channel<VideoImportProgress>((progress) => {
    watch();
    if (
      Number.isFinite(progress.copied) &&
      Number.isFinite(progress.total) &&
      progress.total > 0
    ) {
      options.onProgress?.(
        Math.max(
          0,
          Math.min(100, Math.round((progress.copied / progress.total) * 100)),
        ),
      );
    }
  });
  const cancel = () => {
    pendingError = canceled();
    channel.close();
  };
  options.signal.addEventListener("abort", cancel, { once: true });
  if (options.signal.aborted) cancel();
  if (pendingError) {
    options.signal.removeEventListener("abort", cancel);
    throw pendingError;
  }
  watch();
  try {
    const result = await WorkspaceService.importVideo(choice, channel);
    if (pendingError || options.signal.aborted)
      throw pendingError ?? canceled();
    if (
      !result ||
      typeof result.id !== "string" ||
      !result.id ||
      typeof result.name !== "string" ||
      !result.name
    )
      throw new Error("导入响应异常，请重试");
    return result;
  } catch (error) {
    throw pendingError ?? error;
  } finally {
    clearTimeout(timer);
    options.signal.removeEventListener("abort", cancel);
    channel.close();
  }
}
