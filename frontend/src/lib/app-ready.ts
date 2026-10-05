import { ref } from "vue";

/**
 * True once the splash screen starts leaving. Entrance motion waits for it, so
 * the first page does not animate unseen behind the splash.
 */
export const appReady = ref(false);

export function reducedMotion(): boolean {
  try {
    return window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  } catch {
    return false;
  }
}

export function markAppReady() {
  appReady.value = true;
  document.documentElement.classList.add("app-ready");
}
