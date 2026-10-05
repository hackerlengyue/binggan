<script setup lang="ts">
import { t, locale } from "@/i18n";
import { Save, WandSparkles } from "@lucide/vue";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";
import { Skeleton } from "@/components/ui/skeleton";
import { Field, FieldLabel } from "@/components/ui/field";
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@/components/ui/select";
import { usePlayerSettings } from "@/composables/usePlayerSettings";
import { useLeaveConfirmation } from "@/composables/useLeaveConfirmation";
import { onBeforeRouteLeave } from "vue-router";
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
const {
  settingsError,
  testError,
  testing,
  saving,
  loaded,
  loadingSettings,
  diagnosis,
  draft,
  dirty,
  candidates,
  selectedRecord,
  displayPlayer,
  playerLocated,
  loadSettings,
  testEnvironment,
  useRecord,
  save,
} = usePlayerSettings();
const leave = useLeaveConfirmation();
const leaveOpen = leave.open;
onBeforeRouteLeave(() => {
  if (saving.value) {
    toast.info(t("正在保存配置，请稍候"));
    return false;
  }
  return dirty.value ? leave.request() : true;
});
async function saveAndLeave() {
  if (await save()) leave.resolve(true);
}
</script>
<template>
  <section class="min-w-0" :aria-label="t('解密设置')">
    <Teleport to="#workspace-page-actions">
      <Button
        type="submit"
        form="player-settings-form"
        :disabled="!loaded || saving || testing || loadingSettings || !dirty"
        ><Spinner v-if="saving" /><Save v-else />{{ t("保存配置") }}</Button
      >
    </Teleport>
    <div
      v-if="!loaded && loadingSettings"
      role="status"
      :aria-label="t('正在读取')"
      class="preferences-form w-full space-y-4"
    >
      <div v-for="row in 3" :key="row" class="space-y-2">
        <Skeleton class="h-4 w-28" />
        <Skeleton class="h-8 w-full" />
      </div>
    </div>
    <form
      v-else
      id="player-settings-form"
      class="preferences-form w-full space-y-4"
      @submit.prevent="save"
    >
      <Field>
        <FieldLabel for="player-mode">{{ t("获取方式") }}</FieldLabel>
        <div class="flex flex-wrap items-center gap-3">
          <Select
            :key="locale"
            v-model="draft.mode"
            :disabled="saving || loadingSettings || !loaded"
          >
            <SelectTrigger id="player-mode" class="min-w-44 flex-1"
              ><SelectValue
            /></SelectTrigger>
            <SelectContent
              ><SelectItem value="auto">{{ t("自动识别") }}</SelectItem
              ><SelectItem value="manual">{{
                t("手动配置")
              }}</SelectItem></SelectContent
            >
          </Select>
          <Button
            type="button"
            variant="outline"
            class="shrink-0"
            :disabled="testing || saving || loadingSettings || !loaded"
            :aria-busy="testing"
            :aria-label="testing ? t('获取中') : t('自动获取')"
            @click="testEnvironment(true)"
            ><Spinner v-if="testing" /><WandSparkles v-else />{{
              t("自动获取")
            }}</Button
          >
        </div>
      </Field>
      <Field v-if="draft.mode === 'manual' && candidates.length"
        ><FieldLabel for="player-record">{{ t("从提取记录填入") }}</FieldLabel
        ><Select
          :key="locale"
          :model-value="selectedRecord"
          :disabled="saving || loadingSettings || !loaded"
          @update:model-value="useRecord"
          ><SelectTrigger id="player-record" class="w-full"
            ><SelectValue
              :placeholder="t('选择带有播放器版本的记录')" /></SelectTrigger
          ><SelectContent
            ><SelectItem
              v-for="item in candidates"
              :key="item.id"
              :value="item.id"
              >{{ item.name }} · {{ item.softwareName }}</SelectItem
            ></SelectContent
          ></Select
        ></Field
      >
      <div class="grid gap-4">
        <Field
          ><FieldLabel for="player-version">{{ t("App 版本标识") }}</FieldLabel
          ><Input
            id="player-version"
            :model-value="displayPlayer?.softwareName || ''"
            :readonly="draft.mode === 'auto'"
            :disabled="saving || loadingSettings || !loaded"
            :placeholder="
              draft.mode === 'auto'
                ? t(playerLocated ? '校验参数尚未取得' : '未检测到本机播放器')
                : t('例如 MAC_sz_26.06.54')
            "
            maxlength="100"
            @update:model-value="draft.softwareName = String($event)"
        /></Field>
        <Field
          ><FieldLabel for="player-md5">{{ t("App 校验值") }}</FieldLabel
          ><Input
            id="player-md5"
            class="font-mono"
            :model-value="displayPlayer?.appMd5 || ''"
            :readonly="draft.mode === 'auto'"
            :disabled="saving || loadingSettings || !loaded"
            :placeholder="t('32 位十六进制 appMd5')"
            maxlength="32"
            @update:model-value="draft.appMd5 = String($event)"
        /></Field>
      </div>
      <p
        v-if="draft.mode === 'auto' && diagnosis && !diagnosis.player"
        class="text-sm text-muted-foreground"
      >
        {{
          t(
            playerLocated
              ? "已找到本机播放器，请从相同版本的提取记录填入校验参数或手动配置"
              : "未检测到本机播放器，可手动填写。",
          )
        }}
      </p>
      <p
        v-if="diagnosis?.environmentAppMd5Configured"
        class="text-sm text-muted-foreground"
      >
        {{ t("已设置播放器校验值。") }}
      </p>
      <p v-if="testError" role="alert" class="text-sm text-destructive">
        {{ t(testError) }}
      </p>
      <p v-if="settingsError" role="alert" class="text-sm text-destructive">
        {{ t(settingsError) }}
      </p>
      <div
        v-if="dirty || !loaded"
        class="flex flex-wrap items-center gap-3 border-t pt-4"
      >
        <Button
          v-if="!loaded"
          type="button"
          variant="ghost"
          :disabled="saving || loadingSettings"
          @click="loadSettings"
          >{{ t("重新读取") }}</Button
        ><span v-if="dirty || !loaded" class="text-xs text-muted-foreground">{{
          dirty
            ? t("未保存")
            : loadingSettings
              ? t("正在读取")
              : t("配置未读取")
        }}</span>
      </div>
    </form>
  </section>
  <AlertDialog
    :open="leaveOpen"
    @update:open="!$event && !saving && leave.resolve(false)"
  >
    <AlertDialogContent
      class="data-[size=default]:max-w-[calc(100vw-2rem)] data-[size=default]:sm:max-w-lg"
      @escape-key-down="saving && $event.preventDefault()"
    >
      <AlertDialogHeader>
        <AlertDialogTitle>{{ t("保存更改后离开？") }}</AlertDialogTitle>
        <AlertDialogDescription>{{
          t("解密设置有未保存的更改，可以保存后离开，或继续编辑。")
        }}</AlertDialogDescription>
      </AlertDialogHeader>
      <p v-if="settingsError" role="alert" class="text-sm text-destructive">
        {{ t(settingsError) }}
      </p>
      <AlertDialogFooter class="flex-wrap">
        <AlertDialogCancel :disabled="saving">{{
          t("继续编辑")
        }}</AlertDialogCancel>
        <Button
          variant="outline"
          :disabled="saving"
          @click="leave.resolve(true)"
          >{{ t("放弃更改") }}</Button
        >
        <Button
          :disabled="saving || testing || loadingSettings"
          @click="saveAndLeave"
          ><Spinner v-if="saving" />{{ t("保存并离开") }}</Button
        >
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>
</template>
