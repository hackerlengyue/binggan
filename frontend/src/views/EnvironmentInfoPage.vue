<script setup lang="ts">
import DataTable from "@/components/DataTable.vue";
import { onMounted } from "vue";
import { t } from "@/i18n";
import { FlaskConical } from "@lucide/vue";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Spinner } from "@/components/ui/spinner";
import TableSkeletonRows from "@/components/TableSkeletonRows.vue";
import {
  TableHeader,
  TableHead,
  TableBody,
  TableRow,
  TableCell,
} from "@/components/ui/table";
import { useEnvironmentInfo } from "@/composables/useEnvironmentInfo";
const {
  diagnosis,
  error: testError,
  testing,
  refresh: testEnvironment,
} = useEnvironmentInfo();
onMounted(() => {
  void testEnvironment();
});
</script>
<template>
  <section
    class="flex min-h-0 min-w-0 flex-1 flex-col gap-4"
    :aria-label="t('环境信息')"
  >
    <Teleport to="#workspace-page-actions">
      <Button
        variant="outline"
        :disabled="testing"
        :aria-busy="testing"
        :aria-label="testing ? t('正在检测') : t('检测环境')"
        @click="testEnvironment()"
        ><Spinner v-if="testing" /><FlaskConical v-else />{{
          t("检测环境")
        }}</Button
      >
    </Teleport>
    <p
      v-if="testError"
      role="alert"
      class="break-words text-sm text-destructive"
    >
      {{ t(testError) }}
    </p>
    <div
      v-if="diagnosis || testing"
      class="data-list-panel"
      :aria-busy="testing && !diagnosis"
    >
      <DataTable class="table-fixed"
        ><TableHeader
          ><TableRow
            ><TableHead class="w-[28%]">{{ t("检测项目") }}</TableHead
            ><TableHead class="w-20 text-center">{{ t("状态") }}</TableHead
            ><TableHead>{{ t("检测结果") }}</TableHead></TableRow
          ></TableHeader
        >
        <TableBody
          ><TableSkeletonRows v-if="!diagnosis" :columns="3" />
          <TableRow v-for="check in diagnosis?.checks || []" :key="check.name"
            ><TableCell class="break-words whitespace-normal">{{
              t(check.name)
            }}</TableCell
            ><TableCell class="text-center"
              ><Badge :variant="check.ok ? 'success' : 'destructive'">{{
                check.ok ? t("通过") : t("异常")
              }}</Badge></TableCell
            ><TableCell
              class="min-w-0 break-words whitespace-normal text-muted-foreground [overflow-wrap:anywhere]"
              >{{ t(check.message) }}</TableCell
            ></TableRow
          ></TableBody
        >
      </DataTable>
    </div>
  </section>
</template>
