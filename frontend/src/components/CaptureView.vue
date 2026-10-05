<script setup lang="ts">
import DataTable from "@/components/DataTable.vue";
import { Spinner } from "@/components/ui/spinner";
import { t } from "@/i18n";
import { computed, ref, watch, onBeforeUnmount } from "vue";
import {
  Radio,
  ChevronLeft,
  ChevronRight,
  RefreshCw,
  ArrowLeft,
} from "@lucide/vue";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
} from "@/components/ui/table";
import ListPagination from "@/components/ListPagination.vue";
import { usePagination } from "@/composables/usePagination";
import {
  Empty,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/skeleton";
import { useTraceStore } from "@/stores/trace";
import { formatTime } from "@/lib/traffic";
import TraceDetail from "./TraceDetail.vue";
const store = useTraceStore();
const props = defineProps<{
  traces: import("@/types/trace").TrafficRow[];
  current?: boolean;
  capturing?: boolean;
  loading?: boolean;
}>();
const selectedId = ref("");
const selected = computed(() =>
  props.traces.find((row) => row.id === selectedId.value),
);
const emit = defineEmits<{ inspect: [open: boolean] }>();
const detailOpen = ref(false);
watch(detailOpen, (open) => emit("inspect", open));
onBeforeUnmount(() => emit("inspect", false));
const detailIds = ref<string[]>([]);
const detailIndex = computed(() => detailIds.value.indexOf(selectedId.value));
function moveDetail(step: number) {
  const id = detailIds.value[detailIndex.value + step];
  if (id) selectedId.value = id;
}
watch(
  () => selected.value,
  (entry) => {
    if (!entry) detailOpen.value = false;
  },
);
const { page, pageSize, rows } = usePagination(() => props.traces);
function select(id: string) {
  detailIds.value = props.traces.map((row) => row.id);
  selectedId.value = id;
  detailOpen.value = true;
}
</script>
<template>
  <div class="flex min-h-0 min-w-0 flex-1 flex-col">
    <template v-if="detailOpen && selected">
      <div
        class="flex shrink-0 items-center justify-between gap-2 border-b px-4 py-2 sm:px-5"
      >
        <Button variant="ghost" size="sm" @click="detailOpen = false"
          ><ArrowLeft />{{ t("返回请求列表") }}</Button
        >
        <div class="flex items-center gap-2">
          <span class="text-xs tabular-nums text-muted-foreground"
            >{{ detailIndex + 1 }} / {{ detailIds.length }}</span
          ><Button
            variant="outline"
            size="icon-sm"
            :aria-label="t('上一条请求')"
            :title="t('上一条请求')"
            :disabled="detailIndex <= 0"
            @click="moveDetail(-1)"
            ><ChevronLeft /></Button
          ><Button
            variant="outline"
            size="icon-sm"
            :aria-label="t('下一条请求')"
            :title="t('下一条请求')"
            :disabled="detailIndex >= detailIds.length - 1"
            @click="moveDetail(1)"
            ><ChevronRight
          /></Button>
        </div>
      </div>
      <TraceDetail :entry="selected" />
    </template>
    <template v-else>
      <div class="min-h-0 flex-1 overflow-hidden">
        <section class="flex h-full min-h-0 min-w-0 flex-col">
          <div class="data-list-scroll">
            <DataTable class="text-sm"
              ><TableHeader
                ><TableRow
                  ><TableHead class="w-24 whitespace-nowrap">{{
                    t("时间")
                  }}</TableHead
                  ><TableHead class="w-20">{{ t("方法") }}</TableHead
                  ><TableHead>{{ t("请求地址") }}</TableHead
                  ><TableHead class="w-20">{{ t("状态") }}</TableHead
                  ><TableHead class="w-24 whitespace-nowrap text-right">{{
                    t("耗时")
                  }}</TableHead></TableRow
                ></TableHeader
              ><TableBody
                ><TableRow
                  v-for="row in rows"
                  :key="row.id"
                  :data-state="selectedId === row.id ? 'selected' : undefined"
                  class="cursor-pointer"
                  @click="select(row.id)"
                  ><TableCell
                    class="w-24 whitespace-nowrap text-foreground tabular-nums"
                    >{{ formatTime(row.timestamp) }}</TableCell
                  ><TableCell
                    ><Badge variant="outline" class="font-mono">{{
                      row.method
                    }}</Badge></TableCell
                  ><TableCell
                    ><Button
                      variant="link"
                      class="block h-auto max-w-48 p-0 text-left whitespace-normal text-foreground sm:max-w-96"
                      :title="row.url"
                      :aria-label="
                        t('查看请求 {value} {value2}', {
                          value: row.method,
                          value2: row.path,
                        })
                      "
                      @click.stop="select(row.id)"
                    >
                      <span class="block truncate font-medium">{{
                        row.path
                          .split("?")[0]
                          ?.split("/")
                          .filter(Boolean)
                          .pop() || row.path
                      }}</span>
                      <span
                        class="block truncate text-xs text-muted-foreground"
                        >{{ row.path }}</span
                      >
                    </Button></TableCell
                  ><TableCell class="w-20"
                    ><span
                      :class="
                        row.status >= 400
                          ? 'text-destructive'
                          : row.status
                            ? 'text-foreground'
                            : 'text-muted-foreground'
                      "
                      >{{ row.status || t("待响应") }}</span
                    ></TableCell
                  ><TableCell class="w-24 whitespace-nowrap text-right">{{
                    row.durationMs ? `${row.durationMs} ms` : "-"
                  }}</TableCell></TableRow
                ></TableBody
              ></DataTable
            >
            <div v-if="loading && !traces.length" class="space-y-3 p-4">
              <Skeleton v-for="i in 7" :key="i" class="h-9 w-full" />
            </div>
            <Empty v-else-if="!rows.length" class="min-h-40 flex-1">
              <EmptyHeader>
                <EmptyMedia variant="icon"><Radio /></EmptyMedia>
                <EmptyTitle>{{
                  capturing ? t("等待请求") : t("暂无请求")
                }}</EmptyTitle>
              </EmptyHeader>
            </Empty>
          </div>
          <ListPagination
            v-model:page="page"
            v-model:page-size="pageSize"
            :total="traces.length"
            :loading="loading"
            :label="t('请求分页')"
          >
            <Button
              v-if="current"
              variant="ghost"
              size="icon-sm"
              :aria-label="t('刷新请求')"
              :title="t('刷新请求')"
              :disabled="store.loading || !!store.busy"
              @click="store.refresh()"
              ><Spinner v-if="store.loading" /><RefreshCw v-else
            /></Button>
          </ListPagination>
        </section>
      </div>
    </template>
  </div>
</template>
