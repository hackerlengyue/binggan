<script setup lang="ts">
import { ref } from "vue";
import { Bell } from "@lucide/vue";
import { isMyGo } from "mygo-runtime";
import { toast } from "vue-sonner";
import { t } from "@/i18n";
import {
  desktopNotifications,
  notificationEnabled,
  notificationOptions,
  setNotificationEnabled,
  type NotificationGroup,
  type NotificationKind,
} from "@/lib/notifications";
import { Button } from "@/components/ui/button";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { Spinner } from "@/components/ui/spinner";

const sections: { group: NotificationGroup; title: string }[] = [
  { group: "capture", title: "秘钥提取" },
  { group: "player", title: "播放编排" },
  { group: "decrypt", title: "视频解密" },
];
const enabled = ref(
  Object.fromEntries(
    notificationOptions.map(({ kind }) => [kind, notificationEnabled(kind)]),
  ) as Record<NotificationKind, boolean>,
);
const testing = ref(false);
const desktop = isMyGo();

function setEnabled(kind: NotificationKind, value: boolean) {
  if (!setNotificationEnabled(kind, value)) {
    toast.error(t("无法保存通知设置"));
    return;
  }
  enabled.value[kind] = value;
}

async function sendTest() {
  if (testing.value) return;
  testing.value = true;
  try {
    await desktopNotifications.sendTest();
    toast.success(t("已请求发送测试通知"));
  } catch (error) {
    toast.error(t((error as Error).message));
  } finally {
    testing.value = false;
  }
}
</script>

<template>
  <section class="min-w-0 space-y-4" :aria-label="t('通知管理')">
    <Teleport to="#workspace-page-actions">
      <Button
        variant="outline"
        :disabled="!desktop || testing"
        @click="sendTest"
      >
        <Spinner v-if="testing" /><Bell v-else />{{ t("发送测试通知") }}
      </Button>
    </Teleport>
    <section
      v-for="section in sections"
      :key="section.group"
      :aria-label="t(section.title)"
      class="space-y-2"
    >
      <h2 class="text-sm font-medium">{{ t(section.title) }}</h2>
      <div class="divide-y rounded-lg border bg-card">
        <Field
          v-for="option in notificationOptions.filter(
            (item) => item.group === section.group,
          )"
          :key="option.kind"
          orientation="horizontal"
          class="justify-between px-5 py-4 sm:px-6"
        >
          <div class="min-w-0 space-y-1">
            <FieldLabel :id="`notification-${option.kind}-label`">{{
              t(option.label)
            }}</FieldLabel>
            <FieldDescription>{{ t(option.description) }}</FieldDescription>
          </div>
          <ToggleGroup
            type="single"
            :model-value="enabled[option.kind] ? 'on' : 'off'"
            :spacing="1"
            class="segmented shrink-0"
            size="sm"
            :aria-labelledby="`notification-${option.kind}-label`"
            @update:model-value="
              (value) => {
                if (value === 'on' || value === 'off')
                  setEnabled(option.kind, value === 'on');
              }
            "
          >
            <ToggleGroupItem value="on" data-setting-enabled>{{
              t("开启")
            }}</ToggleGroupItem>
            <ToggleGroupItem value="off">{{ t("关闭") }}</ToggleGroupItem>
          </ToggleGroup>
        </Field>
      </div>
    </section>
  </section>
</template>
