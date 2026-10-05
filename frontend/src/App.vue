<script setup lang="ts">
import { t } from "@/i18n";
import { computed, onMounted, onBeforeUnmount, ref, watch } from "vue";
import { RouterView, useRoute, useRouter } from "vue-router";
import { toast } from "vue-sonner";
import { Play, Square } from "@lucide/vue";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import {
  Field,
  FieldLabel,
  FieldError,
  FieldDescription,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { SidebarProvider, SidebarInset } from "@/components/ui/sidebar";
import SiteHeader from "@/components/SiteHeader.vue";
import SplashScreen from "@/components/SplashScreen.vue";
import StartupError from "@/components/StartupError.vue";
import { markAppReady, reducedMotion } from "@/lib/app-ready";
import { Toaster } from "@/components/ui/sonner";
import { isWorkspacePath } from "@/lib/navigation";
import { desktopNotifications } from "@/lib/notifications";
import AppSidebar from "@/components/AppSidebar.vue";
import { useTraceStore } from "@/stores/trace";
import { usePlayerOrchestrationStore } from "@/stores/player-orchestration";
import { events, WorkspaceService, LifecycleService } from "@/mygo";
import { isMyGo } from "mygo-runtime";
const store = useTraceStore();
const orchestration = usePlayerOrchestrationStore();
const taskOwnsCapture = computed(
  () =>
    orchestration.runState === "running" || orchestration.runState === "paused",
);
const mainContent = ref<HTMLElement | null>(null);
const route = useRoute();
const router = useRouter();
const bootstrapReady = ref(!isMyGo());
const bootstrapPhase = ref("正在启动服务");
const bootstrapError = ref("");
let bootstrapDisposed = false;
// The splash holds briefly, then its logo flies into the sidebar (see SplashScreen).
const splashHeld = ref(true);
const splashGone = ref(false);
const splashLeaving = computed(
  () => !splashHeld.value && bootstrapReady.value && !bootstrapError.value,
);
const splashVisible = computed(
  () => !splashGone.value && !bootstrapError.value,
);
const splashPhase = computed(() =>
  bootstrapReady.value ? "准备就绪" : bootstrapPhase.value,
);
watch(
  [splashLeaving, splashVisible],
  ([leaving, visible]) => {
    if (leaving || !visible) markAppReady();
  },
  { immediate: true },
);
let splashTimer: ReturnType<typeof setTimeout> | undefined;
function savedDarkTheme() {
  try {
    return localStorage.getItem("trace-theme") === "dark";
  } catch {
    return false;
  }
}
const dark = ref(savedDarkTheme());
const saveOpen = ref(false);
const saveMode = ref<"finish" | "rename">("rename");
const saveId = ref("");
const saveName = ref("");
const saveError = ref("");
const saving = ref(false);
const retryingHistory = ref(false);
async function retryHistory() {
  if (retryingHistory.value) return;
  retryingHistory.value = true;
  try {
    await store.retryKeyHistory();
  } catch (e) {
    toast.error(t((e as Error).message));
  } finally {
    retryingHistory.value = false;
  }
}
const pendingFile = computed(() =>
  store.keyHistory.find((entry) => entry.id === saveId.value),
);
const isCapturePage = computed(() => route.path === "/capture");
const pageTitle = computed(() =>
  route.path.startsWith("/history")
    ? t("秘钥管理")
    : route.path === "/player-orchestration"
      ? t("播放编排")
      : isCapturePage.value
        ? t("秘钥提取")
        : route.path === "/decrypt"
          ? t("视频解密")
          : route.path === "/resources"
            ? t("资源管理")
            : route.path === "/environment"
              ? t("环境信息")
              : route.path === "/decrypt-settings"
                ? t("解密设置")
                : route.path === "/power-settings"
                  ? t("电源管理")
                  : route.path === "/notifications"
                    ? t("通知管理")
                    : route.path === "/certificates"
                      ? t("证书管理")
                      : route.path === "/logs"
                        ? t("日志信息")
                        : t("数据概览"),
);
function applyTheme() {
  document.documentElement.classList.toggle("dark", dark.value);
  try {
    localStorage.setItem("trace-theme", dark.value ? "dark" : "light");
  } catch {
    // The current window can still switch themes if WebView storage is disabled.
  }
}
function toggleTheme() {
  dark.value = !dark.value;
  applyTheme();
}
function openSave(id: string, mode: "finish" | "rename" = "rename") {
  const entry = store.keyHistory.find((item) => item.id === id);
  if (!entry) return;
  saveId.value = id;
  saveMode.value = mode;
  saveName.value = entry.name;
  saveError.value = "";
  saveOpen.value = true;
}
async function start() {
  if (
    taskOwnsCapture.value ||
    store.busy ||
    store.captureEnabled ||
    saveOpen.value
  )
    return;
  try {
    await store.startCapture();
  } catch (error) {
    toast.error(t((error as Error).message));
    void desktopNotifications
      .captureAttention()
      .catch((notificationError) =>
        console.warn("Capture notification failed", notificationError),
      );
    return;
  }
  void desktopNotifications
    .captureStarted()
    .catch((error) => console.warn("Capture notification failed", error));
  try {
    await router.push("/capture");
  } catch (error) {
    toast.error(t((error as Error).message));
  }
}
async function stop() {
  if (taskOwnsCapture.value || store.busy || !store.captureEnabled) return;
  let entry;
  try {
    entry = await store.stopCapture({ deferReset: true });
  } catch (error) {
    toast.error(t((error as Error).message));
    void desktopNotifications
      .captureAttention()
      .catch((notificationError) =>
        console.warn("Capture notification failed", notificationError),
      );
    return;
  }
  if (entry) {
    void desktopNotifications
      .captureSaved()
      .catch((error) => console.warn("Capture notification failed", error));
  } else {
    toast.info(t("秘钥提取已停止"));
    void desktopNotifications
      .captureAttention()
      .catch((error) => console.warn("Capture notification failed", error));
  }
  try {
    await router.push("/capture");
  } catch (error) {
    toast.error(t((error as Error).message));
  }
  if (entry) openSave(entry.id, "finish");
}
function finishSave() {
  store.completeCapture(saveId.value);
  saveOpen.value = false;
  toast.success(t("已保存到秘钥管理"));
}
function cancelSave() {
  if (saveMode.value === "finish") finishSave();
  else saveOpen.value = false;
}
async function confirmSave() {
  if (!saveOpen.value || saving.value) return;
  if (!saveName.value.trim()) {
    saveError.value = "请输入记录名称";
    return;
  }
  saving.value = true;
  saveError.value = "";
  try {
    store.renameHistory(saveId.value, saveName.value);
    await store.flushKeyHistory();
    if (saveMode.value === "finish") finishSave();
    else {
      toast.success(t("名称已保存"));
      saveOpen.value = false;
    }
  } catch (error) {
    saveError.value = (error as Error).message;
  } finally {
    saving.value = false;
  }
}
function navigateFromMenu(path: string) {
  if (isWorkspacePath(path)) void router.push(path);
}
const quitting = ref(false);
let stopQuitRequest: (() => void) | undefined;
async function prepareAppQuit(id: number) {
  if (quitting.value) return;
  quitting.value = true;
  let failure = "";
  try {
    await orchestration.prepareQuit();
    if (store.busy) throw new Error("提取操作正在进行，请稍后再退出");
    await store.stopCapture();
    await store.flushKeyHistory();
  } catch (error) {
    failure = error instanceof Error ? error.message : String(error);
  }
  try {
    await LifecycleService.finishQuit(id, failure);
  } finally {
    quitting.value = false;
  }
}
let stopMenuNavigation: (() => void) | undefined;
let stopDecryptNotifications: (() => void) | undefined;
async function waitForBootstrap() {
  while (!bootstrapDisposed) {
    try {
      const status = await WorkspaceService.bootstrapStatus();
      bootstrapPhase.value = status.phase;
      if (status.error) {
        bootstrapError.value = status.error;
        return;
      }
      if (status.ready) {
        bootstrapReady.value = true;
        return;
      }
    } catch (error) {
      bootstrapError.value = (error as Error).message;
      return;
    }
    await new Promise((resolve) => setTimeout(resolve, 400));
  }
}
onMounted(async () => {
  splashTimer = setTimeout(
    () => (splashHeld.value = false),
    reducedMotion() ? 200 : 2200,
  );
  if (isMyGo()) {
    stopQuitRequest = events.desktopQuitRequested.on((id) => {
      void prepareAppQuit(id);
    });
    await LifecycleService.rendererReady();
    stopMenuNavigation = events.desktopNavigate.on(navigateFromMenu);
    stopDecryptNotifications = events.desktopDecryptTaskFinished.on(
      (result) => {
        void desktopNotifications
          .decryptTaskFinished(result)
          .catch((error) => console.warn("Decrypt notification failed", error));
      },
    );
  }
  applyTheme();
  if (isMyGo()) await waitForBootstrap();
  if (bootstrapDisposed || bootstrapError.value) return;
  void store
    .initKeyHistory()
    .then(() => store.init())
    .catch(() => {
      store.historyError =
        "密钥历史读取失败，请重新打开软件重试；现有记录未覆盖";
      toast.error(store.historyError);
    });
});
onBeforeUnmount(() => {
  clearTimeout(splashTimer);
  bootstrapDisposed = true;
  stopQuitRequest?.();
  stopMenuNavigation?.();
  stopDecryptNotifications?.();
  store.dispose();
});
</script>

<template>
  <StartupError v-if="bootstrapError" :message="bootstrapError" />
  <SplashScreen
    v-if="splashVisible"
    :phase="splashPhase"
    :leaving="splashLeaving"
    @done="splashGone = true"
  />
  <Button
    variant="outline"
    type="button"
    @click="mainContent?.focus()"
    class="sr-only focus:not-sr-only focus:fixed focus:top-2 focus:left-2 focus:z-50 focus:rounded-md focus:bg-background focus:p-3"
    >{{ t("跳转到内容") }}</Button
  >
  <SidebarProvider
    :inert="quitting || undefined"
    class="desktop-workspace h-dvh min-h-0 overflow-hidden"
    :style="{
      '--sidebar-width': '13rem',
      '--header-height': '52px',
    }"
  >
    <AppSidebar :dark="dark" @theme="toggleTheme" />
    <SidebarInset class="@container/workspace h-dvh min-w-0 overflow-hidden">
      <SiteHeader :title="pageTitle">
        <template v-if="isCapturePage" #default>
          <div class="flex items-center gap-2">
            <Button
              :variant="store.captureEnabled ? 'outline' : 'default'"
              :disabled="
                taskOwnsCapture ||
                !!store.busy ||
                store.captureEnabled ||
                !store.connected
              "
              @click="start"
              ><Spinner v-if="store.busy === 'start'" /><Play v-else />{{
                t("秘钥提取")
              }}</Button
            >
            <Button
              :variant="store.captureEnabled ? 'default' : 'outline'"
              :disabled="
                taskOwnsCapture || !!store.busy || !store.captureEnabled
              "
              :aria-busy="store.busy === 'stop'"
              :aria-label="
                store.busy === 'stop' ? t('正在停止…') : t('停止并保存')
              "
              @click="stop"
              ><Spinner v-if="store.busy === 'stop'" /><Square v-else />{{
                t("停止并保存")
              }}</Button
            >
          </div>
        </template>
      </SiteHeader>
      <div
        ref="mainContent"
        id="main-content"
        tabindex="-1"
        class="workspace-content @container/main flex min-h-0 flex-1 flex-col gap-4 overflow-auto overscroll-none p-4 outline-none [scrollbar-gutter:stable]"
      >
        <Alert v-if="store.historyError" variant="destructive"
          ><AlertDescription
            >{{ store.historyError
            }}<Button
              variant="link"
              :disabled="retryingHistory"
              @click="retryHistory"
              ><Spinner v-if="retryingHistory" />{{ t("重试保存") }}</Button
            ></AlertDescription
          ></Alert
        >
        <div
          v-if="!bootstrapReady"
          class="mx-auto flex min-h-72 max-w-xl flex-col items-center justify-center gap-3 text-center"
        >
          <Spinner v-if="!bootstrapError" class="size-6" />
          <p class="font-medium">{{ t(bootstrapPhase) }}</p>
        </div>
        <RouterView v-else v-slot="{ Component }"
          ><component
            :is="Component"
            v-on="
              isCapturePage || route.path.startsWith('/history')
                ? { rename: openSave }
                : {}
            "
        /></RouterView>
      </div>
    </SidebarInset>
  </SidebarProvider>
  <Dialog v-model:open="saveOpen">
    <DialogContent
      :show-close-button="saveMode === 'rename'"
      :aria-describedby="undefined"
      @escape-key-down="
        (saveMode === 'finish' || saving) && $event.preventDefault()
      "
      @interact-outside="
        (saveMode === 'finish' || saving) && $event.preventDefault()
      "
    >
      <DialogHeader>
        <DialogTitle>{{
          saveMode === "finish" ? t("保存提取记录") : t("修改记录名称")
        }}</DialogTitle>
      </DialogHeader>
      <form class="space-y-4" @submit.prevent="confirmSave">
        <Field :data-invalid="!!saveError">
          <FieldLabel
            for="capture-file-name"
            :class="{ 'sr-only': saveMode === 'rename' }"
            >{{ t("记录名称") }}</FieldLabel
          >
          <Input
            id="capture-file-name"
            v-model="saveName"
            maxlength="80"
            autofocus
            :aria-invalid="!!saveError"
            :aria-describedby="saveError ? 'capture-save-error' : undefined"
            @focus="($event.target as HTMLInputElement).select()"
            @update:model-value="saveError = ''"
          />
          <FieldError v-if="saveError" id="capture-save-error">{{
            saveError
          }}</FieldError>
          <FieldDescription
            v-if="saveMode === 'finish' && pendingFile && !pendingFile.valid"
          >
            {{
              Object.keys(pendingFile.keyJson.passwords).length
                ? t("密钥不完整，暂不可下载 JSON。")
                : t("本次没有密码，请求内容仍会保存。")
            }}
          </FieldDescription>
        </Field>
        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            :disabled="saving"
            @click="cancelSave"
            >{{ saveMode === "finish" ? t("使用默认名称") : t("取消") }}</Button
          >
          <Button type="submit" :disabled="saving || !saveName.trim()">{{
            t("保存")
          }}</Button>
        </DialogFooter>
      </form>
    </DialogContent>
  </Dialog>
  <Toaster :theme="dark ? 'dark' : 'light'" />
</template>
