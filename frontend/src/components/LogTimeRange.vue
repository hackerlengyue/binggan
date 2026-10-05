<script setup lang="ts">
import { computed, ref } from "vue";
import { CalendarDate } from "@internationalized/date";
import type { DateRange } from "reka-ui";
import { CalendarDays, ChevronDown, X } from "@lucide/vue";
import { t, locale } from "@/i18n";
import { Button } from "@/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { RangeCalendar } from "@/components/ui/range-calendar";

// range is "all" or "custom". from/to are local "YYYY-MM-DDTHH:mm:ss" strings and
// carry a value only while the range is custom.
const range = defineModel<string>("range", { required: true });
const from = defineModel<string>("from", { required: true });
const to = defineModel<string>("to", { required: true });
const open = ref(false);
const active = computed(
  () => range.value === "custom" && !!from.value && !!to.value,
);
function day(value: string) {
  const match = value.match(/^(\d{4})-(\d{2})-(\d{2})/);
  return match
    ? new CalendarDate(Number(match[1]), Number(match[2]), Number(match[3]))
    : undefined;
}
const selection = computed<DateRange>(() => ({
  start: active.value ? day(from.value) : undefined,
  end: active.value ? day(to.value) : undefined,
}));
function short(value: string) {
  const date = new Date(value);
  return Number.isFinite(date.getTime())
    ? new Intl.DateTimeFormat(locale.value, {
        month: "numeric",
        day: "numeric",
      }).format(date)
    : "";
}
const label = computed(() =>
  active.value ? `${short(from.value)} – ${short(to.value)}` : t("全部时间"),
);
function pick(value: DateRange) {
  if (!value.start || !value.end) return;
  from.value = `${value.start.toString()}T00:00:00`;
  to.value = `${value.end.toString()}T23:59:59`;
  range.value = "custom";
  open.value = false;
}
function clear() {
  range.value = "all";
  from.value = "";
  to.value = "";
}
</script>

<template>
  <div class="relative">
    <Popover v-model:open="open">
      <PopoverTrigger as-child>
        <Button
          variant="outline"
          :aria-label="t('时间范围')"
          class="h-7 w-full justify-between gap-1.5 bg-transparent px-2.5 font-normal dark:bg-input/30"
        >
          <span class="flex min-w-0 items-center gap-1.5">
            <CalendarDays class="text-muted-foreground" />
            <span class="truncate">{{ label }}</span>
          </span>
          <ChevronDown v-if="!active" class="text-muted-foreground" />
          <span v-else class="size-4 shrink-0" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" class="w-auto p-0">
        <RangeCalendar
          :model-value="selection"
          :locale="locale"
          :week-starts-on="1"
          :number-of-months="1"
          :aria-label="t('选择日期范围')"
          @update:model-value="pick"
        />
      </PopoverContent>
    </Popover>
    <Button
      v-if="active"
      variant="ghost"
      size="icon-xs"
      class="absolute top-1/2 right-1.5 -translate-y-1/2 text-muted-foreground"
      :aria-label="t('清除时间范围')"
      :title="t('清除时间范围')"
      @click="clear"
    >
      <X />
    </Button>
  </div>
</template>
