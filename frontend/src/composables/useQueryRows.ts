import { computed, ref, toValue, type MaybeRefOrGetter, type Ref } from "vue";

// Keep polling results on screen, but never show them under a different query.
export function useQueryRows<T>(
  items: Ref<T[]>,
  query: MaybeRefOrGetter<string>,
) {
  const resultQuery = ref<string>();
  const current = computed(() => resultQuery.value === toValue(query));
  const rows = computed(() => (current.value ? items.value : []));
  function commit(key: string, values: T[]) {
    if (key !== toValue(query)) return false;
    items.value = values;
    resultQuery.value = key;
    return true;
  }
  return { current, rows, commit };
}
