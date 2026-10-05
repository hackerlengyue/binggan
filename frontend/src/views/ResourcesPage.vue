<script setup lang="ts">
import DataTable from "@/components/DataTable.vue";
import { computed, onBeforeUnmount, ref, useTemplateRef, watch } from "vue";
import { Search, List, LayoutGrid, Play, Download, Film } from "@lucide/vue";
import { t, locale } from "@/i18n";
import { formatDateTime } from "@/lib/datetime";
import { streamURL } from "@/lib/stream-url";
import { WorkspaceService } from "@/mygo";
import { withDeadline } from "@/lib/deadline";
import { Button } from "@/components/ui/button";
import {
  InputGroup,
  InputGroupInput,
  InputGroupAddon,
} from "@/components/ui/input-group";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import {
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
} from "@/components/ui/table";
import { Empty, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import ResourcePreview from "@/components/ResourcePreview.vue";
import TableSkeletonRows from "@/components/TableSkeletonRows.vue";
import { Skeleton } from "@/components/ui/skeleton";
import ListPagination from "@/components/ListPagination.vue";
import { usePaginationState } from "@/composables/usePagination";
import { useQueryRows } from "@/composables/useQueryRows";
interface Resource {
  id: string;
  name: string;
  size: number;
  finishedAt: string;
}
const items = ref<Resource[]>([]);
const total = ref(0);
const { page, pageSize } = usePaginationState(total);
const query = ref("");
const search = ref("");
const loading = ref(true);
const error = ref("");
const queryKey = computed(() =>
  new URLSearchParams({
    page: String(page.value),
    pageSize: String(pageSize.value),
    q: query.value.trim(),
  }).toString(),
);
const result = useQueryRows(items, queryKey);
const rows = result.rows;
const pending = computed(
  () => loading.value || (!result.current.value && !error.value),
);
const mode = ref<"grid" | "list">("grid");
try {
  const current = localStorage.getItem("bg.resources.view");
  if (current === "grid" || current === "list") {
    mode.value = current;
  }
} catch {}
function setMode(value: unknown) {
  if (value !== "grid" && value !== "list") return;
  mode.value = value;
  try {
    localStorage.setItem("bg.resources.view", value);
  } catch {}
}
const current = ref<Resource | null>(null);
const playerFrame = useTemplateRef<HTMLIFrameElement>("playerFrame");
function onPlayerMessage(event: MessageEvent) {
  if (
    event.origin === window.location.origin &&
    event.source === playerFrame.value?.contentWindow &&
    event.data?.type === "bg-player-close"
  )
    current.value = null;
}
window.addEventListener("message", onPlayerMessage);
const playerOpen = computed({
  get: () => current.value !== null,
  set: (open: boolean) => {
    if (!open) current.value = null;
  },
});
const thumbnail = (item: Resource) =>
  streamURL(
    `/api/resources/${encodeURIComponent(item.id)}/thumbnail?v=${encodeURIComponent(item.finishedAt)}`,
  );
const download = (id: string) =>
  streamURL(`/api/resources/${encodeURIComponent(id)}/download`);
function sizeText(size: number) {
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
  if (size < 1024 ** 3) return `${(size / 1024 ** 2).toFixed(1)} MB`;
  return `${(size / 1024 ** 3).toFixed(2)} GB`;
}
let timer: ReturnType<typeof setTimeout> | undefined;
let searchTimer: ReturnType<typeof setTimeout> | undefined;
let version = 0;
let disposed = false;
async function load() {
  if (disposed) return;
  const request = ++version;
  clearTimeout(timer);
  loading.value = true;
  error.value = "";
  try {
    const params = new URLSearchParams({
      page: String(page.value),
      pageSize: String(pageSize.value),
      q: search.value,
    });
    const data = await withDeadline(
      WorkspaceService.resources({
        page: page.value,
        pageSize: pageSize.value,
        search: search.value,
      }),
      10000,
    );
    if (disposed || request !== version) return;
    if (
      !Array.isArray(data.items) ||
      !Number.isInteger(data.total) ||
      data.total < 0 ||
      data.items.some(
        (item: Resource) =>
          typeof item.id !== "string" ||
          typeof item.name !== "string" ||
          typeof item.size !== "number" ||
          typeof item.finishedAt !== "string",
      )
    )
      throw new Error(t("资源数据格式无效"));
    if (!result.commit(params.toString(), data.items)) return;
    total.value = data.total;
    error.value = "";
  } catch (e) {
    if (!disposed && request === version && (e as Error).name !== "AbortError")
      error.value =
        (e as Error).name === "TimeoutError"
          ? t("资源读取超时，请重试")
          : t("资源加载失败，请重试");
  } finally {
    if (!disposed && request === version) {
      loading.value = false;
      timer = setTimeout(load, 5000);
    }
  }
}
watch(query, () => {
  error.value = "";
  clearTimeout(searchTimer);
  searchTimer = setTimeout(() => {
    search.value = query.value.trim();
    page.value = 1;
  }, 250);
});
watch([page, pageSize, search], () => void load());
void load();
onBeforeUnmount(() => {
  window.removeEventListener("message", onPlayerMessage);
  disposed = true;
  ++version;
  clearTimeout(timer);
  clearTimeout(searchTimer);
});
</script>
<template>
  <section
    class="flex min-h-80 min-w-0 flex-1 flex-col gap-4"
    :aria-label="t('资源管理')"
  >
    <div class="flex shrink-0 flex-wrap items-center justify-between gap-3">
      <InputGroup class="w-full sm:max-w-sm"
        ><InputGroupAddon><Search /></InputGroupAddon
        ><InputGroupInput
          v-model="query"
          :placeholder="t('搜索视频名称')"
          :aria-label="t('搜索资源')"
          maxlength="500"
      /></InputGroup>
      <ToggleGroup
        type="single"
        :model-value="mode"
        @update:model-value="setMode"
        :spacing="1"
        class="segmented"
        size="sm"
        :aria-label="t('展示方式')"
      >
        <ToggleGroupItem
          value="list"
          :aria-label="t('列表视图')"
          :title="t('列表视图')"
          ><List class="size-4"
        /></ToggleGroupItem>
        <ToggleGroupItem
          value="grid"
          :aria-label="t('卡片视图')"
          :title="t('卡片视图')"
          ><LayoutGrid class="size-4"
        /></ToggleGroupItem>
      </ToggleGroup>
    </div>
    <div
      v-if="error"
      class="flex items-center justify-between gap-3 text-sm text-destructive"
      role="alert"
    >
      <span>{{ error }}</span
      ><Button variant="outline" size="sm" :disabled="loading" @click="load">{{
        t("重试")
      }}</Button>
    </div>
    <div class="data-list-panel">
      <div class="data-list-scroll" :aria-busy="pending">
        <DataTable v-if="mode === 'list'" class="min-w-[560px] table-fixed">
          <TableHeader
            ><TableRow
              ><TableHead>{{ t("视频名称") }}</TableHead
              ><TableHead class="w-24">{{ t("文件大小") }}</TableHead
              ><TableHead class="w-44">{{ t("完成时间") }}</TableHead
              ><TableHead class="w-28 text-center">{{
                t("操作")
              }}</TableHead></TableRow
            ></TableHeader
          >
          <TableBody
            ><TableSkeletonRows v-if="pending && !rows.length" :columns="4" />
            <TableRow v-for="item in rows" :key="item.id"
              ><TableCell
                ><Button
                  variant="link"
                  class="h-auto w-full justify-start gap-2 whitespace-normal p-0 py-1 text-left font-medium text-foreground hover:text-primary hover:no-underline"
                  @click="current = item"
                >
                  <Film class="size-4 shrink-0 text-muted-foreground" /><span
                    class="min-w-0 break-words [overflow-wrap:anywhere]"
                    :title="item.name"
                    >{{ item.name }}</span
                  >
                </Button></TableCell
              ><TableCell class="tabular-nums">{{
                sizeText(item.size)
              }}</TableCell
              ><TableCell class="text-muted-foreground tabular-nums">{{
                formatDateTime(item.finishedAt)
              }}</TableCell
              ><TableCell
                ><div class="flex justify-center gap-1">
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    :aria-label="`${t('播放视频')}：${item.name}`"
                    :title="t('播放视频')"
                    @click="current = item"
                    ><Play
                  /></Button>
                  <Button as-child variant="ghost" size="icon-sm"
                    ><a
                      :href="download(item.id)"
                      :aria-label="`${t('下载视频')}：${item.name}`"
                      :title="t('下载视频')"
                      ><Download /></a
                  ></Button></div></TableCell></TableRow
          ></TableBody>
        </DataTable>
        <div
          v-else-if="pending && !rows.length"
          role="status"
          :aria-label="t('正在读取资源')"
          class="grid grid-cols-[repeat(auto-fill,minmax(min(100%,220px),1fr))] gap-3 p-3"
        >
          <div
            v-for="item in 6"
            :key="item"
            class="overflow-hidden rounded-lg border bg-card"
          >
            <Skeleton class="aspect-video w-full rounded-none" />
            <div class="space-y-3 p-3">
              <Skeleton class="h-4 w-3/4" />
              <Skeleton class="h-3 w-1/2" />
            </div>
          </div>
        </div>
        <div
          v-else-if="rows.length"
          class="grid grid-cols-[repeat(auto-fill,minmax(min(100%,220px),1fr))] items-stretch gap-3 p-3"
        >
          <article
            v-for="item in rows"
            :key="item.id"
            class="flex flex-col overflow-hidden rounded-lg border bg-card"
          >
            <Button
              variant="ghost"
              class="group relative h-auto aspect-video w-full rounded-none bg-muted p-0 hover:bg-muted"
              :aria-label="`${t('播放视频')}：${item.name}`"
              @click="current = item"
            >
              <Film class="size-10 text-muted-foreground/30" /><ResourcePreview
                :src="thumbnail(item)"
              />
              <span
                class="absolute inset-0 flex items-center justify-center bg-black/10 transition-colors group-hover:bg-black/25"
                ><span
                  class="flex size-10 items-center justify-center rounded-full bg-black/55 text-white"
                  ><Play class="size-5" /></span
              ></span>
            </Button>
            <div class="flex flex-1 flex-col gap-2 p-3">
              <h2
                class="break-words text-sm font-medium [overflow-wrap:anywhere]"
                :title="item.name"
              >
                {{ item.name }}
              </h2>
              <div class="mt-auto flex items-center justify-between gap-2">
                <div class="min-w-0 text-xs tabular-nums text-muted-foreground">
                  <p>{{ sizeText(item.size) }}</p>
                  <time class="mt-1 block" :datetime="item.finishedAt">{{
                    formatDateTime(item.finishedAt)
                  }}</time>
                </div>
                <Button as-child variant="ghost" size="icon-sm"
                  ><a
                    :href="download(item.id)"
                    :aria-label="`${t('下载视频')}：${item.name}`"
                    :title="t('下载视频')"
                    ><Download /></a
                ></Button>
              </div>
            </div>
          </article>
        </div>
        <Empty v-if="!pending && !rows.length" class="min-h-40 flex-1"
          ><EmptyHeader
            ><EmptyTitle>{{
              pending
                ? t("正在读取资源")
                : error
                  ? t("资源暂不可用")
                  : search
                    ? t("没有匹配的视频")
                    : t("暂无视频资源")
            }}</EmptyTitle></EmptyHeader
          ></Empty
        >
      </div>
      <ListPagination
        v-model:page="page"
        v-model:page-size="pageSize"
        :total="total"
        :loading="pending"
        :disabled="!result.current.value"
        :total-pending="!result.current.value"
        :label="t('资源分页')"
      />
    </div>
  </section>
  <Dialog v-model:open="playerOpen"
    ><DialogContent
      class="max-w-[min(1100px,calc(100vw-2rem))]! max-h-[calc(100dvh-2rem)] flex flex-col gap-3 p-4"
      :aria-describedby="undefined"
      ><DialogHeader class="pr-8"
        ><DialogTitle
          class="max-h-[15dvh] overflow-y-auto break-words [overflow-wrap:anywhere]"
          >{{ current?.name }}</DialogTitle
        ></DialogHeader
      ><iframe
        ref="playerFrame"
        v-if="current"
        :key="current.id"
        :src="`/player/?id=${encodeURIComponent(current.id)}&v=${encodeURIComponent(current.finishedAt)}&lang=${locale}`"
        :title="t('视频播放器')"
        allow="fullscreen; picture-in-picture"
        allowfullscreen
        class="min-h-0 w-full shrink rounded-lg border-0 bg-black"
        style="
          height: min(65dvh, calc((100vw - 4rem) * 9 / 16));
        " /></DialogContent
  ></Dialog>
</template>
