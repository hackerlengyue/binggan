<script setup lang="ts">
import { ref } from "vue";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import { t } from "@/i18n";
import { RouterLink } from "vue-router";
import NavMain from "@/components/NavMain.vue";
import SidebarSettings from "@/components/SidebarSettings.vue";
import {
  Sidebar,
  SidebarHeader,
  SidebarContent,
  SidebarFooter,
  SidebarMenu,
  SidebarMenuItem,
  SidebarMenuButton,
  useSidebar,
} from "@/components/ui/sidebar";
defineProps<{ dark: boolean }>();
defineEmits<{ theme: [] }>();
const { setOpenMobile } = useSidebar();
const aboutOpen = ref(false);
const appVersion = __APP_VERSION__;
</script>
<template>
  <Sidebar collapsible="offcanvas" class="desktop-sidebar">
    <SidebarHeader class="desktop-sidebar-header px-3 py-3" data-window-drag>
      <SidebarMenu
        ><SidebarMenuItem class="flex items-center gap-1"
          ><SidebarMenuButton
            class="min-w-0 flex-1 px-1"
            size="default"
            as-child
            :tooltip="t('饼干大小姐')"
          >
            <RouterLink
              to="/overview"
              :aria-label="t('饼干大小姐')"
              @click="setOpenMobile(false)"
            >
              <img
                src="/binggan-logo.png"
                alt=""
                class="size-6 shrink-0 rounded-md object-cover"
                width="24"
                height="24"
              />
              <span class="truncate text-sm font-semibold">{{
                t("饼干大小姐")
              }}</span>
            </RouterLink>
          </SidebarMenuButton>
          <Button
            variant="ghost"
            size="sm"
            class="h-6 shrink-0 px-1 text-[10px] tabular-nums text-muted-foreground"
            :aria-label="`${t('版本信息')} ${appVersion}`"
            :title="t('版本信息')"
            @click="aboutOpen = true"
            >v{{ appVersion }}</Button
          >
        </SidebarMenuItem></SidebarMenu
      >
    </SidebarHeader>
    <SidebarContent><NavMain /></SidebarContent>
    <SidebarFooter class="border-t px-3 py-2"
      ><SidebarSettings :dark="dark" @theme="$emit('theme')"
    /></SidebarFooter>
  </Sidebar>
  <Dialog v-model:open="aboutOpen">
    <DialogContent
      class="gap-0 max-w-[calc(100vw_-_2rem)] p-6 sm:max-w-[320px]"
      :show-close-button="false"
    >
      <DialogHeader class="sr-only">
        <DialogTitle>{{ t("关于") }}</DialogTitle>
      </DialogHeader>
      <div class="flex flex-col items-center pt-4 pb-4 text-center">
        <img
          src="/binggan-logo.png"
          alt=""
          width="80"
          height="80"
          class="mb-4 size-20 rounded-2xl object-cover"
        />
        <p class="text-[22px] leading-7 font-semibold">{{ t("饼干大小姐") }}</p>
        <DialogDescription class="mt-2 text-sm">
          {{ t("版本")
          }}<strong
            class="ml-1.5 font-medium text-foreground/75 tabular-nums"
            >{{ appVersion }}</strong
          >
        </DialogDescription>
      </div>
    </DialogContent>
  </Dialog>
</template>
