<script setup lang="ts">
import { computed, ref } from "vue";
import {
  Download,
  RefreshCw,
  RotateCcw,
  ShieldAlert,
  ShieldCheck,
} from "@lucide/vue";
import { t } from "@/i18n";
import { formatDateTime } from "@/lib/datetime";
import { useCertificateManager } from "@/composables/useCertificateManager";
import { Card } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { Skeleton } from "@/components/ui/skeleton";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Empty, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogCancel,
} from "@/components/ui/alert-dialog";

const { desktop, certificate, error, checking, busy, refresh, act } =
  useCertificateManager();
const confirming = ref(false);
const certificateLabel = computed(() =>
  certificate.value?.trusted
    ? "CA 证书已被系统信任"
    : certificate.value?.installed
      ? "CA 证书已安装，尚未信任"
      : "CA 证书尚未安装到系统",
);
async function regenerate() {
  if (await act("regenerate")) confirming.value = false;
}
</script>
<template>
  <section class="w-full min-w-0 space-y-4" :aria-label="t('证书管理')">
    <Teleport v-if="desktop" to="#workspace-page-actions">
      <Button variant="ghost" :disabled="!!busy || checking" @click="refresh">
        <Spinner v-if="checking" /><RefreshCw v-else />{{ t("刷新状态") }}
      </Button>
      <template v-if="certificate && !certificate.trusted">
        <Button
          v-if="!certificate.installed"
          :disabled="!!busy || checking"
          :aria-label="t('将证书安装到系统')"
          :title="t('将证书安装到系统')"
          @click="act('install')"
          ><Spinner v-if="busy === 'install'" /><Download v-else /><span
            class="hidden @[40rem]/workspace:inline"
            >{{ t("将证书安装到系统") }}</span
          ></Button
        >
        <Button
          :variant="certificate.installed ? 'default' : 'outline'"
          :disabled="!!busy || checking || !certificate.installed"
          :aria-label="t('信任证书')"
          :title="t('信任证书')"
          @click="act('trust')"
          ><Spinner v-if="busy === 'trust'" /><ShieldCheck v-else /><span
            class="hidden @[40rem]/workspace:inline"
            >{{ t("信任证书") }}</span
          ></Button
        >
      </template>
      <Button
        v-if="certificate"
        variant="outline"
        :disabled="!!busy || checking"
        :aria-label="t('生成新证书')"
        :title="t('生成新证书')"
        @click="confirming = true"
        ><RotateCcw /><span class="hidden @[40rem]/workspace:inline">{{
          t("生成新证书")
        }}</span></Button
      >
    </Teleport>
    <Empty v-if="!desktop">
      <EmptyHeader>
        <ShieldCheck class="mx-auto size-6 text-muted-foreground" />
        <EmptyTitle>{{ t("请打开桌面软件") }}</EmptyTitle>
      </EmptyHeader>
    </Empty>
    <div v-else class="space-y-4">
      <Alert v-if="error" variant="destructive">
        <AlertDescription>{{ t(error) }}</AlertDescription>
      </Alert>
      <Card class="w-full gap-0 py-0">
        <div class="px-6 py-5">
          <Skeleton v-if="checking && !certificate" class="h-6 w-64" />
          <h2 v-else class="break-words text-base font-medium tracking-tight">
            {{ certificate?.name || t("本机 HTTPS 根证书") }}
          </h2>
          <div
            class="mt-2 flex min-h-5 items-center gap-1.5 text-xs"
            role="status"
            aria-live="polite"
          >
            <template v-if="checking || busy">
              <Spinner class="shrink-0 text-muted-foreground" />
              <span class="text-muted-foreground">{{
                checking ? t("正在检查证书状态") : t("正在处理，请完成系统授权")
              }}</span>
            </template>
            <template v-else-if="certificate">
              <ShieldCheck
                v-if="certificate.trusted"
                class="size-3.5 shrink-0 text-success"
              />
              <ShieldAlert v-else class="size-3.5 shrink-0 text-warning" />
              <span
                :class="certificate.trusted ? 'text-success' : 'text-warning'"
                >{{ t(certificateLabel) }}</span
              >
            </template>
            <span v-else class="text-muted-foreground">{{
              t("暂时无法读取证书")
            }}</span>
          </div>
        </div>
        <dl v-if="certificate" class="mx-6 grid gap-4 border-t py-5 text-sm">
          <div class="grid gap-1.5 sm:grid-cols-[80px_minmax(0,1fr)] sm:gap-6">
            <dt class="text-muted-foreground">{{ t("有效期") }}</dt>
            <dd class="min-w-0 tabular-nums">
              {{ formatDateTime(certificate.expiresAt) }}
            </dd>
          </div>
          <div class="grid gap-1.5 sm:grid-cols-[80px_minmax(0,1fr)] sm:gap-6">
            <dt class="text-muted-foreground">{{ t("指纹") }}</dt>
            <dd
              class="min-w-0 select-all break-all font-mono text-xs leading-5 text-muted-foreground"
            >
              {{ certificate.fingerprint }}
            </dd>
          </div>
        </dl>
        <div
          v-else-if="checking"
          role="status"
          :aria-label="t('正在检查证书状态')"
          class="mx-6 space-y-4 border-t py-5"
        >
          <div class="grid gap-2 sm:grid-cols-[80px_minmax(0,1fr)] sm:gap-6">
            <Skeleton class="h-4 w-16" /><Skeleton class="h-5 w-48" />
          </div>
          <div class="grid gap-2 sm:grid-cols-[80px_minmax(0,1fr)] sm:gap-6">
            <Skeleton class="h-4 w-16" /><Skeleton
              class="h-5 w-full max-w-lg"
            />
          </div>
        </div>
      </Card>
    </div>
    <AlertDialog
      :open="confirming"
      @update:open="!busy && (confirming = $event)"
    >
      <AlertDialogContent @escape-key-down="busy && $event.preventDefault()">
        <AlertDialogHeader>
          <AlertDialogTitle>{{ t("生成新证书？") }}</AlertDialogTitle>
          <AlertDialogDescription>{{
            t(
              "更换后需要先安装到系统，再重新信任。已信任的旧证书不再用于解密。",
            )
          }}</AlertDialogDescription>
        </AlertDialogHeader>
        <p
          v-if="error"
          role="alert"
          class="break-words text-sm text-destructive"
        >
          {{ t(error) }}
        </p>
        <AlertDialogFooter>
          <AlertDialogCancel :disabled="!!busy">{{
            t("取消")
          }}</AlertDialogCancel>
          <Button
            :disabled="!!busy || checking || !certificate"
            @click="regenerate"
            ><Spinner v-if="busy" />{{ t("确认生成") }}</Button
          >
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  </section>
</template>
