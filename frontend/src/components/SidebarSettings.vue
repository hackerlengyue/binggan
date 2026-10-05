<script setup lang="ts">
import { t, locale, setLocale } from "@/i18n";
// One footer switcher: the dropdown holds the language and theme groups.
import { ChevronsUpDown, Settings } from "@lucide/vue";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  useSidebar,
} from "@/components/ui/sidebar";
const props = defineProps<{ dark: boolean }>();
const emit = defineEmits<{ theme: [] }>();
const { isMobile } = useSidebar();
function setAppearance(value: unknown) {
  if (
    (value === "light" || value === "dark") &&
    (value === "dark") !== props.dark
  )
    emit("theme");
}
</script>
<template>
  <SidebarMenu>
    <SidebarMenuItem>
      <DropdownMenu>
        <DropdownMenuTrigger as-child>
          <SidebarMenuButton
            :tooltip="t('外观与语言')"
            :aria-label="t('外观与语言')"
            class="workspace-nav-item data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground"
          >
            <Settings />
            <span>{{ t("外观与语言") }}</span>
            <ChevronsUpDown class="ml-auto size-4 text-muted-foreground" />
          </SidebarMenuButton>
        </DropdownMenuTrigger>
        <DropdownMenuContent
          class="min-w-44 rounded-lg"
          :side="isMobile ? 'top' : 'right'"
          align="end"
          :side-offset="4"
        >
          <DropdownMenuLabel>{{ t("语言") }}</DropdownMenuLabel>
          <DropdownMenuRadioGroup
            :model-value="locale"
            @update:model-value="setLocale"
          >
            <DropdownMenuRadioItem value="zh-CN">中文</DropdownMenuRadioItem>
            <DropdownMenuRadioItem value="en">English</DropdownMenuRadioItem>
          </DropdownMenuRadioGroup>
          <DropdownMenuSeparator />
          <DropdownMenuLabel>{{ t("外观") }}</DropdownMenuLabel>
          <DropdownMenuRadioGroup
            :model-value="dark ? 'dark' : 'light'"
            @update:model-value="setAppearance"
          >
            <DropdownMenuRadioItem value="light">{{
              t("浅色")
            }}</DropdownMenuRadioItem>
            <DropdownMenuRadioItem value="dark">{{
              t("深色")
            }}</DropdownMenuRadioItem>
          </DropdownMenuRadioGroup>
        </DropdownMenuContent>
      </DropdownMenu>
    </SidebarMenuItem>
  </SidebarMenu>
</template>
