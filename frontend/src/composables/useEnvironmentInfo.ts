import { onBeforeUnmount, ref } from "vue";
import { parseDiagnosis, type Diagnosis } from "@/lib/connection-api";
import { WorkspaceService } from "@/mygo";

export function useEnvironmentInfo() {
  const diagnosis = ref<Diagnosis>();
  const error = ref("");
  const testing = ref(false);
  let disposed = false;

  async function refresh() {
    if (testing.value || disposed) return;
    testing.value = true;
    error.value = "";
    try {
      const result = parseDiagnosis(await WorkspaceService.diagnose());
      if (disposed) return;
      diagnosis.value = result;
      return result;
    } catch (e) {
      if (!disposed) error.value = (e as Error).message;
    } finally {
      testing.value = false;
    }
  }

  onBeforeUnmount(() => {
    disposed = true;
  });

  return { diagnosis, error, testing, refresh };
}
