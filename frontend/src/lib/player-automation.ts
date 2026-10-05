import {
  PlayerService,
  type Status as PlayerAutomationStatus,
  type Snapshot as PlayerSnapshot,
} from "@/mygo";
import { isMyGo } from "mygo-runtime";

export type { PlayerAutomationStatus, PlayerSnapshot };
export type PlayerClickMatch = {
  ordinal: number;
  count: number;
  path: string[];
  expectedPid?: number;
};

function desktopOnly<Args extends unknown[], Result>(
  operation: (...args: Args) => Promise<Result>,
) {
  return (...args: Args): Promise<Result> =>
    isMyGo()
      ? operation(...args)
      : Promise.reject(new Error("播放编排仅在桌面软件中可用"));
}

const click = desktopOnly(PlayerService.click);

// Keep the orchestration's course-match argument shape; MyGo generates the IPC.
export const playerAutomation = {
  status: desktopOnly(PlayerService.status),
  wait: desktopOnly(PlayerService.wait),
  snapshot: desktopOnly(PlayerService.snapshot),
  playbackSnapshot: desktopOnly(PlayerService.playbackSnapshot),
  courseSnapshot: desktopOnly(PlayerService.courseSnapshot),
  expandFolder: desktopOnly(PlayerService.expandFolder),
  revealCourse: (path: string[], expectedPid?: number) =>
    desktopOnly(PlayerService.revealCourse)({ path, expectedPid }),
  openCourse: desktopOnly(PlayerService.openCourse),
  setVolume: desktopOnly(PlayerService.setVolume),
  setFullscreen: desktopOnly(PlayerService.setFullscreen),
  setPlaybackRate: desktopOnly(PlayerService.setPlaybackRate),
  togglePlayback: desktopOnly(PlayerService.togglePlayback),
  click: (text: string, clickCount = 1, match?: PlayerClickMatch) =>
    click({
      text,
      clickCount,
      ...(match
        ? {
            path: match.path,
            expectedPid: match.expectedPid,
          }
        : {}),
    }),
  openPlayer: desktopOnly(PlayerService.openPlayer),
  openPermissionSettings: desktopOnly(PlayerService.openPermissionSettings),
  revealAppForPermission: desktopOnly(PlayerService.revealAppForPermission),
};
