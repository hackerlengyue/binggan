//go:build darwin

package main

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/egoist/mygo"
)

func verifyMyGoPlayerReadiness(t *testing.T, w *mygo.Window, fixture *mygoPlayerFixture) {
	t.Helper()
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		for (const close of document.querySelectorAll('[data-slot="dialog-close"]')) close.click();
		return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `!document.querySelector('[role="dialog"]')`)
	defer fixture.accessibilityBlocked.Store(false)
	defer fixture.notInstalled.Store(false)
	defer fixture.courseFailure.Store(false)
	fixture.notInstalled.Store(true)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		location.hash='#/player-orchestration';
		document.querySelector('button[aria-label="重新检查"]').click(); return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `(() => {
		const section=document.querySelector('section[aria-label="播放编排"]');
		const next=[...section.querySelectorAll('button')].find(b=>b.textContent.includes('下一步'));
		return !section.innerText.includes('未检测到 SzPlayer') && next?.disabled &&
			!section.innerText.includes('正在检查运行条件');
	})()`)
	fixture.notInstalled.Store(false)
	fixture.accessibilityBlocked.Store(true)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		location.hash='#/player-orchestration';
		document.querySelector('button[aria-label="重新检查"]').click(); return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `(() => {
		const section=document.querySelector('section[aria-label="播放编排"]');
		const next=[...section.querySelectorAll('button')].find(b=>b.textContent.includes('下一步'));
		const permissionLabel=[...section.querySelectorAll('span')].find(s=>s.textContent.trim()==='辅助功能');
		const permissionButtons=[...section.querySelectorAll('button')].filter(b=>b.textContent.trim()==='打开权限设置');
		return section.innerText.includes('请先开启辅助功能权限') && next?.disabled &&
			permissionButtons.length===1 && permissionLabel?.parentElement?.contains(permissionButtons[0]);
	})()`)
	if out := os.Getenv("BINGGAN_MYGO_E2E_SNAPSHOT"); out != "" {
		png, err := w.CapturePage()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(strings.TrimSuffix(out, ".png")+"-permission-review.png", png, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		[...document.querySelectorAll('section[aria-label="播放编排"] button')].find(b=>b.textContent.trim()==='打开权限设置').click(); return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('[role="dialog"]')?.innerText.includes('需要辅助功能权限') && document.querySelector('[role="dialog"]')?.innerText.includes('设备控制和数据访问') && [...document.querySelectorAll('[role="dialog"] button')].some(b=>b.textContent.includes('在访达中显示应用'))`)
	if out := os.Getenv("BINGGAN_MYGO_E2E_SNAPSHOT"); out != "" {
		png, err := w.CapturePage()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(strings.TrimSuffix(out, ".png")+"-permission.png", png, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	waitMyGoPage(t, w, `document.body.innerText.includes('test fixture cannot open settings')`)
	if count := fixture.permissionOpens.Load(); count != 1 {
		t.Fatalf("permission settings call count: %d", count)
	}
	fixture.accessibilityBlocked.Store(false)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { window.dispatchEvent(new Event('focus')); return true })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `(() => {
		const section=document.querySelector('section[aria-label="播放编排"]');
		const next=[...section.querySelectorAll('button')].find(b=>b.textContent.includes('下一步'));
		return !document.querySelector('[role="dialog"]') && next && !next.disabled &&
			!section.innerText.includes('请先开启辅助功能权限');
	})()`)
	fixture.courseFailure.Store(true)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		[...document.querySelectorAll('section[aria-label="播放编排"] button')].find(b=>b.textContent.includes('下一步')).click(); return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('section[aria-label="播放编排"]')?.innerText.includes('isolated catalog unavailable')`)
	fixture.courseFailure.Store(false)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		[...document.querySelectorAll('section[aria-label="播放编排"] button')].find(b=>b.textContent.trim()==='重试读取').click(); return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `(() => {
		const section=document.querySelector('section[aria-label="播放编排"]');
		const next=[...section.querySelectorAll('button')].find(b=>b.textContent.trim()==='下一步');
		return section.innerText.includes('已选择 0 / 3 节') && !section.innerText.includes('isolated catalog unavailable') && next?.disabled;
	})()`)
}

func verifyMyGoPlayerPlanner(t *testing.T, w *mygo.Window) {
	t.Helper()
	previous, err := mygo.EvalAs[string](w.Page(), `localStorage.getItem("szjm.player-orchestration.v1") || ""`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = mygo.EvalAs[bool](w.Page(), fmt.Sprintf(`(() => {
			const previous=%q;
			if (previous) localStorage.setItem("szjm.player-orchestration.v1", previous);
			else localStorage.removeItem("szjm.player-orchestration.v1");
			return true;
		})()`, previous))
	}()
	before, err := mygo.EvalAs[float64](w.Page(), `performance.timeOrigin`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		localStorage.removeItem("szjm.player-orchestration.v1");
		location.hash="#/player-orchestration";
		location.reload();
		return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, fmt.Sprintf(`performance.timeOrigin > %f && location.hash === "#/player-orchestration" && !!document.querySelector('section[aria-label="播放编排"]')`, before))
	waitMyGoPage(t, w, `(() => {
		const button=[...document.querySelectorAll('section[aria-label="播放编排"] button')].find(b=>b.textContent.includes("下一步"));
		return button && !button.disabled;
	})()`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		[...document.querySelectorAll('section[aria-label="播放编排"] button')].find(b=>b.textContent.includes("下一步")).click();
		return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('section[aria-label="播放编排"]')?.innerText.includes("已选择 3 / 3 节")`)
	if obsolete, err := mygo.EvalAs[bool](w.Page(), `!!document.querySelector('section[aria-label="播放编排"] [role="radiogroup"]') || [...document.querySelectorAll('section[aria-label="播放编排"] button')].some(b=>['单选','多选'].includes(b.textContent.trim()))`); err != nil || obsolete {
		t.Fatalf("obsolete selection mode controls remain: %v %v", obsolete, err)
	}

	if selected, err := mygo.EvalAs[bool](w.Page(), `(() => {
		const saved=JSON.parse(localStorage.getItem("szjm.player-orchestration.v1"));
		return saved.courseSelectionInitialized === true && saved.selectedCourses.map(c=>c.name).join("|") === "阶段一 / 第一讲|阶段一 / 第二讲|阶段二 / 第一讲";
	})()`); err != nil || !selected {
		t.Fatalf("first course discovery must select all three in source order: %v, %v", selected, err)
	}
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		document.querySelector('button[aria-label^="阶段一：2 / 2"]').click();
		return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('section[aria-label="播放编排"]')?.innerText.includes("已选择 1 / 3 节")`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		const input=document.querySelector('input[aria-label="搜索课程"]');
		input.value="第二讲";
		input.dispatchEvent(new Event('input',{bubbles:true}));
		return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `(() => {
		const page=document.querySelector('section[aria-label="播放编排"]');
		return page?.innerText.includes("阶段一") && page.innerText.includes("第二讲") && !page.innerText.includes("阶段二");
	})()`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		const input=document.querySelector('input[aria-label="搜索课程"]');
		input.value="";
		input.dispatchEvent(new Event('input',{bubbles:true}));
		return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `!!document.querySelector('button[aria-label^="阶段二：1 / 1"]')`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
        document.querySelector('button[aria-label^="阶段二：1 / 1"]').click();
        return true;
    })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('section[aria-label="播放编排"]')?.innerText.includes("已选择 0 / 3 节")`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		[...document.querySelectorAll('section[aria-label="播放编排"] label')].find(l=>l.textContent.trim()==="第二讲").click();
		return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `JSON.parse(localStorage.getItem("szjm.player-orchestration.v1")).selectedCourses[0]?.name === "阶段一 / 第二讲"`)

	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		[...document.querySelectorAll('button[aria-label^="阶段一："]')][0].click();
		return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('section[aria-label="播放编排"]')?.innerText.includes("已选择 2 / 3 节")`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		[...document.querySelectorAll('section[aria-label="播放编排"] button')].find(b=>b.textContent.includes("阶段一") && b.hasAttribute("data-state")).click();
		return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('section[aria-label="播放编排"] button[data-state="closed"]')?.textContent.includes("阶段一")`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		[...document.querySelectorAll('section[aria-label="播放编排"] button[data-state="closed"]')].find(b=>b.textContent.includes("阶段一")).click();
		[...document.querySelectorAll('section[aria-label="播放编排"] button')].find(b=>b.textContent.includes("下一步")).click();
		return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `!!document.querySelector('#playback-volume') && !!document.querySelector('#action-delay')`)
	if defaults, err := mygo.EvalAs[bool](w.Page(), `(() => {
		const values=Object.fromEntries(["playback-volume","action-delay","course-ready-timeout","poll-interval","max-minutes"].map(id=>[id,document.getElementById(id)?.value]));
		return values["playback-volume"] === "35" && values["action-delay"] === "1500" && values["max-minutes"] === "180";
	})()`); err != nil || !defaults {
		t.Fatalf("player settings did not render safe defaults: %v, %v", defaults, err)
	}
	if _, err := mygo.EvalAs[bool](w.Page(), `(async () => {
		const edit=async(id,value)=>{
			const input=document.getElementById(id);
			input.value=value;
			input.dispatchEvent(new Event('input',{bubbles:true}));
			await new Promise(resolve=>setTimeout(resolve,50));
			input.dispatchEvent(new Event('change',{bubbles:true}));
		};
		await edit("playback-volume","47");
		await edit("action-delay","299");
		return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `(() => {
		const saved=JSON.parse(localStorage.getItem("szjm.player-orchestration.v1"));
		return saved.config.playbackVolumePercent === 47 && saved.config.actionDelayMs === 1500;
	})()`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(async () => {
		const input=document.getElementById("action-delay");
		input.value="1700";
		input.dispatchEvent(new Event('input',{bubbles:true}));
		await new Promise(resolve=>setTimeout(resolve,50));
		input.dispatchEvent(new Event('change',{bubbles:true}));
		const thumb=document.querySelector('[role="slider"]');
		thumb.focus();
		thumb.dispatchEvent(new KeyboardEvent('keydown',{key:'End',bubbles:true,cancelable:true}));
		return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `(() => {
		const saved=JSON.parse(localStorage.getItem("szjm.player-orchestration.v1"));
		return saved.config.actionDelayMs === 1700 && !("enterFullscreen" in saved.config) && saved.config.playbackRate === 2;
	})()`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(async () => {
		const thumb=document.querySelector('[role="slider"]');
		for(let i=0;i<5;i++) {
			thumb.dispatchEvent(new KeyboardEvent('keydown',{key:'ArrowLeft',bubbles:true,cancelable:true}));
			await new Promise(resolve=>setTimeout(resolve,40));
		}
		return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `(() => {
		const saved=JSON.parse(localStorage.getItem("szjm.player-orchestration.v1"));
		return saved.config.playbackRate === 1.5 && document.querySelector('output[for="playback-rate"]')?.textContent.includes("1.5");
	})()`)
	w.SetSize(390, 844)
	waitMyGoPage(t, w, `innerWidth <= 392`)
	if width, err := mygo.EvalAs[struct {
		View   int `json:"view"`
		Scroll int `json:"scroll"`
	}](w.Page(), `({view:innerWidth,scroll:document.documentElement.scrollWidth})`); err != nil || width.Scroll > width.View+1 {
		t.Fatalf("player settings overflow at 390px: %+v, %v", width, err)
	}
	before, err = mygo.EvalAs[float64](w.Page(), `performance.timeOrigin`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => { location.reload(); return true })()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, fmt.Sprintf(`performance.timeOrigin > %f && !!document.querySelector('section[aria-label="播放编排"]')`, before))
	waitMyGoPage(t, w, `(() => {
		const next=[...document.querySelectorAll('section[aria-label="播放编排"] button')].find(b=>b.textContent.includes("下一步"));
		return next && !next.disabled;
	})()`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		[...document.querySelectorAll('section[aria-label="播放编排"] button')].find(b=>b.textContent.includes("下一步")).click();
		return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('section[aria-label="播放编排"]')?.innerText.includes("已选择 2 / 3 节")`)
	if selected, err := mygo.EvalAs[bool](w.Page(), `(() => {
		const saved=JSON.parse(localStorage.getItem("szjm.player-orchestration.v1"));
		return saved.selectedCourses.map(c=>c.name).join("|") === "阶段一 / 第一讲|阶段一 / 第二讲";
	})()`); err != nil || !selected {
		t.Fatalf("manual course exclusion was lost after reload: %v, %v", selected, err)
	}
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		[...document.querySelectorAll('section[aria-label="播放编排"] button')].find(b=>b.textContent.includes("下一步")).click();
		return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `!!document.querySelector('#playback-volume')`)
	if restored, err := mygo.EvalAs[bool](w.Page(), `(() =>
		document.getElementById("playback-volume")?.value === "47" &&
		document.getElementById("action-delay")?.value === "1700" &&
		!document.getElementById("playback-fullscreen") &&
		document.querySelector('output[for="playback-rate"]')?.textContent.includes("1.5")
	)()`); err != nil || !restored {
		t.Fatalf("player preferences did not survive WebView reload: %v, %v", restored, err)
	}
	mygoE2E.player.(*mygoPlayerFixture).resumePending.Store(true)
	waitMyGoPage(t, w, `(() => {
		const start=[...document.querySelectorAll('section[aria-label="播放编排"] button')].find(b=>b.textContent.includes("开始任务"));
		return start && !start.disabled;
	})()`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		[...document.querySelectorAll('section[aria-label="播放编排"] button')].find(b=>b.textContent.includes("开始任务")).click();
		return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `(() => {
		const page=document.querySelector('section[aria-label="播放编排"]');
		return page?.querySelector('tbody tr')?.textContent.includes("执行失败") && page.innerText.includes("继续执行");
	})()`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		const failedRow=[...document.querySelectorAll('section[aria-label="播放编排"] tbody tr')].find(row=>row.textContent.includes("执行失败"));
		[...failedRow.querySelectorAll('button')].find(b=>b.textContent.trim()==="查看").click();
		return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('[role="dialog"]')?.innerText.includes("test fixture cannot expand a folder")`)
	if mygoE2E.player.(*mygoPlayerFixture).resumeDeclines.Load() != 1 || mygoE2E.player.(*mygoPlayerFixture).resumePending.Load() {
		t.Fatal("resume prompt was not declined exactly once through the MyGo binding")
	}
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		document.querySelector('[role="dialog"] [data-slot="dialog-close"]').click();
		[...document.querySelectorAll('section[aria-label="播放编排"] button')].find(b=>b.textContent.trim()==="结束任务").click();
		return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('[role="dialog"]')?.innerText.includes("结束任务？")`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		[...document.querySelectorAll('[role="dialog"] button')].find(b=>b.textContent.trim()==="结束任务").click();
		return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	// Ending keeps the result available; starting a new task resets its queue.
	waitMyGoPage(t, w, `(() => {
		const page=document.querySelector('section[aria-label="播放编排"]');
		return !document.querySelector('[role="dialog"]') && page?.innerText.includes("任务已结束") && page.querySelectorAll('tbody tr').length === 2;
	})()`)
	if _, err := mygo.EvalAs[bool](w.Page(), `(() => {
		[...document.querySelectorAll('section[aria-label="播放编排"] button')].find(b=>b.textContent.trim()==="新建任务").click();
		return true;
	})()`); err != nil {
		t.Fatal(err)
	}
	waitMyGoPage(t, w, `document.querySelector('section[aria-label="播放编排"]')?.innerText.includes("运行条件") && !document.querySelector('section[aria-label="播放编排"] tbody')`)
	if reset, err := mygo.EvalAs[bool](w.Page(), `(() => {
		const saved=JSON.parse(localStorage.getItem("szjm.player-orchestration.v1"));
		return saved.selectedCourses.length === 2 && !saved.queue;
	})()`); err != nil || !reset {
		state, _ := mygo.EvalAs[string](w.Page(), `(() => { const saved=JSON.parse(localStorage.getItem("szjm.player-orchestration.v1")); return JSON.stringify({selected:saved.selectedCourses?.length,logs:saved.logs}); })()`)
		t.Fatalf("new player task did not reset queue and retain course selection: %v, %v; state=%s", reset, err, state)
	}
}
