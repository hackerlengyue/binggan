<script setup lang="ts">
import { Skeleton } from "@/components/ui/skeleton";
import { TableCell, TableRow } from "@/components/ui/table";

withDefaults(
  defineProps<{ columns: number; rows?: number; selection?: boolean }>(),
  {
    rows: 7,
    selection: false,
  },
);
</script>

<template>
  <TableRow v-for="row in rows" :key="row" aria-hidden="true">
    <TableCell v-for="column in columns" :key="column" class="py-4">
      <Skeleton
        :class="
          selection && column === 1
            ? 'size-4'
            : column === 1 || (selection && column === 2)
              ? 'h-4 w-3/4'
              : column === columns
                ? 'ml-auto h-4 w-1/2'
                : 'h-4 w-2/3'
        "
      />
    </TableCell>
  </TableRow>
</template>
