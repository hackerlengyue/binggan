//go:build darwin && cgo

package playerautomation

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeToggleDoesNotRepeatUncertainAXAction(t *testing.T) {
	source, err := os.ReadFile("automation_darwin.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	start, end := strings.Index(text, "static int togglePlayerPlayback("), strings.Index(text, "static AXUIElementRef copyPlaybackWindow(")
	if start < 0 || end <= start {
		t.Fatal("native toggle not found")
	}
	rateStart, rateEnd := strings.Index(text, "static int setPlayerPlaybackRate("), strings.Index(text, "static AXUIElementRef findDeepestTextElement(")
	if rateStart < 0 || rateEnd <= rateStart {
		t.Fatal("native rate setter not found")
	}
	fixture := `#include <ApplicationServices/ApplicationServices.h>
#include <math.h>
#include <stdio.h>
#include <unistd.h>
static AXError actionResult;
static int clicks, readMode;
static double clockNow;
static double monotonicSeconds(void) { return clockNow; }
static AXUIElementRef fixtureElement(void) { return (AXUIElementRef)CFRetain(kCFBooleanTrue); }
static AXUIElementRef fixtureApp(pid_t pid) { return fixtureElement(); }
static AXUIElementRef findPlaybackSlider(AXUIElementRef a,int b,int*c,double d) {
 if(readMode==1) return NULL;
 if(readMode==2) { clockNow+=2.1; return NULL; }
 return fixtureElement();
}
static AXUIElementRef findPlaybackButton(AXUIElementRef a,CGPoint b,int c,int*d,double deadline) {
 if(readMode==3) { clockNow+=2.1; return NULL; }
 return fixtureElement();
}
static int supportsPress(AXUIElementRef a) { return 1; }
static AXUIElementRef findElement(AXUIElementRef a,const char*b,int c,int d,int e,int*f,double deadline) { return fixtureElement(); }
static AXUIElementRef findPlayerRateMenuItem(AXUIElementRef a,const char*b,int c,int*d,double deadline) { return fixtureElement(); }
static AXUIElementRef findPlayerRateMenuItemInAttribute(AXUIElementRef a,CFStringRef b,const char*c,double deadline) { return fixtureElement(); }
static double elementWidth(AXUIElementRef a) { return readMode==4 ? 0 : 624; }
static int elementCenter(AXUIElementRef a,CGPoint*b) { *b=CGPointZero; return 1; }
static AXError fixtureAction(AXUIElementRef a,CFStringRef b) { return actionResult; }
static int postPlayerSingleClick(pid_t pid,double x,double y) { clicks++; return 1; }
static int revealPlayerControlBar(pid_t pid) { return 0; }
#define AXUIElementCreateApplication fixtureApp
#define AXUIElementPerformAction fixtureAction
` + text[start:end] + text[rateStart:rateEnd] + `
int main(void) {
 AXError errors[]={kAXErrorCannotComplete,kAXErrorFailure,kAXErrorInvalidUIElement};
 for(int i=0;i<3;i++) {
  clicks=0; actionResult=errors[i]; int result=togglePlayerPlayback(1);
  if(result==1 || clicks!=0) { fprintf(stderr,"uncertain action %d repeated: result=%d clicks=%d\n",errors[i],result,clicks); return 1; }
 }
 for(int i=0;i<3;i++) {
  clicks=0; actionResult=errors[i]; int result=setPlayerPlaybackRate(1,"1.0X");
  if(result==1 || clicks!=0) { fprintf(stderr,"uncertain rate action repeated: result=%d clicks=%d\n",result,clicks); return 4; }
 }
 clicks=0; actionResult=kAXErrorActionUnsupported;
 if(togglePlayerPlayback(1)!=1 || clicks!=1) return 2;
 clicks=0; actionResult=kAXErrorSuccess;
 if(togglePlayerPlayback(1)!=1 || clicks!=0) return 3;
 clicks=0; actionResult=kAXErrorSuccess;
 if(setPlayerPlaybackRate(1,"1.0X")!=1 || clicks!=0) return 5;
 int expected[]={-1,-6,-6,-7};
 for(int mode=1;mode<=4;mode++) {
  readMode=mode;clicks=0;clockNow=0;
  int result=togglePlayerPlayback(1);
  if(result!=expected[mode-1] || clicks) {
   fprintf(stderr,"unsafe or indistinguishable read failure: mode=%d result=%d clicks=%d\n",mode,result,clicks);return 6;
  }
 }
 return 0;
}
`
	dir := t.TempDir()
	file, binary := filepath.Join(dir, "action.c"), filepath.Join(dir, "action")
	if err := os.WriteFile(file, []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("xcrun", "clang", file, "-framework", "ApplicationServices", "-o", binary).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	if out, err := exec.Command(binary).CombinedOutput(); err != nil {
		t.Fatalf("action uncertainty: %v\n%s", err, out)
	}
}

func TestNativeToggleRevealsHiddenControlBarBeforePressing(t *testing.T) {
	source, err := os.ReadFile("automation_darwin.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	start, end := strings.Index(text, "static int togglePlayerPlayback("), strings.Index(text, "static AXUIElementRef copyPlaybackWindow(")
	if start < 0 || end <= start {
		t.Fatal("native toggle not found")
	}
	fixture := `#include <ApplicationServices/ApplicationServices.h>
#include <math.h>
#include <stdio.h>
#include <unistd.h>
static int hidden=1, reveals, clicks;
static AXError actionResult = kAXErrorSuccess;
static double monotonicSeconds(void) { return 0; }
static AXUIElementRef fixtureElement(void) { return (AXUIElementRef)CFRetain(kCFBooleanTrue); }
static AXUIElementRef fixtureApp(pid_t pid) { return fixtureElement(); }
static AXUIElementRef findPlaybackSlider(AXUIElementRef a,int b,int*c,double d) {
 if(hidden) return NULL;
 return fixtureElement();
}
static AXUIElementRef findPlaybackButton(AXUIElementRef a,CGPoint b,int c,int*d,double deadline) {
 return fixtureElement();
}
static double elementWidth(AXUIElementRef a) { return 624; }
static int elementCenter(AXUIElementRef a,CGPoint*b) { *b=CGPointZero; return 1; }
static AXError fixtureAction(AXUIElementRef a,CFStringRef b) { return actionResult; }
static int postPlayerSingleClick(pid_t pid,double x,double y) { clicks++; return 1; }
static int revealPlayerControlBar(pid_t pid) { reveals++; hidden=0; return 1; }
#define AXUIElementCreateApplication fixtureApp
#define AXUIElementPerformAction fixtureAction
` + text[start:end] + `
int main(void) {
 hidden=1; reveals=0; clicks=0; actionResult=kAXErrorSuccess;
 int result=togglePlayerPlayback(1);
 if(result!=1 || reveals!=1 || clicks!=0) {
  fprintf(stderr,"hidden bar was not paused after reveal: result=%d reveals=%d clicks=%d\n",result,reveals,clicks);
  return 1;
 }
 hidden=0; reveals=0; clicks=0;
 result=togglePlayerPlayback(1);
 if(result!=1 || reveals!=0 || clicks!=0) {
  fprintf(stderr,"visible bar still revealed controls: result=%d reveals=%d clicks=%d\n",result,reveals,clicks);
  return 2;
 }
 return 0;
}
`
	dir := t.TempDir()
	file, binary := filepath.Join(dir, "reveal.c"), filepath.Join(dir, "reveal")
	if err := os.WriteFile(file, []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("xcrun", "clang", file, "-framework", "ApplicationServices", "-o", binary).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	if out, err := exec.Command(binary).CombinedOutput(); err != nil {
		t.Fatalf("hidden control bar reveal: %v\n%s", err, out)
	}
}

func TestNativeRevealControlBarPostsHoverNotClick(t *testing.T) {
	source, err := os.ReadFile("automation_darwin.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	start, end := strings.Index(text, "static int revealPlayerControlBar(pid_t pid) {"), strings.Index(text, "static int setPlayerFullscreen(")
	if start < 0 || end <= start {
		t.Fatal("native reveal not found")
	}
	fixture := `#include <ApplicationServices/ApplicationServices.h>
#include <stdio.h>
#include <unistd.h>
static int hovers, fullscreenHovers, clicks, minimized, fullscreen, offscreen;
static AXUIElementRef fixtureElement(void) { return (AXUIElementRef)CFRetain(kCFBooleanTrue); }
static AXUIElementRef fixtureApp(pid_t pid) { return fixtureElement(); }
static AXUIElementRef copyPlaybackWindow(AXUIElementRef app) { return fixtureElement(); }
static double elementWidth(AXUIElementRef a) { return 800; }
static int elementCenter(AXUIElementRef a,CGPoint*b) { *b=CGPointMake(400, 300); return 1; }
static int copyBool(AXUIElementRef a, CFStringRef b, int *out) {
 *out = 0;
 if (b == kAXMinimizedAttribute) *out = minimized;
 else *out = fullscreen;
 return 1;
}
static int postPlayerSingleClick(pid_t pid,double x,double y) { clicks++; return 1; }
static int postPlayerMouseMove(pid_t pid,double x,double y) { hovers++; return offscreen ? -3 : 1; }
static int postPlayerFullscreenMouseMove(pid_t pid,double x,double y) { fullscreenHovers++; return 1; }
#define AXUIElementCreateApplication fixtureApp
` + text[start:end] + `
int main(void) {
 hovers=0; fullscreenHovers=0; clicks=0; minimized=0; fullscreen=0; offscreen=0;
 if(revealPlayerControlBar(1)!=1 || hovers!=1 || fullscreenHovers || clicks) {
  fprintf(stderr,"windowed reveal: hovers=%d fs=%d clicks=%d\n",hovers,fullscreenHovers,clicks); return 1;
 }
 hovers=0; fullscreenHovers=0; clicks=0; fullscreen=1;
 if(revealPlayerControlBar(1)!=1 || hovers || fullscreenHovers!=1 || clicks) {
  fprintf(stderr,"fullscreen reveal: hovers=%d fs=%d clicks=%d\n",hovers,fullscreenHovers,clicks); return 2;
 }
 hovers=0; fullscreenHovers=0; clicks=0; fullscreen=0; offscreen=1;
 if(revealPlayerControlBar(1)!=1 || hovers!=1 || fullscreenHovers!=1 || clicks) {
  fprintf(stderr,"other-space reveal: hovers=%d fs=%d clicks=%d\n",hovers,fullscreenHovers,clicks); return 3;
 }
 hovers=0; fullscreenHovers=0; clicks=0; minimized=1; offscreen=0;
 if(revealPlayerControlBar(1)!=-6 || hovers || fullscreenHovers || clicks) {
  fprintf(stderr,"minimized reveal posted input\n"); return 4;
 }
 return 0;
}
`
	dir := t.TempDir()
	file, binary := filepath.Join(dir, "hover.c"), filepath.Join(dir, "hover")
	if err := os.WriteFile(file, []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("xcrun", "clang", file, "-framework", "ApplicationServices", "-o", binary).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	if out, err := exec.Command(binary).CombinedOutput(); err != nil {
		t.Fatalf("control bar hover: %v\n%s", err, out)
	}
}
