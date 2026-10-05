import { createRouter, createWebHashHistory } from "vue-router";
import CapturePage from "@/views/CapturePage.vue";
import HistoryPage from "@/views/HistoryPage.vue";

export const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: "/", redirect: "/overview" },
    { path: "/overview", component: () => import("@/views/OverviewPage.vue") },
    {
      path: "/resources",
      component: () => import("@/views/ResourcesPage.vue"),
    },
    { path: "/decrypt", component: () => import("@/views/DecryptPage.vue") },
    { path: "/logs", component: () => import("@/views/LogsPage.vue") },
    {
      path: "/environment",
      component: () => import("@/views/EnvironmentInfoPage.vue"),
    },
    {
      path: "/decrypt-settings",
      component: () => import("@/views/DecryptionSettingsPage.vue"),
    },
    {
      path: "/notifications",
      component: () => import("@/views/NotificationSettingsPage.vue"),
    },
    {
      path: "/power-settings",
      component: () => import("@/views/PowerSettingsPage.vue"),
    },
    {
      path: "/certificates",
      component: () => import("@/views/CertificatesPage.vue"),
    },
    { path: "/capture", component: CapturePage },
    {
      path: "/player-orchestration",
      component: () => import("@/views/PlayerOrchestrationPage.vue"),
    },
    { path: "/history/:id?", component: HistoryPage },
    { path: "/:pathMatch(.*)*", redirect: "/overview" },
  ],
});
