<script setup lang="ts">
import DataTable from "@/components/DataTable.vue";
import { t } from "@/i18n";
import { formatDateTime } from "@/lib/datetime";
import { computed, ref, watch } from "vue";
import {
  RouterLink,
  onBeforeRouteLeave,
  useRoute,
  useRouter,
} from "vue-router";
import {
  ArrowLeft,
  Download,
  UnlockKeyhole,
  Pencil,
  Search,
  Trash2,
} from "@lucide/vue";
import { toast } from "vue-sonner";
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogCancel,
} from "@/components/ui/alert-dialog";
import { Spinner } from "@/components/ui/spinner";
import { Button } from "@/components/ui/button";
import {
  InputGroup,
  InputGroupInput,
  InputGroupAddon,
} from "@/components/ui/input-group";
import ListPagination from "@/components/ListPagination.vue";
import { usePagination } from "@/composables/usePagination";
import {
  Empty,
  EmptyHeader,
  EmptyTitle,
  EmptyDescription,
} from "@/components/ui/empty";

import { Checkbox } from "@/components/ui/checkbox";
import { Badge } from "@/components/ui/badge";
import {
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
} from "@/components/ui/table";
import RecordWorkspace from "@/components/RecordWorkspace.vue";
import TableSkeletonRows from "@/components/TableSkeletonRows.vue";
import { useTraceStore } from "@/stores/trace";
const emit = defineEmits<{ rename: [id: string] }>();
const route = useRoute();
const router = useRouter();
const store = useTraceStore();
const id = computed(() => String(route.params.id || ""));
const entry = computed(() =>
  store.keyHistory.find((item) => item.id === id.value),
);
const search = ref("");
const entries = computed(() =>
  store.keyHistory.filter(
    (item) =>
      !(store.captureEnabled && item.id === store.currentHistoryId) &&
      item.name
        .toLocaleLowerCase()
        .includes(search.value.trim().toLocaleLowerCase()),
  ),
);
const { page, pageSize, rows } = usePagination(entries);
const selectedIds = ref<Set<string>>(new Set());
const pageSelection = computed(() => {
  const count = rows.value.filter((item) =>
    selectedIds.value.has(item.id),
  ).length;
  return count === 0
    ? false
    : count === rows.value.length
      ? true
      : "indeterminate";
});
function selectRow(targetId: string, checked: boolean | "indeterminate") {
  if (checked === true) selectedIds.value.add(targetId);
  else selectedIds.value.delete(targetId);
}
function selectPage(checked: boolean | "indeterminate") {
  for (const item of rows.value) selectRow(item.id, checked);
}
watch([page, pageSize], () => selectedIds.value.clear());
watch(search, () => {
  page.value = 1;
  selectedIds.value.clear();
});
watch(entries, (items) => {
  const available = new Set(items.map((item) => item.id));
  selectedIds.value = new Set(
    [...selectedIds.value].filter((id) => available.has(id)),
  );
});
const deleteOpen = ref(false);
const deleteTargets = ref<{ id: string; name: string }[]>([]);
const deleteError = ref("");
const deleting = ref(false);
onBeforeRouteLeave(() => !deleting.value);
function askDelete(item: { id: string; name: string }) {
  deleteTargets.value = [{ id: item.id, name: item.name }];
  deleteError.value = "";
  deleteOpen.value = true;
}
function askDeleteSelected() {
  deleteTargets.value = entries.value
    .filter((item) => selectedIds.value.has(item.id))
    .map(({ id, name }) => ({ id, name }));
  if (!deleteTargets.value.length) return;
  deleteError.value = "";
  deleteOpen.value = true;
}
async function confirmDelete() {
  if (deleting.value || !deleteTargets.value.length) return;
  deleting.value = true;
  deleteError.value = "";
  const targets = [...deleteTargets.value];
  try {
    store.deleteHistories(targets.map((item) => item.id));
    await store.flushKeyHistory();
    for (const target of targets) selectedIds.value.delete(target.id);
    deleteOpen.value = false;
    const removedDetail = targets.some((target) => target.id === id.value);
    deleting.value = false;
    if (removedDetail) await router.replace("/history");
    toast.success(
      targets.length > 1
        ? t("已删除 {count} 条记录", { count: targets.length })
        : t("记录已删除"),
    );
  } catch (error) {
    deleteError.value = (error as Error).message;
  } finally {
    deleting.value = false;
  }
}
function date(value: string) {
  return formatDateTime(value);
}
function download(id: string) {
  try {
    store.downloadHistory(id);
  } catch (error) {
    toast.error(t((error as Error).message));
  }
}
</script>

<template>
  <section
    class="flex min-h-80 flex-1 flex-col gap-4 md:gap-6"
    :aria-label="t('秘钥管理')"
  >
    <template v-if="id">
      <div class="flex shrink-0 flex-wrap items-center justify-between gap-4">
        <div
          class="flex min-w-0 flex-1 basis-full items-start gap-3 sm:basis-0"
        >
          <Button
            as-child
            variant="outline"
            size="icon"
            class="shrink-0"
            :aria-label="t('返回秘钥管理')"
            ><RouterLink to="/history"><ArrowLeft /></RouterLink
          ></Button>
          <div class="min-w-0">
            <h2 class="break-words text-base font-semibold">
              {{ entry?.name ?? t("找不到这条记录") }}
            </h2>
            <p
              v-if="entry"
              class="mt-1.5 text-sm tabular-nums text-muted-foreground"
            >
              {{ date(entry.createdAt) }}
            </p>
          </div>
        </div>
        <div v-if="entry" class="flex shrink-0 items-center gap-2">
          <Button
            :disabled="!entry.valid"
            @click="router.push({ path: '/decrypt', query: { key: entry.id } })"
            ><UnlockKeyhole />{{ t("解密视频") }}</Button
          >
          <Button variant="outline" @click="emit('rename', entry.id)"
            ><Pencil />{{ t("改名") }}</Button
          >
          <Button
            variant="outline"
            :disabled="!entry.valid"
            :title="entry.valid ? undefined : entry.issues.join('；')"
            @click="download(entry.id)"
            ><Download />{{ t("下载 JSON") }}</Button
          >
          <Button
            variant="outline"
            class="text-destructive hover:text-destructive"
            :disabled="
              !!store.busy ||
              (store.captureEnabled && entry.id === store.currentHistoryId)
            "
            @click="askDelete(entry)"
            ><Trash2 />{{ t("删除") }}</Button
          >
        </div>
      </div>
      <RecordWorkspace
        v-if="entry"
        :key="entry.id"
        :payload="entry.keyJson"
        :traces="entry.traces"
      />
    </template>
    <template v-else>
      <div class="flex shrink-0 flex-wrap items-center justify-between gap-4">
        <InputGroup class="w-full sm:w-64">
          <InputGroupInput
            v-model="search"
            :placeholder="t('搜索记录名称')"
            :aria-label="t('搜索记录名称')"
          />
          <InputGroupAddon><Search /></InputGroupAddon>
        </InputGroup>
        <div v-if="selectedIds.size" class="flex flex-wrap items-center gap-2">
          <span class="text-sm text-muted-foreground" role="status">{{
            t("已选 {count} 条", { count: selectedIds.size })
          }}</span>
          <Button variant="ghost" size="sm" @click="selectedIds.clear()">{{
            t("取消选择")
          }}</Button>
          <Button
            variant="destructive"
            size="sm"
            :disabled="!!store.busy"
            @click="askDeleteSelected"
          >
            <Trash2 />{{ t("删除所选") }}</Button
          >
        </div>
      </div>
      <div class="flex min-h-0 flex-1 flex-col gap-4">
        <div class="data-list-panel">
          <div
            class="data-list-scroll"
            :aria-busy="
              !store.historyReady && !store.historyError && !entries.length
            "
          >
            <DataTable class="min-w-[700px] table-fixed"
              ><TableHeader
                ><TableRow>
                  <TableHead class="w-10">
                    <Checkbox
                      :model-value="pageSelection"
                      :disabled="!rows.length"
                      :aria-label="t('全选当前页')"
                      @update:model-value="selectPage"
                    />
                  </TableHead>
                  <TableHead>{{ t("名称") }}</TableHead>
                  <TableHead class="w-16 text-center">{{
                    t("请求数")
                  }}</TableHead>
                  <TableHead class="w-16 text-center">{{
                    t("密码数")
                  }}</TableHead>
                  <TableHead class="w-44">{{ t("提取时间") }}</TableHead>
                  <TableHead class="w-32 text-center">{{
                    t("操作")
                  }}</TableHead>
                </TableRow></TableHeader
              ><TableBody>
                <TableSkeletonRows
                  v-if="
                    !store.historyReady &&
                    !store.historyError &&
                    !entries.length
                  "
                  :columns="6"
                  selection
                />
                <TableRow
                  v-for="item in rows"
                  :key="item.id"
                  :data-state="
                    selectedIds.has(item.id) ? 'selected' : undefined
                  "
                >
                  <TableCell>
                    <Checkbox
                      :model-value="selectedIds.has(item.id)"
                      :aria-label="t('选择 {name}', { name: item.name })"
                      @update:model-value="selectRow(item.id, $event)"
                    />
                  </TableCell>
                  <TableCell>
                    <div class="flex items-center gap-2">
                      <RouterLink
                        :to="`/history/${encodeURIComponent(item.id)}`"
                        class="min-w-0 break-words whitespace-normal font-medium [overflow-wrap:anywhere] underline-offset-4 hover:underline"
                        :title="item.name"
                        >{{ item.name }}</RouterLink
                      >
                      <Badge
                        v-if="!item.valid"
                        variant="destructive"
                        class="shrink-0"
                        >{{
                          Object.keys(item.keyJson.passwords).length
                            ? t("密钥不完整")
                            : t("无密码")
                        }}</Badge
                      >
                    </div>
                  </TableCell>
                  <TableCell class="text-center tabular-nums">{{
                    item.traces?.length ?? t("未保存")
                  }}</TableCell>
                  <TableCell class="text-center tabular-nums">{{
                    Object.keys(item.keyJson.passwords).length
                  }}</TableCell>
                  <TableCell
                    class="whitespace-nowrap text-muted-foreground tabular-nums"
                    >{{ date(item.createdAt) }}</TableCell
                  >
                  <TableCell>
                    <div class="flex items-center justify-center gap-1">
                      <Button
                        variant="ghost"
                        size="icon"
                        class="size-8"
                        :disabled="!!store.busy"
                        :aria-label="t('改名 {name}', { name: item.name })"
                        :title="t('改名')"
                        @click="emit('rename', item.id)"
                        ><Pencil
                      /></Button>
                      <Button
                        class="size-8"
                        variant="ghost"
                        size="icon"
                        :disabled="!item.valid"
                        :aria-label="t('下载 {name}', { name: item.name })"
                        :title="t('下载 JSON')"
                        @click="download(item.id)"
                        ><Download
                      /></Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        class="size-8"
                        :disabled="!!store.busy"
                        :aria-label="t('删除 {name}', { name: item.name })"
                        :title="t('删除记录')"
                        @click="askDelete(item)"
                        ><Trash2
                      /></Button>
                    </div>
                  </TableCell>
                </TableRow> </TableBody
            ></DataTable>
            <Empty
              v-if="(store.historyReady || store.historyError) && !rows.length"
              class="min-h-40 flex-1"
            >
              <EmptyHeader>
                <EmptyTitle>{{
                  store.historyError
                    ? t(store.historyError)
                    : search
                      ? t("没有匹配的记录")
                      : t("还没有提取记录")
                }}</EmptyTitle>
                <EmptyDescription v-if="!store.historyError">{{
                  search
                    ? t("试试其他名称。")
                    : t("手动停止提取后，本次内容会保存在这里。")
                }}</EmptyDescription>
              </EmptyHeader>
            </Empty>
          </div>
          <ListPagination
            v-model:page="page"
            v-model:page-size="pageSize"
            :total="entries.length"
            :loading="
              !store.historyReady && !store.historyError && !entries.length
            "
            :total-pending="
              !store.historyReady && !store.historyError && !entries.length
            "
            :disabled="
              !store.historyReady && !store.historyError && !entries.length
            "
            :label="t('记录分页')"
          />
        </div>
      </div>
    </template>
  </section>
  <AlertDialog
    :open="deleteOpen"
    @update:open="!deleting && (deleteOpen = $event)"
  >
    <AlertDialogContent>
      <AlertDialogHeader>
        <AlertDialogTitle>{{
          deleteTargets.length > 1
            ? t("删除所选的 {count} 条记录？", { count: deleteTargets.length })
            : t("删除这条记录？")
        }}</AlertDialogTitle>
        <AlertDialogDescription class="break-words">{{
          t("{name}的请求和密码将从秘钥管理中删除，无法恢复。", {
            name:
              deleteTargets.length > 1
                ? t("所选 {count} 条记录", { count: deleteTargets.length })
                : `“${deleteTargets[0]?.name ?? ""}”`,
          })
        }}</AlertDialogDescription>
      </AlertDialogHeader>
      <p v-if="deleteError" class="text-sm text-destructive" role="alert">
        {{ t(deleteError) }}
      </p>
      <AlertDialogFooter>
        <AlertDialogCancel :disabled="deleting">{{
          t("取消")
        }}</AlertDialogCancel>
        <Button
          variant="destructive"
          :disabled="deleting || !!store.busy"
          :aria-busy="deleting"
          :aria-label="deleting ? t('正在删除') : undefined"
          @click="confirmDelete"
          ><Spinner v-if="deleting" />{{ t("确认删除") }}</Button
        >
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>
</template>
