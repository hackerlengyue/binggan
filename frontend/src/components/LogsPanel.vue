<script setup lang="ts">
import DataTable from "@/components/DataTable.vue";
import { t, locale } from "@/i18n";
import { formatDateTime } from "@/lib/datetime";
import { streamURL } from "@/lib/stream-url";
import { WorkspaceService } from "@/mygo";
import { withDeadline } from "@/lib/deadline";
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { Download, Search, Trash2 } from "@lucide/vue";
import { toast } from "vue-sonner";
import { Button } from "@/components/ui/button";
import {
  InputGroup,
  InputGroupInput,
  InputGroupAddon,
} from "@/components/ui/input-group";
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@/components/ui/select";
import { Badge } from "@/components/ui/badge";
import { Spinner } from "@/components/ui/spinner";
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogCancel,
} from "@/components/ui/alert-dialog";
import ListPagination from "@/components/ListPagination.vue";
import TableSkeletonRows from "@/components/TableSkeletonRows.vue";
import LogTimeRange from "@/components/LogTimeRange.vue";
import { useQueryRows } from "@/composables/useQueryRows";
import { usePaginationState } from "@/composables/usePagination";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import {
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
  TableEmpty,
} from "@/components/ui/table";
import { flushConnectionJournal } from "@/lib/connection-journal";
import { parseSystemLogs, type SystemLog as LogEntry } from "@/lib/logs";
const level = ref("all"),
  source = ref("all"),
  query = ref(""),
  range = ref("all"),
  from = ref(""),
  to = ref("");
// Presets are resolved when a request is made, so "last hour" keeps moving.
const now = ref(Date.now());
function bounds(at: number): { from: string; to: string } {
  const date = new Date(at);
  if (range.value === "1h")
    return { from: new Date(at - 3600_000).toISOString(), to: "" };
  if (range.value === "7d")
    return { from: new Date(at - 7 * 86400_000).toISOString(), to: "" };
  if (range.value === "today")
    return {
      from: new Date(
        date.getFullYear(),
        date.getMonth(),
        date.getDate(),
      ).toISOString(),
      to: "",
    };
  if (range.value !== "custom") return { from: "", to: "" };
  return {
    from:
      from.value && Number.isFinite(Date.parse(from.value))
        ? new Date(from.value).toISOString()
        : "",
    to:
      to.value && Number.isFinite(Date.parse(to.value))
        ? new Date(to.value).toISOString()
        : "",
  };
}
const total = ref(0),
  items = ref<LogEntry[]>([]),
  loading = ref(false),
  error = ref("");
const { page, pageSize } = usePaginationState(total);
const detail = ref<LogEntry>();
const clearOpen = ref(false),
  clearing = ref(false),
  clearError = ref("");
const sources: Record<string, string> = {
  connection: "连接",
  server: "服务",
  request: "接口",
  decrypt: "解密",
  frontend: "页面",
  capture: "监控器",
  certificate: "证书",
};
const levels: Record<string, string> = {
  info: "信息",
  warn: "警告",
  error: "错误",
};
const params = computed(() => {
  const result = new URLSearchParams();
  if (source.value !== "all") result.set("source", source.value);
  if (level.value !== "all") result.set("level", level.value);
  if (query.value.trim()) result.set("q", query.value.trim());
  // The key names the filter, not the moving window, so polling does not flicker.
  if (range.value !== "all") result.set("range", range.value);
  if (range.value === "custom") {
    if (from.value) result.set("from", from.value);
    if (to.value) result.set("to", to.value);
  }
  return result;
});
const queryKey = computed(
  () => `${params.value}&page=${page.value}&pageSize=${pageSize.value}`,
);
const resultRows = useQueryRows(items, queryKey);
const rows = resultRows.rows;
const pending = computed(
  () => loading.value || (!resultRows.current.value && !error.value),
);
const invalidRange = computed(
  () =>
    range.value === "custom" &&
    !!(from.value && to.value && from.value > to.value),
);
const downloadUrl = computed(() => {
  const result = new URLSearchParams();
  if (source.value !== "all") result.set("source", source.value);
  if (level.value !== "all") result.set("level", level.value);
  if (query.value.trim()) result.set("q", query.value.trim());
  const window = bounds(now.value);
  if (window.from) result.set("from", window.from);
  if (window.to) result.set("to", window.to);
  return streamURL(`/api/logs?${result}&download=1`);
});
let timer: ReturnType<typeof setTimeout> | undefined;
let version = 0,
  disposed = false;
async function load() {
  if (clearing.value || disposed) return;
  clearTimeout(timer);
  const token = ++version;
  loading.value = true;
  error.value = "";
  const key = queryKey.value;
  try {
    if (invalidRange.value) throw new Error(t("结束时间须晚于开始时间"));
    await flushConnectionJournal();
    if (token !== version || disposed) return;
    now.value = Date.now();
    const window = bounds(now.value);
    const result = await withDeadline(
      WorkspaceService.logs({
        level: params.value.get("level") || "",
        source: params.value.get("source") || "",
        search: params.value.get("q") || "",
        from: window.from,
        to: window.to,
        page: page.value,
        pageSize: pageSize.value,
      }),
      10000,
    );
    if (token !== version || disposed) return;
    const parsed = parseSystemLogs(result);
    if (!resultRows.commit(key, parsed.items)) return;
    total.value = parsed.total;
    error.value = "";
  } catch (e) {
    if (token === version && !disposed) error.value = (e as Error).message;
  } finally {
    if (token === version && !disposed) {
      loading.value = false;
      timer = setTimeout(load, 5000);
    }
  }
}
async function clearLogs() {
  if (clearing.value) return;
  clearing.value = true;
  clearError.value = "";
  ++version;
  clearTimeout(timer);
  loading.value = false;
  try {
    await withDeadline(WorkspaceService.clearLogs("all"), 15000);
    if (disposed) return;
    items.value = [];
    total.value = 0;
    page.value = 1;
    detail.value = undefined;
    error.value = "";
    clearOpen.value = false;
    toast.success(t("日志已清空"));
  } catch (e) {
    if (!disposed) clearError.value = (e as Error).message;
  } finally {
    clearing.value = false;
    if (!disposed) void load();
  }
}
watch([level, source, query, range, from, to], () => {
  error.value = "";
  page.value = 1;
  clearTimeout(timer);
  ++version;
  timer = setTimeout(load, 300);
});
watch([page, pageSize], () => {
  clearTimeout(timer);
  ++version;
  timer = setTimeout(load, 300);
});
void load();
onBeforeUnmount(() => {
  disposed = true;
  ++version;
  clearTimeout(timer);
});
</script>
<template>
  <section
    class="flex min-h-80 min-w-0 flex-1 flex-col gap-4"
    :aria-label="t('日志查询')"
  >
    <Teleport to="#workspace-page-actions">
      <Button
        v-if="invalidRange"
        disabled
        variant="outline"
        class="h-7 shrink-0"
        :aria-label="t('下载全部')"
        :title="t('下载全部')"
        ><Download /><span class="hidden @2xl/workspace:inline">{{
          t("下载全部")
        }}</span></Button
      >
      <Button
        v-else
        as-child
        variant="outline"
        class="h-7 shrink-0"
        :aria-label="t('下载全部')"
        :title="t('下载全部')"
        ><a
          :href="downloadUrl"
          :aria-label="t('下载全部')"
          :title="t('下载全部')"
          ><Download /><span class="hidden @2xl/workspace:inline">{{
            t("下载全部")
          }}</span></a
        ></Button
      >
      <Button
        variant="outline"
        class="h-7 shrink-0 text-destructive hover:text-destructive"
        :disabled="clearing"
        :aria-label="t('清空日志')"
        :title="t('清空日志')"
        @click="
          clearError = '';
          clearOpen = true;
        "
        ><Trash2 /><span class="hidden @2xl/workspace:inline">{{
          t("清空日志")
        }}</span></Button
      >
    </Teleport>
    <div
      class="logs-filter-grid grid grid-cols-2 items-center gap-2"
      :aria-label="t('日志筛选')"
    >
      <InputGroup class="logs-search col-span-2 h-7 min-w-0"
        ><InputGroupAddon><Search /></InputGroupAddon
        ><InputGroupInput
          v-model="query"
          :placeholder="t('搜索内容、请求编号')"
          :aria-label="t('搜索日志')"
          maxlength="500"
      /></InputGroup>
      <Select :key="`level-${locale}`" v-model="level"
        ><SelectTrigger class="h-7! w-full min-w-0" :aria-label="t('日志级别')"
          ><SelectValue /></SelectTrigger
        ><SelectContent
          ><SelectItem value="all">{{ t("全部级别") }}</SelectItem
          ><SelectItem value="error">{{ t("错误") }}</SelectItem
          ><SelectItem value="warn">{{ t("警告") }}</SelectItem
          ><SelectItem value="info">{{ t("信息") }}</SelectItem></SelectContent
        ></Select
      >
      <Select :key="`source-${locale}`" v-model="source"
        ><SelectTrigger class="h-7! w-full min-w-0" :aria-label="t('日志来源')"
          ><SelectValue /></SelectTrigger
        ><SelectContent
          ><SelectItem value="all">{{ t("全部来源") }}</SelectItem
          ><SelectItem
            v-for="(label, value) in sources"
            :key="value"
            :value="value"
            >{{ t(label) }}</SelectItem
          >
        </SelectContent></Select
      >
      <LogTimeRange
        v-model:range="range"
        v-model:from="from"
        v-model:to="to"
        class="logs-time-range"
      />
    </div>
    <div
      v-if="error"
      role="alert"
      class="flex items-center justify-between gap-3 text-sm text-destructive"
    >
      <span>{{ t(error) }}</span
      ><Button
        v-if="!invalidRange"
        variant="outline"
        size="sm"
        :disabled="loading || clearing"
        @click="load"
        >{{ t("重试") }}</Button
      >
    </div>
    <div class="flex min-h-0 flex-1 flex-col overflow-hidden rounded-lg border">
      <div
        class="flex min-h-0 flex-1 flex-col overflow-hidden"
        :aria-busy="pending && !rows.length"
      >
        <DataTable class="min-w-[720px] table-fixed"
          ><TableHeader
            ><TableRow
              ><TableHead class="w-44">{{ t("时间") }}</TableHead
              ><TableHead class="w-20">{{ t("级别") }}</TableHead
              ><TableHead class="w-20">{{ t("来源") }}</TableHead
              ><TableHead>{{ t("内容") }}</TableHead
              ><TableHead class="w-20 text-right">{{
                t("操作")
              }}</TableHead></TableRow
            ></TableHeader
          >
          <TableBody
            ><TableSkeletonRows v-if="pending && !rows.length" :columns="5" />
            <TableRow
              v-for="entry in rows"
              :key="entry.id"
              class="group"
              :class="entry.level === 'error' ? 'bg-destructive/[0.03]' : ''"
              ><TableCell class="text-xs tabular-nums text-muted-foreground">{{
                formatDateTime(entry.at)
              }}</TableCell
              ><TableCell
                ><Badge
                  :variant="
                    entry.level === 'error' ? 'destructive' : 'secondary'
                  "
                  >{{ t(levels[entry.level] || entry.level) }}</Badge
                ></TableCell
              ><TableCell class="text-xs text-muted-foreground">{{
                t(sources[entry.source] || entry.source)
              }}</TableCell
              ><TableCell class="whitespace-normal"
                ><p class="line-clamp-2 break-words text-sm leading-6">
                  {{ entry.message }}
                </p></TableCell
              ><TableCell class="text-right"
                ><Button
                  variant="ghost"
                  size="sm"
                  class="pr-0"
                  @click="detail = entry"
                  >{{ t("详情") }}</Button
                ></TableCell
              ></TableRow
            >
            <TableEmpty
              v-if="!pending && !rows.length"
              :colspan="5"
              class="h-36 text-center text-muted-foreground"
              >{{ error ? t("日志暂不可用") : t("暂无匹配日志") }}</TableEmpty
            ></TableBody
          >
        </DataTable>
      </div>
      <ListPagination
        v-model:page="page"
        v-model:page-size="pageSize"
        :total="total"
        :loading="pending"
        :total-pending="!resultRows.current.value"
        :disabled="clearing || !resultRows.current.value"
        :label="t('日志分页')"
      />
    </div>
    <AlertDialog
      :open="clearOpen"
      @update:open="!clearing && (clearOpen = $event)"
      ><AlertDialogContent
        ><AlertDialogHeader
          ><AlertDialogTitle>{{ t("清空全部日志？") }}</AlertDialogTitle
          ><AlertDialogDescription>{{
            t(
              "包括当前筛选之外的日志，清空后无法恢复。解密任务及子任务日志保留。",
            )
          }}</AlertDialogDescription></AlertDialogHeader
        >
        <p v-if="clearError" role="alert" class="text-sm text-destructive">
          {{ t(clearError) }}
        </p>
        <AlertDialogFooter
          ><AlertDialogCancel :disabled="clearing">{{
            t("取消")
          }}</AlertDialogCancel
          ><Button
            variant="destructive"
            :disabled="clearing"
            :aria-busy="clearing"
            :aria-label="clearing ? t('正在清空') : undefined"
            @click="clearLogs"
            ><Spinner v-if="clearing" />{{ t("确认清空") }}</Button
          ></AlertDialogFooter
        ></AlertDialogContent
      ></AlertDialog
    >
    <Dialog :open="!!detail" @update:open="!$event && (detail = undefined)">
      <DialogContent class="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{{ t("日志详情") }}</DialogTitle>
          <DialogDescription
            class="max-h-[60vh] select-text overflow-y-auto whitespace-pre-wrap break-words pt-2 text-sm leading-6 text-foreground"
            >{{ detail?.message }}</DialogDescription
          >
        </DialogHeader>
      </DialogContent>
    </Dialog>
  </section>
</template>

<style scoped>
.logs-time-range {
  grid-column: 1 / -1;
}
@container main (min-width: 540px) {
  .logs-filter-grid {
    grid-template-columns: minmax(192px, 1fr) 112px 112px 168px;
  }
  .logs-search {
    grid-column: auto;
  }
  .logs-time-range {
    grid-column: auto;
  }
}
</style>
