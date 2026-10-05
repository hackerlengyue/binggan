<script setup lang="ts">
import { computed, ref } from "vue";
import { Copy } from "@lucide/vue";
import { toast } from "vue-sonner";
import { isMyGo } from "mygo-runtime";
import { t } from "@/i18n";
import { copyText } from "@/lib/clipboard";
import { LifecycleService } from "@/mygo";
import { Button } from "@/components/ui/button";
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";

const props = defineProps<{ message: string }>();
const busy = ref(false);
// The raw message stays available under "details"; the headline says what to do.
const cause = computed(() => {
  const message = props.message;
  if (/已被另一个后端使用/.test(message))
    return {
      title: "饼干大小姐已在运行",
      body: "这份数据正在被另一个窗口使用。请先关闭其他的饼干大小姐，再重新启动。",
    };
  if (/端口.*占用|address already in use/i.test(message))
    return {
      title: "本机端口被占用",
      body: "有其他程序占用了软件需要的端口。关闭它之后重新启动即可。",
    };
  if (/安装文件不完整/.test(message))
    return {
      title: "安装文件不完整",
      body: "缺少运行所需的组件，请重新安装软件。",
    };
  return { title: "无法启动", body: "启动时遇到问题，重新启动通常可以解决。" };
});
async function relaunch() {
  if (busy.value) return;
  busy.value = true;
  try {
    if (isMyGo()) await LifecycleService.relaunch();
    else location.reload();
  } finally {
    setTimeout(() => (busy.value = false), 1500);
  }
}
async function leave() {
  if (isMyGo()) await LifecycleService.quit();
}
async function copy() {
  try {
    await copyText(props.message);
    toast.success(t("已复制"));
  } catch {
    toast.error(t("复制失败"));
  }
}
</script>

<template>
  <div
    class="fixed inset-0 z-[150] grid place-items-center bg-background"
    data-window-drag
    role="alert"
  >
    <Empty class="max-w-md border-0">
      <EmptyHeader>
        <EmptyMedia>
          <img
            src="/binggan-logo.png"
            alt=""
            width="72"
            height="72"
            class="size-[72px] rounded-[18px] object-cover opacity-90 shadow-sm"
            draggable="false"
          />
        </EmptyMedia>
        <EmptyTitle class="text-lg">{{ t(cause.title) }}</EmptyTitle>
        <EmptyDescription>{{ t(cause.body) }}</EmptyDescription>
      </EmptyHeader>
      <EmptyContent>
        <div class="flex items-center gap-2">
          <Button :disabled="busy" @click="relaunch">{{
            t("重新启动")
          }}</Button>
          <Button v-if="isMyGo()" variant="outline" @click="leave">{{
            t("退出")
          }}</Button>
        </div>
        <Collapsible class="w-full">
          <CollapsibleTrigger as-child>
            <Button variant="link" size="sm" class="text-muted-foreground">
              {{ t("技术详情") }}
            </Button>
          </CollapsibleTrigger>
          <CollapsibleContent>
            <div class="relative mt-2 rounded-lg border bg-muted/40 text-left">
              <pre
                class="max-h-40 select-text overflow-auto whitespace-pre-wrap break-words px-4 py-3 pr-11 font-mono text-xs leading-5"
                >{{ message }}</pre>
              <Button
                variant="ghost"
                size="icon-xs"
                class="absolute top-2 right-2"
                :aria-label="t('复制')"
                :title="t('复制')"
                @click="copy"
                ><Copy
              /></Button>
            </div>
          </CollapsibleContent>
        </Collapsible>
      </EmptyContent>
    </Empty>
  </div>
</template>
