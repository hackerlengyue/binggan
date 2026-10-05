import { t } from "@/i18n";
import { computed, onMounted, onBeforeUnmount, reactive, ref } from "vue";
import { useTraceStore } from "@/stores/trace";
import { buildKeyJson } from "@/lib/keyjson";
import { parseSettings, type PlayerSettings } from "@/lib/connection-api";
import { toast } from "vue-sonner";
import { useEnvironmentInfo } from "@/composables/useEnvironmentInfo";
import { WorkspaceService } from "@/mygo";

export function usePlayerSettings() {
  const store = useTraceStore();
  const {
    diagnosis,
    error: testError,
    testing,
    refresh,
  } = useEnvironmentInfo();
  const settingsError = ref("");
  const saving = ref(false),
    loaded = ref(false);
  const loadingSettings = ref(false);
  const draft = reactive<PlayerSettings>({
    mode: "auto",
    softwareName: "",
    appMd5: "",
  });
  const saved = ref<PlayerSettings>({ ...draft });
  const dirty = computed(
    () =>
      draft.mode !== saved.value.mode ||
      (draft.mode === "manual" &&
        (draft.softwareName !== saved.value.softwareName ||
          draft.appMd5 !== saved.value.appMd5)),
  );
  const candidates = computed(() =>
    store.keyHistory
      .map((entry) => {
        // Older saved records can recover metadata from their preserved requests.
        const extracted = entry.traces?.length
          ? buildKeyJson(entry.traces).keyJson.getPwdData
          : undefined;
        const data = { ...extracted, ...entry.keyJson.getPwdData };
        return {
          id: entry.id,
          name: entry.name,
          softwareName: data.softwareName || "",
          appMd5: data.appMd5 || "",
        };
      })
      .filter((item) => item.softwareName),
  );
  const selectedRecord = ref("");
  const displayPlayer = computed(() =>
    draft.mode === "manual" ? draft : diagnosis.value?.player,
  );
  const playerLocated = computed(
    () =>
      !!diagnosis.value?.checks.some(
        (check) => check.name === "播放器安装位置" && check.ok,
      ),
  );
  let disposed = false;
  async function loadSettings() {
    if (loadingSettings.value || saving.value) return;
    loadingSettings.value = true;
    try {
      const data = parseSettings({
        settings: await WorkspaceService.settings(),
      });
      if (disposed) return;
      Object.assign(draft, data.settings);
      saved.value = { ...data.settings };
      loaded.value = true;
      settingsError.value = "";
    } catch (error) {
      if (!disposed) settingsError.value = (error as Error).message;
    } finally {
      loadingSettings.value = false;
    }
  }
  async function testEnvironment(fill = false) {
    if (testing.value) return;
    const initialDraft = JSON.stringify(draft);
    const data = await refresh();
    if (!data || disposed) return;
    if (
      fill &&
      draft.mode === "manual" &&
      JSON.stringify(draft) === initialDraft
    ) {
      if (data.player) {
        Object.assign(draft, data.player);
        selectedRecord.value = "";
        toast.success(t("已填入本机播放器参数，保存后生效"));
      } else
        toast.info(
          t(
            playerLocated.value
              ? "已找到本机播放器，请从相同版本的提取记录填入校验参数或手动配置"
              : "未识别到本机播放器，可从提取记录填入或手动填写",
          ),
        );
    }
  }
  function useRecord(id: unknown) {
    const record = candidates.value.find((item) => item.id === String(id));
    if (!record) return;
    selectedRecord.value = record.id;
    draft.mode = "manual";
    draft.softwareName = record.softwareName;
    draft.appMd5 =
      record.appMd5 ||
      (diagnosis.value?.player?.softwareName === record.softwareName
        ? diagnosis.value.player.appMd5
        : diagnosis.value?.knownPlayers[record.softwareName]) ||
      "";
  }
  async function save() {
    if (!loaded.value || saving.value || loadingSettings.value || testing.value)
      return false;
    saving.value = true;
    settingsError.value = "";
    try {
      const data = parseSettings({
        settings: await WorkspaceService.saveSettings({ ...draft }),
      });
      if (disposed) return false;
      Object.assign(draft, data.settings);
      saved.value = { ...data.settings };
      loaded.value = true;
      toast.success(t("配置已保存，后续新任务生效"));
      return true;
    } catch (error) {
      if (!disposed) settingsError.value = (error as Error).message;
      return false;
    } finally {
      saving.value = false;
    }
  }
  onMounted(() => {
    void loadSettings();
    void testEnvironment();
  });
  onBeforeUnmount(() => {
    disposed = true;
  });

  return {
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
  };
}
