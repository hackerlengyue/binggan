<script setup lang="ts">
import { t } from "@/i18n";
import { computed, ref } from "vue";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Empty, EmptyDescription } from "@/components/ui/empty";
import KeyDetails from "@/components/KeyDetails.vue";
import CaptureView from "@/components/CaptureView.vue";
import type { KeyJsonPayload, TrafficRow } from "@/types/trace";
const props = defineProps<{
  payload: KeyJsonPayload;
  traces?: TrafficRow[];
  capturing?: boolean;
  current?: boolean;
  loading?: boolean;
}>();
const activeTab = ref("keys");
const inspecting = ref(false);
const count = computed(() => Object.keys(props.payload.passwords).length);
</script>

<template>
  <Tabs v-model="activeTab" class="min-h-0 flex-1 gap-4">
    <div
      v-show="!inspecting"
      class="flex shrink-0 items-center justify-between"
    >
      <TabsList>
        <TabsTrigger value="keys"
          >{{ t("密码列表")
          }}<span class="ml-1 tabular-nums text-muted-foreground">{{
            count
          }}</span></TabsTrigger
        >
        <TabsTrigger value="requests"
          >{{ t("请求")
          }}<span
            v-if="traces"
            class="ml-1 tabular-nums text-muted-foreground"
            >{{ traces.length }}</span
          ></TabsTrigger
        >
      </TabsList>
      <slot name="status" />
    </div>
    <TabsContent
      value="keys"
      class="flex min-h-0 flex-1 flex-col overflow-hidden rounded-lg border"
    >
      <KeyDetails :payload="payload" :loading="loading" />
    </TabsContent>
    <TabsContent
      value="requests"
      class="flex min-h-0 flex-1 flex-col overflow-hidden rounded-lg border"
    >
      <CaptureView
        v-if="traces"
        :traces="traces"
        :current="current"
        :capturing="capturing"
        :loading="loading"
        @inspect="inspecting = $event"
      />
      <Empty v-else class="min-h-80"
        ><EmptyDescription>{{
          t("这条旧记录未保存请求内容")
        }}</EmptyDescription></Empty
      >
    </TabsContent>
  </Tabs>
</template>
