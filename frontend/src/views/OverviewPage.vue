<script setup lang="ts">
import AnimatedNumber from "@/components/AnimatedNumber.vue";
import { t, locale } from "@/i18n";
import { WorkspaceService } from "@/mygo";
import { withDeadline } from "@/lib/deadline";
import { computed, ref, onMounted, onBeforeUnmount } from "vue";
import { useIntervalFn, useNow } from "@vueuse/core";
import { KeyRound, ListTodo, Video, CircleAlert } from "@lucide/vue";
import { CurveType } from "@unovis/ts";
import { VisArea, VisAxis, VisLine, VisXYContainer } from "@unovis/vue";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  ChartContainer,
  ChartCrosshair,
  ChartTooltip,
  ChartTooltipContent,
  componentToString,
  type ChartConfig,
} from "@/components/ui/chart";
import { useTraceStore } from "@/stores/trace";

import { Empty, EmptyDescription } from "@/components/ui/empty";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import OverviewStatusChart from "@/components/OverviewStatusChart.vue";
import { Skeleton } from "@/components/ui/skeleton";
type Counts = {
  total: number;
  active: number;
  completed: number;
  failed: number;
};
const summary = ref<{ tasks: Counts; videos: Counts }>();
const summaryError = ref("");
let loading = false;
let disposed = false;
async function loadSummary() {
  if (loading || disposed) return;
  loading = true;
  try {
    const data = await withDeadline(WorkspaceService.summary(), 8000);
    for (const counts of [data.tasks, data.videos]) {
      if (
        !counts ||
        (["total", "active", "completed", "failed"] as const).some(
          (key) => !Number.isSafeInteger(counts[key]) || counts[key] < 0,
        )
      )
        throw new Error(t("统计数据格式无效"));
    }
    if (!disposed) {
      summary.value = data;
      summaryError.value = "";
    }
  } catch {
    if (!disposed)
      summaryError.value = summary.value
        ? "解密统计更新失败，当前显示上次数据"
        : "解密统计暂不可用";
  } finally {
    loading = false;
  }
}
onMounted(loadSummary);
useIntervalFn(loadSummary, 5000);
onBeforeUnmount(() => {
  disposed = true;
});
const taskChart = computed(() => [
  {
    label: t("已完成"),
    value: summary.value?.tasks.completed ?? 0,
    color: "var(--chart-3)",
  },
  {
    label: t("进行中"),
    value: summary.value?.tasks.active ?? 0,
    color: "color-mix(in srgb, var(--chart-3) 65%, var(--background))",
  },
  {
    label: t("失败"),
    value: summary.value?.tasks.failed ?? 0,
    color: "color-mix(in srgb, var(--chart-3) 35%, var(--background))",
  },
]);
const videoChart = computed(() => [
  {
    label: t("成功"),
    value: summary.value?.videos.completed ?? 0,
    color: "var(--chart-3)",
  },
  {
    label: t("进行中"),
    value: summary.value?.videos.active ?? 0,
    color: "color-mix(in srgb, var(--chart-3) 65%, var(--background))",
  },
  {
    label: t("失败"),
    value: summary.value?.videos.failed ?? 0,
    color: "color-mix(in srgb, var(--chart-3) 35%, var(--background))",
  },
]);
const successRate = computed(() => {
  const videos = summary.value?.videos;
  const ended = videos ? videos.completed + videos.failed : 0;
  return videos && ended
    ? `${((videos.completed / ended) * 100).toFixed(1)}%`
    : "-";
});
const period = ref("14");
const dayCount = computed(() => Number(period.value));
const store = useTraceStore();
const now = useNow({ scheduler: (update) => useIntervalFn(update, 60_000) });
const entries = computed(() =>
  store.keyHistory.filter(
    (entry) => !(store.captureEnabled && entry.id === store.currentHistoryId),
  ),
);
const completeKeys = computed(
  () => entries.value.filter((entry) => entry.valid).length,
);
const numberFormatter = computed(() => new Intl.NumberFormat(locale.value));
const metrics = computed(() => [
  {
    title: t("完整密钥记录"),
    value:
      store.historyReady || entries.value.length
        ? completeKeys.value
        : undefined,
    pending:
      !store.historyReady && !entries.value.length && !store.historyError,
    description:
      !store.historyReady && !entries.value.length
        ? t(store.historyError || "正在读取")
        : t("已保存 {time} · 未完整 {time2}", {
            time: numberFormatter.value.format(entries.value.length),
            time2: numberFormatter.value.format(
              entries.value.length - completeKeys.value,
            ),
          }),
    icon: KeyRound,
  },
  {
    title: t("进行中任务"),
    value: summary.value?.tasks.active,
    pending: !summary.value && !summaryError.value,
    description: summary.value
      ? t("含排队任务 · 共 {time} 个任务", {
          time: numberFormatter.value.format(summary.value.tasks.total),
        })
      : t(summaryError.value ? "统计暂不可用" : "正在读取任务统计"),
    icon: ListTodo,
  },
  {
    title: t("已完成视频"),
    value: summary.value?.videos.completed,
    pending: !summary.value && !summaryError.value,
    description: summary.value
      ? t("共 {time} 个视频 · 处理中 {time2}", {
          time: numberFormatter.value.format(summary.value.videos.total),
          time2: numberFormatter.value.format(summary.value.videos.active),
        })
      : t(summaryError.value ? "统计暂不可用" : "正在读取视频统计"),
    icon: Video,
  },
  {
    title: t("失败视频"),
    value: summary.value?.videos.failed,
    pending: !summary.value && !summaryError.value,
    description: summary.value
      ? summary.value.videos.failed
        ? t("可在任务详情查看错误与日志")
        : ""
      : t(summaryError.value ? "统计暂不可用" : "正在读取视频统计"),
    icon: CircleAlert,
  },
]);

function dayKey(date: Date) {
  return `${date.getFullYear()}-${date.getMonth()}-${date.getDate()}`;
}
const dailyData = computed(() => {
  const today = now.value;
  const days = Array.from({ length: dayCount.value }, (_, index) => {
    const date = new Date(
      today.getFullYear(),
      today.getMonth(),
      today.getDate() - dayCount.value + 1 + index,
    );
    return {
      index,
      key: dayKey(date),
      label: date.toLocaleDateString(locale.value),
      tick: `${date.getMonth() + 1}/${date.getDate()}`,
      captures: 0,
    };
  });
  const byDay = new Map(days.map((day) => [day.key, day]));
  for (const entry of entries.value) {
    const day = byDay.get(dayKey(new Date(entry.createdAt)));
    if (day) day.captures++;
  }
  return days;
});
const recentCaptureCount = computed(() =>
  dailyData.value.reduce((total, day) => total + day.captures, 0),
);
const trendMax = computed(() =>
  Math.max(1, ...dailyData.value.map((day) => day.captures)),
);
const trendTicks = computed(() => [
  ...new Set([
    0,
    ...[1, 2, 3, 4].map((n) => Math.round(((dayCount.value - 1) * n) / 4)),
  ]),
]);
type DayData = (typeof dailyData.value)[number];
const trendConfig = computed(
  () =>
    ({
      captures: { label: t("提取记录"), color: "var(--chart-3)" },
    }) satisfies ChartConfig,
);
const trendTooltip = computed(() =>
  componentToString(trendConfig.value, ChartTooltipContent, {
    labelKey: "label",
  }),
);
function countTick(value: number) {
  return Number.isInteger(value) ? numberFormatter.value.format(value) : "";
}
</script>

<template>
  <section
    class="flex min-w-0 flex-1 flex-col gap-4 md:gap-6"
    :aria-label="t('数据概览')"
  >
    <div class="grid grid-cols-2 gap-4 xl:grid-cols-4">
      <Card v-for="metric in metrics" :key="metric.title" class="min-w-0">
        <CardHeader>
          <CardDescription class="font-medium">{{
            metric.title
          }}</CardDescription>
          <CardAction
            ><component
              :is="metric.icon"
              class="size-4 text-muted-foreground"
              aria-hidden="true"
          /></CardAction>
        </CardHeader>
        <CardContent class="space-y-3">
          <p class="text-3xl font-semibold tracking-tight tabular-nums">
            <Skeleton v-if="metric.pending" class="h-9 w-20" />
            <AnimatedNumber
              v-else
              :value="metric.value"
              :format="numberFormatter.format"
            />
          </p>
          <p class="text-xs leading-relaxed text-muted-foreground">
            {{ metric.description }}
          </p>
        </CardContent>
      </Card>
    </div>

    <p v-if="summaryError" role="status" class="text-sm text-muted-foreground">
      {{ t(summaryError) }}
    </p>

    <Card class="min-w-0">
      <CardHeader>
        <CardTitle>{{ t("提取趋势") }}</CardTitle>
        <CardDescription>{{
          t("{time} 条记录", {
            time: numberFormatter.format(recentCaptureCount),
          })
        }}</CardDescription>
        <CardAction>
          <ToggleGroup
            type="single"
            size="sm"
            :spacing="1"
            class="segmented"
            :model-value="period"
            @update:model-value="
              (value) => {
                if (value) period = String(value);
              }
            "
            :aria-label="t('趋势时间范围')"
          >
            <ToggleGroupItem value="7" :aria-label="t('近7天')">{{
              t("7 天")
            }}</ToggleGroupItem>
            <ToggleGroupItem value="14" :aria-label="t('近14天')">{{
              t("14 天")
            }}</ToggleGroupItem>
            <ToggleGroupItem value="30" :aria-label="t('近30天')">{{
              t("30 天")
            }}</ToggleGroupItem>
          </ToggleGroup>
        </CardAction>
      </CardHeader>
      <CardContent>
        <Skeleton
          v-if="!store.historyReady && !entries.length && !store.historyError"
          class="h-[240px] w-full sm:h-[280px]"
          role="status"
          :aria-label="t('正在读取')"
        />
        <Empty
          v-else-if="!store.historyReady && !entries.length"
          class="h-[240px] sm:h-[280px]"
        >
          <EmptyDescription>{{
            t(store.historyError || "正在读取")
          }}</EmptyDescription>
        </Empty>
        <ChartContainer
          v-else
          :config="trendConfig"
          class="h-[240px] w-full aspect-auto sm:h-[280px]"
          role="img"
          :aria-label="
            t('近 {count} 天提取记录共 {count2} 条', {
              count: dayCount,
              count2: recentCaptureCount,
            })
          "
        >
          <VisXYContainer
            :data="dailyData"
            :x-domain="[0, dayCount - 1]"
            :y-domain="[0, trendMax]"
            :duration="0"
          >
            <VisArea
              v-if="recentCaptureCount"
              :x="(d: DayData) => d.index"
              :y="(d: DayData) => d.captures"
              :color="trendConfig.captures.color"
              :opacity="0.2"
              :curve-type="CurveType.Linear"
            />
            <VisLine
              v-if="recentCaptureCount"
              :x="(d: DayData) => d.index"
              :y="(d: DayData) => d.captures"
              :color="trendConfig.captures.color"
              :line-width="2"
              :curve-type="CurveType.Linear"
            />
            <VisAxis
              type="x"
              :x="(d: DayData) => d.index"
              :tick-line="false"
              :domain-line="false"
              :grid-line="false"
              :tick-values="trendTicks"
              :tick-format="(value: number) => dailyData[value]?.tick ?? ''"
            />
            <VisAxis
              type="y"
              :tick-line="false"
              :domain-line="false"
              :num-ticks="3"
              :tick-format="countTick"
            />
            <ChartTooltip />
            <ChartCrosshair
              v-if="recentCaptureCount"
              :x="(d: DayData) => d.index"
              :y="(d: DayData) => d.captures"
              :template="trendTooltip"
              :color="trendConfig.captures.color"
            />
          </VisXYContainer>
        </ChartContainer>
      </CardContent>
    </Card>

    <div class="grid min-w-0 gap-4 lg:grid-cols-2 md:gap-6">
      <Card class="min-w-0">
        <CardHeader>
          <CardTitle>{{ t("视频处理结果") }}</CardTitle>
          <CardDescription>{{
            t("已结束视频成功率 {value}", { value: successRate })
          }}</CardDescription>
        </CardHeader>
        <CardContent
          ><OverviewStatusChart
            kind="bar"
            :total="summary?.videos.total"
            :pending="!summary && !summaryError"
            :items="videoChart"
            :label="t('视频处理结果')"
        /></CardContent>
      </Card>
      <Card class="min-w-0">
        <CardHeader
          ><CardTitle>{{ t("任务状态分布") }}</CardTitle></CardHeader
        >
        <CardContent
          ><OverviewStatusChart
            kind="ring"
            :total="summary?.tasks.total"
            :pending="!summary && !summaryError"
            :items="taskChart"
            :label="t('任务状态分布')"
        /></CardContent>
      </Card>
    </div>
  </section>
</template>
