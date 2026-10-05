import { onBeforeUnmount, onMounted, ref } from "vue";
import { CaptureService, type CertificateState } from "@/mygo";
import { isMyGo } from "mygo-runtime";

export type CertificateOperation = "install" | "trust" | "regenerate";

export function useCertificateManager() {
  const desktop = isMyGo();
  const certificate = ref<CertificateState | null>(null);
  const error = ref("");
  const checking = ref(false);
  const busy = ref<CertificateOperation | "">("");
  let disposed = false;
  const fault = (e: unknown) =>
    (e instanceof Error ? e.message : String(e)).replace(/^Error:\s*/, "");

  async function readCertificate() {
    checking.value = true;
    try {
      const value = await CaptureService.certificate();
      if (!disposed) certificate.value = value;
    } catch (e) {
      if (!disposed) {
        certificate.value = null;
        error.value = fault(e);
      }
    } finally {
      checking.value = false;
    }
  }

  async function refresh() {
    if (!desktop || disposed || busy.value || checking.value) return;
    error.value = "";
    await readCertificate();
  }

  async function act(action: CertificateOperation) {
    if (
      !desktop ||
      disposed ||
      busy.value ||
      checking.value ||
      !certificate.value
    )
      return false;
    if (action === "install" && certificate.value.installed) return false;
    if (
      action === "trust" &&
      (!certificate.value.installed || certificate.value.trusted)
    )
      return false;
    busy.value = action;
    error.value = "";
    try {
      const value = await CaptureService[action]();
      if (disposed) return false;
      certificate.value = value;
      return true;
    } catch (e) {
      if (!disposed) {
        // An OS operation can finish partly before reporting an error. Read its
        // actual state instead of leaving install/trust buttons out of date.
        await readCertificate();
        if (!disposed) error.value = fault(e);
      }
      return false;
    } finally {
      busy.value = "";
    }
  }

  onMounted(() => {
    void refresh();
  });
  onBeforeUnmount(() => {
    disposed = true;
  });
  return { desktop, certificate, error, checking, busy, refresh, act };
}
