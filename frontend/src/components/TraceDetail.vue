<script setup lang="ts">
import DataTable from "@/components/DataTable.vue";
import { t } from "@/i18n";
import { computed, ref } from "vue";
import { Copy, AlignLeft, Braces } from "@lucide/vue";
import { toast } from "vue-sonner";
import { Button } from "@/components/ui/button";
import { Toggle } from "@/components/ui/toggle";
import { Empty, EmptyDescription } from "@/components/ui/empty";
import { Badge } from "@/components/ui/badge";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import {
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import type { TrafficRow } from "@/types/trace";
import { formatTime } from "@/lib/traffic";
import { copyText } from "@/lib/clipboard";
import { highlightJson, type JsonTokenKind } from "@/lib/json-highlight";
const props = defineProps<{ entry: TrafficRow }>();
const partName = ref("response");
const view = ref("body");
const pretty = ref(true);
const part = computed(() =>
  partName.value === "request" ? props.entry?.request : props.entry?.response,
);
const headers = computed(() => Object.entries(part.value?.headers ?? {}));
const formattedBody = computed(() => highlightJson(part.value?.body ?? ""));
const tokenColors: Record<JsonTokenKind, string> = {
  key: "text-info",
  string: "text-success",
  number: "text-warning",
  boolean: "text-destructive",
  null: "text-destructive",
  plain: "",
};
const contentToCopy = computed(() =>
  view.value === "headers"
    ? headers.value
        .map(
          ([key, value]) =>
            `${key}: ${Array.isArray(value) ? value.join(", ") : value}`,
        )
        .join("\n")
    : (part.value?.body ?? ""),
);
async function copy(value: string) {
  try {
    await copyText(value);
    toast.success(t("已复制"));
  } catch {
    toast.error(t("复制失败"));
  }
}
</script>

<template>
  <section
    v-if="props.entry"
    class="flex min-h-0 min-w-0 flex-1 flex-col"
    :aria-label="t('请求详情')"
  >
    <div class="shrink-0 space-y-3 border-b px-4 py-4 sm:px-5">
      <div class="flex flex-wrap items-center gap-2">
        <Badge variant="outline" class="font-mono">{{
          props.entry.method
        }}</Badge>
        <Badge
          :variant="props.entry.status >= 400 ? 'destructive' : 'secondary'"
          >{{ props.entry.status || t("等待响应") }}</Badge
        >
        <span class="ml-auto text-xs tabular-nums text-muted-foreground"
          >{{ formatTime(props.entry.timestamp)
          }}<span v-if="props.entry.durationMs">
            · {{ props.entry.durationMs }} ms</span
          ></span
        >
      </div>
      <div class="flex items-start gap-3">
        <code class="min-w-0 flex-1 break-all text-xs leading-6 sm:text-sm">{{
          props.entry.url
        }}</code
        ><Button
          variant="ghost"
          size="icon-sm"
          :aria-label="t('复制请求地址')"
          :title="t('复制地址')"
          @click="copy(props.entry.url)"
          ><Copy
        /></Button>
      </div>
    </div>
    <div
      class="flex shrink-0 flex-wrap items-center gap-3 border-b px-4 py-3 sm:px-5"
    >
      <ToggleGroup
        type="single"
        size="sm"
        :spacing="1"
        class="segmented"
        :model-value="partName"
        :aria-label="t('请求或响应')"
        @update:model-value="
          (value) => {
            if (value === 'response' || value === 'request') partName = value;
          }
        "
      >
        <ToggleGroupItem value="response">{{ t("响应") }}</ToggleGroupItem>
        <ToggleGroupItem value="request">{{ t("请求") }}</ToggleGroupItem>
      </ToggleGroup>
      <ToggleGroup
        type="single"
        size="sm"
        :spacing="1"
        class="segmented"
        :model-value="view"
        :aria-label="t('内容类型')"
        @update:model-value="
          (value) => {
            if (value === 'body' || value === 'headers') view = value;
          }
        "
      >
        <ToggleGroupItem value="body">{{ t("正文") }}</ToggleGroupItem>
        <ToggleGroupItem value="headers"
          >Headers
          <span class="text-muted-foreground">{{
            headers.length
          }}</span></ToggleGroupItem
        >
      </ToggleGroup>
      <div class="ml-auto flex items-center gap-1">
        <Toggle
          v-if="view === 'body' && part?.body"
          v-model="pretty"
          size="sm"
          :aria-label="t('格式化 JSON')"
          :title="pretty ? t('查看原始正文') : t('格式化 JSON')"
          ><Braces v-if="pretty" /><AlignLeft v-else /></Toggle
        ><Button
          variant="outline"
          size="sm"
          :disabled="!contentToCopy"
          @click="copy(contentToCopy)"
          ><Copy />{{ t("复制") }}</Button
        >
      </div>
    </div>
    <div
      :key="`${props.entry.id}-${partName}-${view}`"
      class="min-h-64 max-h-[65svh] flex-1 lg:min-h-0 lg:max-h-none"
      :class="
        view === 'headers' ? 'flex flex-col overflow-hidden' : 'overflow-auto'
      "
    >
      <template v-if="view === 'body'">
        <div
          v-if="
            partName === 'request'
              ? props.entry.requestTruncated
              : props.entry.responseTruncated
          "
          class="border-b bg-muted px-5 py-2 text-xs text-muted-foreground"
        >
          {{ t("正文已截断") }}
        </div>
        <pre
          v-if="part?.body"
          class="min-h-full p-4 font-mono text-xs leading-7 whitespace-pre-wrap break-all sm:p-5 sm:text-sm"
        ><code><template v-if="pretty && formattedBody.tokens"><span
              v-for="(token, index) in formattedBody.tokens"
              :key="index"
              :class="tokenColors[token.kind]"
              >{{ token.text }}</span></template><template v-else>{{ pretty ? formattedBody.text : part.body }}</template></code></pre>
        <Empty v-else class="min-h-64 h-full"
          ><EmptyDescription>
            {{
              partName === "response" && !props.entry.hasResponse
                ? t("等待响应内容")
                : t("正文为空")
            }}
          </EmptyDescription></Empty
        >
      </template>
      <DataTable v-else-if="headers.length" class="table-fixed text-sm"
        ><TableHeader
          ><TableRow
            ><TableHead class="w-1/3">{{ t("名称") }}</TableHead
            ><TableHead>{{ t("值") }}</TableHead></TableRow
          ></TableHeader
        ><TableBody
          ><TableRow v-for="[name, value] in headers" :key="name"
            ><TableCell class="whitespace-normal break-all font-mono text-xs">{{
              name
            }}</TableCell
            ><TableCell class="whitespace-normal break-all font-mono text-xs">{{
              Array.isArray(value) ? value.join(", ") : value
            }}</TableCell></TableRow
          ></TableBody
        ></DataTable
      >
      <Empty v-else class="min-h-64 h-full"
        ><EmptyDescription>{{ t("没有 Header 信息") }}</EmptyDescription></Empty
      >
    </div>
  </section>
</template>
