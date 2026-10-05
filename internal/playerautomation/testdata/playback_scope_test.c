#include <ApplicationServices/ApplicationServices.h>
#include <math.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

static double clockNow;
static int courseReads;
static CFMutableDictionaryRef app, courseOutline, row, slider, button;
static CFMutableDictionaryRef node(CFStringRef role) {
    CFMutableDictionaryRef result = CFDictionaryCreateMutable(NULL, 0,
        &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
    CFDictionarySetValue(result, kAXRoleAttribute, role);
    return result;
}
static void children(CFMutableDictionaryRef parent, const void **values, int count) {
    CFArrayRef array = CFArrayCreate(NULL, values, count, &kCFTypeArrayCallBacks);
    CFDictionarySetValue(parent, kAXChildrenAttribute, array);
    CFRelease(array);
}
static AXError fixtureCopy(AXUIElementRef element, CFStringRef attribute, CFTypeRef *out) {
    clockNow += 0.002;
    if (element == (AXUIElementRef)row) courseReads++;
    CFTypeRef value = CFDictionaryGetValue((CFDictionaryRef)element, attribute);
    *out = value ? CFRetain(value) : NULL;
    return value ? kAXErrorSuccess : kAXErrorNoValue;
}
static AXUIElementRef fixtureApp(pid_t pid) { return (AXUIElementRef)CFRetain(app); }
static int fixtureClock(clockid_t id, struct timespec *out) {
    out->tv_sec = (time_t)clockNow;
    out->tv_nsec = (long)((clockNow - out->tv_sec) * 1e9);
    return 0;
}
static int supportsPress(AXUIElementRef element) { return element == (AXUIElementRef)button; }
static int hasRole(AXUIElementRef element, const char *role) {
    CFTypeRef value = NULL;
    fixtureCopy(element, kAXRoleAttribute, &value);
    char text[128] = {0};
    CFStringGetCString((CFStringRef)value, text, sizeof(text), kCFStringEncodingUTF8);
    CFRelease(value);
    return strcmp(text, role) == 0;
}
static int elementCenter(AXUIElementRef element, CGPoint *point) { *point = CGPointZero; return 1; }
#define AXUIElementCopyAttributeValue fixtureCopy
#define AXUIElementCreateApplication fixtureApp
#define AXUIElementSetMessagingTimeout(a,b) kAXErrorSuccess
#define clock_gettime fixtureClock
static double monotonicSeconds(void);
/* PRODUCTION_READERS */
/* PRODUCTION_CONTROLS */

int main(void) {
    app = node(CFSTR("AXApplication")); courseOutline = node(CFSTR("AXOutline"));
    row = node(CFSTR("AXRow")); slider = node(CFSTR("AXSlider")); button = node(CFSTR("AXButton"));
    CFDictionarySetValue(row, kAXValueAttribute, CFSTR("course-sentinel"));
    CFDictionarySetValue(slider, kAXIdentifierAttribute, CFSTR("_NS:26"));
    CFDictionarySetValue(slider, kAXValueAttribute, CFSTR("100"));
    CFMutableDictionaryRef clock = node(CFSTR("AXStaticText"));
    CFDictionarySetValue(clock, kAXIdentifierAttribute, CFSTR("_NS:8"));
    CFDictionarySetValue(clock, kAXValueAttribute, CFSTR("00:28:06/00:28:06"));
    CFMutableDictionaryRef alert = node(CFSTR("AXSheet"));
    CFDictionarySetValue(alert, kAXTitleAttribute, CFSTR("playback-error-sentinel"));
    const void *rows[512]; for (int i=0;i<512;i++) rows[i]=row;
    children(courseOutline, rows, 512);
    CFMutableDictionaryRef menuBar = node(CFSTR("AXMenuBar"));
    children(menuBar, rows, 512);
    const void *ordered[] = {courseOutline, menuBar, slider, clock, button, alert};
    children(app, ordered, 6);
    int failed = 0;
    clockNow=0; courseReads=0; int visited=0;
    AXUIElementRef found = findPlaybackSlider((AXUIElementRef)app, 0, &visited, 2.0);
    if (found != (AXUIElementRef)slider || courseReads != 0) {
        fprintf(stderr,"slider starved by course tree: found=%d courseReads=%d time=%.3f\n",found!=NULL,courseReads,clockNow); failed=1;
    }
    if (found) CFRelease(found);
    clockNow=0; courseReads=0; visited=0;
    found = findPlaybackButton((AXUIElementRef)app, CGPointZero, 0, &visited, 2.0);
    if (found != (AXUIElementRef)button || courseReads != 0) {
        fprintf(stderr,"pause button starved by course tree: found=%d courseReads=%d time=%.3f\n",found!=NULL,courseReads,clockNow); failed=1;
    }
    if (found) CFRelease(found);
    clockNow=0; courseReads=0; int incomplete=0;
    char *snapshot = PLAYBACK_SNAPSHOT;
    if (!snapshot || incomplete || courseReads || !strstr(snapshot,"_NS:26") ||
        !strstr(snapshot,"00:28:06/00:28:06") || !strstr(snapshot,"playback-error-sentinel")) {
        fprintf(stderr,"playback snapshot starved: incomplete=%d courseReads=%d time=%.3f\n",incomplete,courseReads,clockNow); failed=1;
    }
    free(snapshot);
    // Course discovery retains the complete reader rather than being pruned.
    children(courseOutline, rows, 1); children(menuBar, NULL, 0);
    clockNow=0; courseReads=0; incomplete=0;
    snapshot = FULL_SNAPSHOT;
    if (!snapshot || incomplete || !courseReads || !strstr(snapshot,"course-sentinel")) return 4;
    free(snapshot);
    // A genuinely incomplete playback subtree must still report uncertainty.
    CFMutableDictionaryRef busy = node(CFSTR("AXGroup")); children(busy, rows, 512);
    const void *blocked[] = {slider, busy}; children(app, blocked, 2);
    clockNow=0; incomplete=0;
    snapshot = PLAYBACK_SNAPSHOT;
    if (!snapshot || !incomplete) return 5;
    free(snapshot);
    // Absence remains absence: pruning must never invent a button or progress.
    const void *absent[] = {courseOutline, alert}; children(app, absent, 2);
    clockNow=0; visited=0;
    found = findPlaybackSlider((AXUIElementRef)app, 0, &visited, 2.0);
    if (found) { CFRelease(found); return 2; }
    clockNow=0; visited=0;
    found = findPlaybackButton((AXUIElementRef)app, CGPointZero, 0, &visited, 2.0);
    if (found) { CFRelease(found); return 3; }
    return failed;
}
