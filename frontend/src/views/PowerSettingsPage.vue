<script setup lang="ts">
import { t } from "@/i18n";
import { usePowerSettings } from "@/composables/usePowerSettings";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
const { enabled, loaded, busy, error, load, save } = usePowerSettings();
</script>
<template>
  <section class="min-w-0 space-y-4" :aria-label="t('电源管理')">
    <div class="rounded-lg border bg-card">
      <Field orientation="horizontal" class="justify-between px-5 py-4 sm:px-6">
        <div class="min-w-0 space-y-1">
          <FieldLabel id="keep-awake-label">{{
            t("运行时阻止息屏和屏保")
          }}</FieldLabel>
          <FieldDescription>{{
            t(
              "开启后，应用运行期间保持屏幕常亮，包括最小化到后台时。关闭或退出应用后恢复系统设置。",
            )
          }}</FieldDescription>
        </div>
        <div class="flex shrink-0 items-center gap-2" :aria-busy="busy">
          <Spinner v-if="busy" />
          <ToggleGroup
            type="single"
            :model-value="enabled ? 'on' : 'off'"
            :disabled="!loaded || busy"
            :spacing="1"
            class="segmented"
            size="sm"
            aria-labelledby="keep-awake-label"
            @update:model-value="
              (value) => {
                if (value === 'on' || value === 'off') save(value === 'on');
              }
            "
          >
            <ToggleGroupItem value="on" data-setting-enabled>{{
              t("开启")
            }}</ToggleGroupItem>
            <ToggleGroupItem value="off">{{ t("关闭") }}</ToggleGroupItem>
          </ToggleGroup>
        </div>
      </Field>
    </div>
    <div v-if="error" class="flex items-center gap-3">
      <p role="alert" class="text-sm text-destructive">{{ t(error) }}</p>
      <Button v-if="!loaded" variant="outline" :disabled="busy" @click="load">{{
        t("重试")
      }}</Button>
    </div>
  </section>
</template>
