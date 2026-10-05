<script setup lang="ts">
import { Label } from "@/components/ui/label";
import { computed } from "vue";
import { ChevronRight, FolderClosed, FolderOpen } from "@lucide/vue";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import type { CourseTreeNode } from "@/lib/course-tree";
import type { Course } from "@/lib/player-orchestration";

const props = withDefaults(
  defineProps<{
    node: CourseTreeNode;
    selectedIds: ReadonlySet<string>;
    depth?: number;
    disabled?: boolean;
  }>(),
  { depth: 0 },
);
const emit = defineEmits<{
  selectCourse: [course: Course, selected: boolean];
  selectFolder: [courses: Course[], selected: boolean];
}>();

const selectedCount = computed(() =>
  props.node.kind === "folder"
    ? props.node.courses.filter((course) => props.selectedIds.has(course.id))
        .length
    : 0,
);
const folderChecked = computed(() => {
  if (props.node.kind !== "folder" || !selectedCount.value) return false;
  return selectedCount.value === props.node.courses.length
    ? true
    : "indeterminate";
});
</script>

<template>
  <Collapsible
    v-if="node.kind === 'folder'"
    v-slot="{ open }"
    :default-open="true"
  >
    <div
      class="flex h-9 items-center gap-2 pr-4 [@media(hover:hover)]:hover:bg-muted/60"
      :style="{ paddingLeft: `${depth * 20 + 16}px` }"
    >
      <Checkbox
        :model-value="folderChecked"
        :disabled="disabled"
        :aria-label="`${node.label}：${selectedCount} / ${node.courses.length}`"
        @update:model-value="
          emit('selectFolder', node.courses, folderChecked !== true)
        "
      />
      <CollapsibleTrigger
        class="group/folder flex h-9 min-w-0 flex-1 cursor-default items-center gap-1.5 text-left text-sm font-medium outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        <ChevronRight
          class="size-3.5 shrink-0 text-muted-foreground transition-transform duration-150 group-data-[state=open]/folder:rotate-90 motion-reduce:transition-none"
        />
        <component
          :is="open ? FolderOpen : FolderClosed"
          class="size-4 shrink-0 text-primary/70"
        />
        <span class="min-w-0 flex-1 truncate" :title="node.hint">
          {{ node.label
          }}<span
            v-if="node.hint"
            class="ml-2 text-xs font-normal text-muted-foreground"
            >{{ node.hint }}</span
          >
        </span>
        <span
          class="shrink-0 text-xs font-normal tabular-nums text-muted-foreground"
        >
          {{ selectedCount }}/{{ node.courses.length }}
        </span>
      </CollapsibleTrigger>
    </div>
    <CollapsibleContent>
      <CourseTreeNode
        v-for="child in node.children"
        :key="child.key"
        :node="child"
        :depth="depth + 1"
        :selected-ids="selectedIds"
        :disabled="disabled"
        @select-course="
          (course, selected) => emit('selectCourse', course, selected)
        "
        @select-folder="
          (courses, selected) => emit('selectFolder', courses, selected)
        "
      />
    </CollapsibleContent>
  </Collapsible>

  <Label
    v-else
    class="font-normal leading-normal flex min-h-9 cursor-default items-center gap-2 py-1.5 pr-4 text-sm has-[[data-state=checked]]:bg-(--selection) [@media(hover:hover)]:hover:bg-muted/60"
    :style="{ paddingLeft: `${depth * 20 + 16 + 22}px` }"
  >
    <Checkbox
      :model-value="selectedIds.has(node.course.id)"
      :disabled="disabled"
      @update:model-value="emit('selectCourse', node.course, $event === true)"
    />
    <span class="min-w-0 flex-1 break-words leading-5">{{ node.label }}</span>
  </Label>
</template>
