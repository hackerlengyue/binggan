<script setup lang="ts">
import DataTable from "@/components/DataTable.vue";
import { t } from "@/i18n";
import TableSkeletonRows from "@/components/TableSkeletonRows.vue";
import { computed, ref, watch } from "vue";
import { Copy, Search, X } from "@lucide/vue";
import { toast } from "vue-sonner";
import { Button } from "@/components/ui/button";
import {
  InputGroup,
  InputGroupInput,
  InputGroupAddon,
  InputGroupButton,
} from "@/components/ui/input-group";
import {
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  TableEmpty,
} from "@/components/ui/table";
import ListPagination from "@/components/ListPagination.vue";
import { usePagination } from "@/composables/usePagination";
import { copyText } from "@/lib/clipboard";
import type { KeyJsonPayload } from "@/types/trace";
const props = defineProps<{ payload: KeyJsonPayload; loading?: boolean }>();
const search = ref("");
const matches = computed(() =>
  Object.entries(props.payload.passwords).filter(([index, value]) =>
    `${index} ${value}`
      .toLowerCase()
      .includes(search.value.trim().toLowerCase()),
  ),
);
const { page, pageSize, rows } = usePagination(matches);
watch(search, () => {
  page.value = 1;
});
const total = computed(() => Object.keys(props.payload.passwords).length);
watch(
  () => props.payload,
  () => {
    if (!total.value) search.value = "";
  },
);
async function copy(value: string) {
  try {
    await copyText(value);
    toast.success(t("已复制密码"));
  } catch {
    toast.error(t("复制失败"));
  }
}
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <div
      class="flex shrink-0 items-center justify-between gap-4 border-b px-4 py-3"
    >
      <InputGroup class="max-w-xs flex-1">
        <InputGroupInput
          v-model="search"
          :aria-label="t('搜索密码')"
          :placeholder="t('搜索密码或序号')"
        />
        <InputGroupAddon><Search /></InputGroupAddon>
        <InputGroupAddon v-if="search" align="inline-end">
          <InputGroupButton
            size="icon-xs"
            :aria-label="t('清除密码搜索')"
            @click="search = ''"
            ><X
          /></InputGroupButton>
        </InputGroupAddon>
      </InputGroup>
      <span
        v-if="search"
        class="shrink-0 text-xs tabular-nums text-muted-foreground"
        >{{ t("{count} 条", { count: `${matches.length} / ${total}` }) }}</span
      >
    </div>
    <div class="data-list-scroll">
      <DataTable
        ><TableHeader
          ><TableRow
            ><TableHead class="w-16">{{ t("序号") }}</TableHead
            ><TableHead>{{ t("密码") }}</TableHead
            ><TableHead class="w-16"
              ><span class="sr-only">{{ t("复制") }}</span></TableHead
            ></TableRow
          ></TableHeader
        ><TableBody>
          <TableSkeletonRows
            v-if="loading && !rows.length"
            :columns="3"
            :rows="5"
          />
          <TableRow v-for="[index, password] in rows" :key="index"
            ><TableCell class="tabular-nums text-muted-foreground">{{
              index.padStart(2, "0")
            }}</TableCell
            ><TableCell class="whitespace-normal font-mono text-sm"
              ><span class="inline-flex flex-wrap gap-x-2 gap-y-1"
                ><span
                  v-for="(chunk, part) in password.match(/.{1,8}/g)"
                  :key="part"
                  >{{ chunk }}</span
                ></span
              ></TableCell
            ><TableCell
              ><Button
                variant="ghost"
                size="icon-sm"
                :aria-label="t('复制第 {index} 条密码', { index: index })"
                :title="t('复制密码')"
                @click="copy(password)"
                ><Copy /></Button></TableCell
          ></TableRow>
          <TableEmpty v-if="!loading && !rows.length" :colspan="3">{{
            t("暂无匹配密码")
          }}</TableEmpty>
        </TableBody></DataTable
      >
    </div>
    <ListPagination
      v-model:page="page"
      v-model:page-size="pageSize"
      :total="matches.length"
      :loading="loading"
      :total-pending="loading && !matches.length"
      :label="t('密码分页')"
    />
  </div>
</template>
