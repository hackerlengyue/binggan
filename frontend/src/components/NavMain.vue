<script setup lang="ts">
import { t } from "@/i18n";
import { RouterLink, useRoute } from "vue-router";
import { LayoutDashboard } from "@lucide/vue";
import {
  SidebarGroup,
  SidebarGroupLabel,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  useSidebar,
} from "@/components/ui/sidebar";
import { navigationGroups, routeMatches } from "@/lib/navigation";
const route = useRoute();
const { setOpenMobile } = useSidebar();
</script>

<template>
  <SidebarGroup>
    <SidebarMenu>
      <SidebarMenuItem>
        <SidebarMenuButton
          class="workspace-nav-item"
          as-child
          :tooltip="t('数据概览')"
          :is-active="route.path === '/overview'"
        >
          <RouterLink
            to="/overview"
            :aria-label="t('数据概览')"
            :aria-current="route.path === '/overview' ? 'page' : undefined"
            @click="setOpenMobile(false)"
          >
            <LayoutDashboard /><span>{{ t("数据概览") }}</span>
          </RouterLink>
        </SidebarMenuButton>
      </SidebarMenuItem>
    </SidebarMenu>
  </SidebarGroup>
  <SidebarGroup v-for="group in navigationGroups" :key="group.title">
    <SidebarGroupLabel>{{ t(group.title) }}</SidebarGroupLabel>
    <SidebarMenu>
      <SidebarMenuItem v-for="item in group.items" :key="item.url">
        <SidebarMenuButton
          class="workspace-nav-item"
          as-child
          :tooltip="t(item.title)"
          :is-active="routeMatches(route.path, item.url)"
        >
          <RouterLink
            :to="item.url"
            :aria-label="t(item.title)"
            :aria-current="
              routeMatches(route.path, item.url) ? 'page' : undefined
            "
            @click="setOpenMobile(false)"
          >
            <component :is="item.icon" /><span>{{ t(item.title) }}</span>
          </RouterLink>
        </SidebarMenuButton>
      </SidebarMenuItem>
    </SidebarMenu>
  </SidebarGroup>
</template>
