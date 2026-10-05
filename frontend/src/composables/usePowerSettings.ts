import { onBeforeUnmount, onMounted, ref } from "vue";
import { WorkspaceService } from "@/mygo";

export function usePowerSettings() {
  const enabled = ref(false);
  const loaded = ref(false);
  const busy = ref(false);
  const error = ref("");
  let disposed = false;
  async function load() {
    if (disposed || busy.value) return;
    busy.value = true;
    error.value = "";
    try {
      const result = await WorkspaceService.powerSettings();
      if (!disposed) {
        enabled.value = result.keepAwake;
        loaded.value = true;
      }
    } catch (e) {
      if (!disposed) error.value = String((e as Error).message);
    } finally {
      busy.value = false;
    }
  }
  async function save(value: boolean) {
    if (!loaded.value || disposed || busy.value) return;
    busy.value = true;
    error.value = "";
    try {
      const result = await WorkspaceService.savePowerSettings({
        keepAwake: value,
      });
      if (!disposed) enabled.value = result.keepAwake;
    } catch (e) {
      if (!disposed) error.value = String((e as Error).message);
    } finally {
      busy.value = false;
    }
  }
  onMounted(load);
  onBeforeUnmount(() => {
    disposed = true;
  });
  return { enabled, loaded, busy, error, load, save };
}
