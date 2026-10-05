import { WorkspaceService } from "@/mygo";
import { isMyGo } from "mygo-runtime";

export async function copyText(value: string): Promise<void> {
  if (isMyGo()) {
    await WorkspaceService.copyText(value);
    return;
  }
  await navigator.clipboard.writeText(value);
}
