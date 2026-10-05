<script setup lang="ts">
import { t } from "@/i18n";
import { computed } from "vue";
import { Donut, GroupedBar, Orientation } from "@unovis/ts";
import {
  VisAxis,
  VisDonut,
  VisGroupedBar,
  VisSingleContainer,
  VisXYContainer,
} from "@unovis/vue";
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  componentToString,
} from "@/components/ui/chart";
import { Empty, EmptyDescription } from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/skeleton";
const props = defineProps<{
  kind: "ring" | "bar";
  total?: number;
  pending?: boolean;
  items: { label: string; value: number; color: string }[];
  label: string;
}>();
const data = computed(() =>
  props.items.map((item, index) => ({ ...item, index, count: item.value })),
);
const ringData = computed(() => data.value.filter((item) => item.value > 0));
const config = computed(() => ({
  count: { label: t("数量"), color: "var(--chart-3)" },
}));
const tooltip = computed(() =>
  componentToString(config.value, ChartTooltipContent, { labelKey: "label" }),
);
const description = computed(() =>
  props.items.map((item) => `${item.label} ${item.value}`).join("，"),
);
</script>
<template>
  <div class="space-y-4">
    <ChartContainer
      v-if="total"
      :config="config"
      class="h-[220px] w-full aspect-auto"
      role="img"
      :aria-label="`${label}：${description}`"
      :style="{
        '--vis-donut-central-label-font-size': 'var(--text-3xl)',
        '--vis-donut-central-label-font-weight': '600',
        '--vis-donut-central-label-text-color': 'var(--foreground)',
      }"
    >
      <VisSingleContainer v-if="kind === 'ring'" :data="ringData" :duration="0">
        <VisDonut
          :value="(d: (typeof data)[number]) => d.count"
          :color="
            (d: (typeof data)[number]) =>
              d.count > 0 ? d.color : 'transparent'
          "
          :arc-width="24"
          :central-label="String(total)"
          :central-sub-label="t('全部任务')"
        />
        <ChartTooltip :triggers="{ [Donut.selectors.segment]: tooltip! }" />
      </VisSingleContainer>
      <VisXYContainer
        v-else
        :data="data"
        :duration="0"
        :x-domain="[-0.5, Math.max(0.5, data.length - 0.5)]"
        :y-domain="[0, Math.max(1, ...items.map((item) => item.value))]"
      >
        <VisGroupedBar
          :x="(d: (typeof data)[number]) => d.index"
          :y="(d: (typeof data)[number]) => d.count"
          :color="
            (d: (typeof data)[number]) =>
              d.count > 0 ? d.color : 'transparent'
          "
          :orientation="Orientation.Vertical"
          :rounded-corners="4"
          :group-max-width="48"
          :bar-min-height="0"
        />
        <VisAxis
          type="x"
          :tick-values="data.map((item) => item.index)"
          :tick-format="(value: number) => data[value]?.label ?? ''"
          :grid-line="false"
          :domain-line="false"
          :tick-line="false"
        />
        <VisAxis
          type="y"
          :num-ticks="3"
          :tick-format="
            (value: number) => (Number.isInteger(value) ? String(value) : '')
          "
          :domain-line="false"
          :tick-line="false"
        />
        <ChartTooltip :triggers="{ [GroupedBar.selectors.bar]: tooltip! }" />
      </VisXYContainer>
    </ChartContainer>
    <Skeleton
      v-else-if="pending"
      class="h-[220px] w-full"
      role="status"
      :aria-label="t('正在读取')"
    />
    <Empty v-else class="h-[220px]"
      ><EmptyDescription>{{
        total === undefined ? t("统计暂不可用") : t("暂无数据")
      }}</EmptyDescription></Empty
    >
    <div v-if="pending" class="flex justify-center gap-5" aria-hidden="true">
      <Skeleton v-for="index in 3" :key="index" class="h-4 w-16" />
    </div>
    <div v-else class="flex flex-wrap justify-center gap-x-5 gap-y-2 text-xs">
      <span
        v-for="item in items"
        :key="item.label"
        class="flex items-center gap-2"
        ><span
          v-if="total !== undefined && item.value > 0"
          class="size-2 rounded-full"
          :style="{ backgroundColor: item.color }"
        />{{ item.label
        }}<span class="font-medium tabular-nums">{{
          total === undefined ? "-" : item.value
        }}</span></span
      >
    </div>
  </div>
</template>
