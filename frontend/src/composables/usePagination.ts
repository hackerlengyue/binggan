import { computed, ref, toValue, watch, type MaybeRefOrGetter } from "vue";

export const PAGE_SIZES = [20, 50, 100] as const;
export const DEFAULT_PAGE_SIZE = PAGE_SIZES[0];

export function usePaginationState(total: MaybeRefOrGetter<number>) {
  const page = ref(1);
  const pageSize = ref<number>(DEFAULT_PAGE_SIZE);
  const pageCount = computed(() =>
    Math.max(1, Math.ceil(toValue(total) / pageSize.value)),
  );
  watch(
    pageSize,
    () => {
      page.value = 1;
    },
    { flush: "sync" },
  );
  watch(
    [page, pageCount],
    () => {
      page.value = Math.min(Math.max(1, page.value), pageCount.value);
    },
    { flush: "sync" },
  );
  return { page, pageSize, pageCount };
}

export function usePagination<T>(items: MaybeRefOrGetter<readonly T[]>) {
  const total = computed(() => toValue(items).length);
  const state = usePaginationState(total);
  const rows = computed(() =>
    toValue(items).slice(
      (state.page.value - 1) * state.pageSize.value,
      state.page.value * state.pageSize.value,
    ),
  );
  return { ...state, total, rows };
}
