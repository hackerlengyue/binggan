import { onBeforeUnmount, ref } from "vue";

export function useLeaveConfirmation() {
  const open = ref(false);
  let pending: Promise<boolean> | undefined;
  let settle: ((leave: boolean) => void) | undefined;
  function resolve(leave: boolean) {
    open.value = false;
    settle?.(leave);
    pending = undefined;
    settle = undefined;
  }
  function request() {
    if (!pending) {
      pending = new Promise<boolean>((done) => {
        settle = done;
      });
      open.value = true;
    }
    return pending;
  }
  onBeforeUnmount(() => resolve(false));
  return { open, request, resolve };
}
