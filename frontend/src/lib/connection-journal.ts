import type { ConnectionLog } from "./connection-state";
import { WorkspaceService } from "@/mygo";
import { withDeadline } from "./deadline";
interface Entry extends ConnectionLog {
  id: string;
  source?: "frontend";
}
let db: Promise<IDBDatabase> | undefined;
let writes = Promise.resolve();
let activeFlush: Promise<void> | null = null;
const fallback = new Map<string, Entry>();
function database() {
  return (db ??= new Promise<IDBDatabase>((resolve, reject) => {
    const open = indexedDB.open("shenzao-connection-journal", 1);
    open.onupgradeneeded = () =>
      open.result.createObjectStore("pending", { keyPath: "id" });
    open.onsuccess = () => resolve(open.result);
    open.onerror = () => reject(open.error);
  }));
}
async function pendingOperation<T>(
  mode: IDBTransactionMode,
  operation: (store: IDBObjectStore) => IDBRequest<T>,
): Promise<T> {
  const databaseHandle = await database();
  return new Promise((resolve, reject) => {
    const tx = databaseHandle.transaction("pending", mode);
    const result = operation(tx.objectStore("pending"));
    tx.oncomplete = () => resolve(result.result);
    tx.onerror = () => reject(tx.error);
    tx.onabort = () => reject(tx.error);
  });
}
export function journalConnection(
  entry: ConnectionLog & { source?: "frontend" },
) {
  // Split oversized browser stacks without dropping their contents or blocking a batch.
  const parts = Math.max(1, Math.ceil(entry.message.length / 4000));
  for (let index = 0; index < parts; index++) {
    const value = {
      ...entry,
      id: crypto.randomUUID(),
      message:
        (parts > 1 ? `[${index + 1}/${parts}] ` : "") +
        entry.message.slice(index * 4000, (index + 1) * 4000),
    };
    writes = writes.then(async () => {
      try {
        await pendingOperation("readwrite", (store) => store.put(value));
      } catch {
        fallback.set(value.id, value);
      }
    });
  }
}
export function flushConnectionJournal(): Promise<void> {
  if (activeFlush) return activeFlush;
  activeFlush = (async () => {
    try {
      await writes;
      const queued = (await pendingOperation("readonly", (store) =>
        store.getAll(undefined, 100),
      ).catch(() => [] as Entry[])) as Entry[];
      const entries = [...queued, ...fallback.values()].slice(0, 100);
      if (!entries.length) return;
      await withDeadline(
        WorkspaceService.appendLogs(
          entries.map((entry) => ({
            id: entry.id,
            at: entry.at,
            level: entry.level,
            source: entry.source || "connection",
            message: entry.message,
            detail: "",
          })),
        ),
        8000,
      );
      for (const entry of entries) {
        await pendingOperation("readwrite", (store) =>
          store.delete(entry.id),
        ).catch(() => {});
        fallback.delete(entry.id);
      }
    } catch {
      /* Keep pending logs locally until the receiver returns. */
    }
  })().finally(() => {
    activeFlush = null;
  });
  return activeFlush;
}
export function startConnectionJournal() {
  void flushConnectionJournal();
  const timer = setInterval(() => void flushConnectionJournal(), 3000);
  return () => clearInterval(timer);
}
