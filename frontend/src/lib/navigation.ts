import {
  Radio,
  FilePlay,
  History,
  UnlockKeyhole,
  Laptop,
  SlidersHorizontal,
  ShieldCheck,
  ScrollText,
  ListVideo,
  Bell,
  Monitor,
} from "@lucide/vue";
export const navigationGroups = [
  {
    title: "视频处理",
    items: [
      { title: "秘钥提取", url: "/capture", icon: Radio },
      { title: "秘钥管理", url: "/history", icon: History },
      { title: "播放编排", url: "/player-orchestration", icon: ListVideo },
      { title: "视频解密", url: "/decrypt", icon: UnlockKeyhole },
      { title: "资源管理", url: "/resources", icon: FilePlay },
    ],
  },
  {
    title: "系统管理",
    items: [
      { title: "证书管理", url: "/certificates", icon: ShieldCheck },
      { title: "环境信息", url: "/environment", icon: Laptop },
      { title: "解密设置", url: "/decrypt-settings", icon: SlidersHorizontal },
      { title: "电源管理", url: "/power-settings", icon: Monitor },
      { title: "通知管理", url: "/notifications", icon: Bell },
      { title: "日志信息", url: "/logs", icon: ScrollText },
    ],
  },
];
export function routeMatches(path: string, url: string) {
  return path === url || path.startsWith(url + "/");
}
export function navigationSection(path: string) {
  return (
    navigationGroups.find((group) =>
      group.items.some((item) => routeMatches(path, item.url)),
    )?.title || "概览"
  );
}

export function isWorkspacePath(value: unknown): value is string {
  return (
    typeof value === "string" &&
    (value === "/overview" ||
      navigationGroups.some((group) =>
        group.items.some((item) => item.url === value),
      ))
  );
}
