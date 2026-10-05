//go:build darwin && cgo

package playerautomation

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeSnapshotDoesNotRoundFinalFrameToCompletion(t *testing.T) {
	source, err := os.ReadFile("automation_darwin.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	start, end := strings.Index(text, "static int copyString("), strings.Index(text, "static void clean(")
	if start < 0 || end <= start {
		t.Fatal("native numeric reader not found")
	}
	// Compile the production reader with a synthetic numeric AX attribute. This
	// exercises its actual CFNumber serialization, not a second Go implementation.
	fixture := `#include <ApplicationServices/ApplicationServices.h>
#include <stdio.h>
#include <stdlib.h>
static double progress;
static AXError fixtureValue(AXUIElementRef element, CFStringRef attribute, CFTypeRef *value) {
 *value = CFNumberCreate(NULL,kCFNumberDoubleType,&progress); return kAXErrorSuccess;
}
#define AXUIElementCopyAttributeValue fixtureValue
` + text[start:end] + `
int main(void) {
 double values[] = {99.9996, 99.999999999, 100.0, 0.125};
 for (int i=0;i<4;i++) {
  progress=values[i]; char buffer[128]={0};
  if (!copyString(NULL,NULL,buffer,sizeof(buffer))) return 2;
  if (strtod(buffer,NULL) != progress) {
   fprintf(stderr,"progress rounded: %.17g became %s\n",progress,buffer); return 1;
  }
 }
 return 0;
}
`
	dir := t.TempDir()
	file, binary := filepath.Join(dir, "numeric.c"), filepath.Join(dir, "numeric")
	if err := os.WriteFile(file, []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("xcrun", "clang", file, "-framework", "ApplicationServices", "-o", binary).CombinedOutput(); err != nil {
		t.Fatalf("build AX number fixture: %v\n%s", err, out)
	}
	if out, err := exec.Command(binary).CombinedOutput(); err != nil {
		t.Fatalf("AX progress precision: %v\n%s", err, out)
	}
}

func TestNativeVolumeRejectsUnappliedValue(t *testing.T) {
	source, err := os.ReadFile("automation_darwin.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	start, end := strings.Index(text, "static int setPlayerVolume("), strings.Index(text, "static int pressPlayerButton(")
	if start < 0 || end <= start {
		t.Fatal("native volume setter not found")
	}
	fixture := `#include <ApplicationServices/ApplicationServices.h>
#include <math.h>
#include <stdio.h>
#include <unistd.h>
static double monotonicSeconds(void) { return 0; }
static double current, requested;
static int reads, applies;
static AXUIElementRef fixtureApp(pid_t pid) { return (AXUIElementRef)CFRetain(kCFBooleanTrue); }
static AXUIElementRef findVolumeSlider(AXUIElementRef a,int b,int*c,double deadline) { return fixtureApp(1); }
static AXError fixtureSet(AXUIElementRef e,CFStringRef a,CFTypeRef v) {
 CFNumberGetValue(v,kCFNumberDoubleType,&requested);return kAXErrorSuccess;
}
static AXError fixtureGet(AXUIElementRef e,CFStringRef a,CFTypeRef *v) {
 if(applies && ++reads>=2) current=requested;
 *v=CFNumberCreate(NULL,kCFNumberDoubleType,&current);return kAXErrorSuccess;
}
static int fixtureSleep(useconds_t t) { return 0; }
#define AXUIElementCreateApplication fixtureApp
#define AXUIElementSetAttributeValue fixtureSet
#define AXUIElementCopyAttributeValue fixtureGet
#define usleep fixtureSleep
#define AXUIElementSetMessagingTimeout(a,b) kAXErrorSuccess
` + text[start:end] + `
int main(void) {
 current=90;applies=0;reads=0;
 if(setPlayerVolume(1,5)==1) { fprintf(stderr,"acknowledged 5 but slider remains 90\n");return 1; }
 current=90;applies=1;reads=0;
 if(setPlayerVolume(1,5)!=1 || current!=5) { fprintf(stderr,"delayed matching value was not verified\n");return 2; }
 current=0;applies=1;reads=0;
 if(setPlayerVolume(1,0)!=1 || current!=0) return 3;
 return 0;
}
`
	dir := t.TempDir()
	file, binary := filepath.Join(dir, "volume.c"), filepath.Join(dir, "volume")
	if err := os.WriteFile(file, []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("xcrun", "clang", file, "-framework", "ApplicationServices", "-o", binary).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	if out, err := exec.Command(binary).CombinedOutput(); err != nil {
		t.Fatalf("volume verification: %v\n%s", err, out)
	}
}

func TestNativeControlSearchesRespectTimeBudget(t *testing.T) {
	source, err := os.ReadFile("automation_darwin.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	extract := func(start, end string) string {
		a, b := strings.Index(text, start), strings.Index(text, end)
		if a < 0 || b <= a {
			t.Fatalf("missing native helper %s", start)
		}
		return text[a:b]
	}
	element := extract("static AXUIElementRef findElement(", "static int hasRole(")
	volume := extract("static AXUIElementRef findVolumeSlider(", "static int setPlayerVolume(")
	rate := extract("static AXUIElementRef findPlayerRateMenuItem(", "static AXUIElementRef findPlayerRateMenuItemInAttribute(")
	button := extract("static AXUIElementRef findPlaybackButton(", "static AXUIElementRef findPlaybackSlider(")
	excluded := extract("static int isPlaybackExcludedRole(", "static int walkSnapshot(")
	deepest := extract("static AXUIElementRef findDeepestTextElement(", "static int pointWithinCourseRow(")
	call := func(body, expression string) string {
		if strings.Contains(strings.Split(body, "{")[0], "deadline") {
			return expression + ",2.0)"
		}
		return expression + ")"
	}
	fixture := `#include <ApplicationServices/ApplicationServices.h>
#include <stdio.h>
#include <math.h>
static double clockNow;
static int calls, configured;
static double monotonicSeconds(void) { return clockNow; }
static AXError fixtureTimeout(AXUIElementRef e,float seconds) { configured++; return kAXErrorSuccess; }
static AXError fixtureChildren(AXUIElementRef e,CFStringRef a,CFTypeRef *v) {
 calls++; clockNow+=0.2;
 const void* children[]={kCFBooleanTrue,kCFBooleanTrue,kCFBooleanTrue};
 *v=CFArrayCreate(NULL,children,3,&kCFTypeArrayCallBacks); return kAXErrorSuccess;
}
static int containsText(AXUIElementRef a,const char*b,int c) { return 0; }
static int supportsPress(AXUIElementRef a) { return 0; }
static int hasRole(AXUIElementRef a,const char*b) { return 0; }
static int copyString(AXUIElementRef a,CFStringRef b,char*c,size_t d) { return 0; }
static double elementWidth(AXUIElementRef a) { return 0; }
static int elementCenter(AXUIElementRef a,CGPoint*b) { return 0; }
#define AXUIElementCopyAttributeValue fixtureChildren
#define AXUIElementSetMessagingTimeout fixtureTimeout
` + excluded + element + volume + rate + button + deepest + `
int main(void) {
 AXUIElementRef root=(AXUIElementRef)kCFBooleanTrue;
 for(int method=0;method<5;method++) {
  calls=0;configured=0;clockNow=0;int visited=0;AXUIElementRef result=NULL;
  if(method==0) result=` + call(element, `findElement(root,"absent",1,1,0,&visited`) + `;
  if(method==1) result=` + call(volume, `findVolumeSlider(root,0,&visited`) + `;
  if(method==2) result=` + call(rate, `findPlayerRateMenuItem(root,"absent",0,&visited`) + `;
  if(method==3) result=` + call(button, `findPlaybackButton(root,CGPointZero,0,&visited`) + `;
  if(method==4) result=` + call(deepest, `findDeepestTextElement(root,"absent",0,&visited`) + `;
  if(result || calls>12 || calls<1 || configured<calls) {
   fprintf(stderr,"search %d exceeded bounded AX work: calls=%d configured=%d time=%f\n",method,calls,configured,clockNow);return 1;
  }
 }
 return 0;
}`
	dir := t.TempDir()
	file, binary := filepath.Join(dir, "budget.c"), filepath.Join(dir, "budget")
	if err := os.WriteFile(file, []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("xcrun", "clang", file, "-framework", "ApplicationServices", "-o", binary).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	if out, err := exec.Command(binary).CombinedOutput(); err != nil {
		t.Fatalf("control query budget: %v\n%s", err, out)
	}
}
