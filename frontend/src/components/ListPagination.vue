<script setup lang="ts">
import { t, locale } from "@/i18n";
import { computed } from "vue";
import { ChevronLeft, ChevronRight } from "@lucide/vue";
import { PAGE_SIZES } from "@/composables/usePagination";
import { Spinner } from "@/components/ui/spinner";
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@/components/ui/select";
import {
  Pagination,
  PaginationContent,
  PaginationItem,
  PaginationEllipsis,
  PaginationPrevious,
  PaginationNext,
} from "@/components/ui/pagination";

withDefaults(
  defineProps<{
    total: number;
    label?: string;
    loading?: boolean;
    totalPending?: boolean;
    disabled?: boolean;
  }>(),
  { label: "列表分页" },
);
const page = defineModel<number>("page", { required: true });
const pageSize = defineModel<number>("pageSize", { required: true });
const sizeValue = computed({
  get: () => String(pageSize.value),
  set: (value: string) => {
    pageSize.value = Number(value);
  },
});
</script>

<template>
  <footer class="min-w-0 shrink-0 border-t px-3 py-2">
    <div class="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
      <div
        class="mr-auto flex min-h-7 items-center gap-2 text-xs tabular-nums text-muted-foreground"
        role="status"
      >
        <span v-if="!totalPending || loading">{{
          totalPending ? t("正在读取") : t("共 {count} 条", { count: total })
        }}</span>
        <Spinner
          v-if="loading"
          class="size-3.5"
          :aria-label="t('正在更新列表')"
        />
        <slot />
      </div>
      <Select :key="locale" v-model="sizeValue" :disabled="disabled">
        <SelectTrigger
          size="sm"
          class="w-28"
          :aria-label="t('{value}每页条数', { value: t(label) })"
          ><SelectValue
        /></SelectTrigger>
        <SelectContent
          position="popper"
          side="top"
          align="start"
          :side-offset="6"
          ><SelectItem
            v-for="size in PAGE_SIZES"
            :key="size"
            :value="String(size)"
            >{{ t("{count} 条 / 页", { count: size }) }}</SelectItem
          ></SelectContent
        >
      </Select>
      <Pagination
        v-model:page="page"
        :total="Math.max(1, total)"
        :items-per-page="pageSize"
        :sibling-count="1"
        :disabled="disabled"
        show-edges
        class="mx-0 w-auto max-w-full max-sm:mx-auto"
        :aria-label="t(label)"
      >
        <PaginationContent v-slot="{ items }" class="flex-wrap">
          <PaginationPrevious :aria-label="t('上一页')" :title="t('上一页')"
            ><ChevronLeft data-icon="inline-start" class="cn-rtl-flip" />
            <span class="hidden sm:block">{{ t("上一页") }}</span>
          </PaginationPrevious>
          <template v-for="(item, index) in items" :key="index">
            <PaginationItem
              v-if="item.type === 'page'"
              :value="item.value"
              :is-active="item.value === page"
              :aria-label="t('第 {item} 页', { item: item.value })"
              >{{ item.value }}</PaginationItem
            >
            <PaginationEllipsis v-else :index="index" />
          </template>
          <PaginationNext :aria-label="t('下一页')" :title="t('下一页')"
            ><span class="hidden sm:block">{{ t("下一页") }}</span>
            <ChevronRight data-icon="inline-end" class="cn-rtl-flip" />
          </PaginationNext>
        </PaginationContent>
      </Pagination>
    </div>
  </footer>
</template>
