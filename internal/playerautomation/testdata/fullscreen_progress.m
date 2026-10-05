#import <AppKit/AppKit.h>
#include <ApplicationServices/ApplicationServices.h>
#include <stdio.h>

static BOOL onCurrentSpace = NO;
static BOOL hasWindow = YES;
static int posted = 0;
static CGWindowListOption observedOption;
static CFArrayRef fixtureWindows(CGWindowListOption option, CGWindowID relative) {
    observedOption = option;
    if (!hasWindow || (!onCurrentSpace && (option & kCGWindowListOptionOnScreenOnly))) {
        return CFArrayCreate(NULL, NULL, 0, &kCFTypeArrayCallBacks);
    }
    CGRect bounds = CGRectMake(-1440, 0, 1440, 900);
    CFDictionaryRef rect = CGRectCreateDictionaryRepresentation(bounds);
    NSDictionary *window = @{
        (id)kCGWindowOwnerPID: @4242,
        (id)kCGWindowLayer: @0,
        (id)kCGWindowNumber: @31415,
        (id)kCGWindowBounds: (id)rect,
        (id)kCGWindowIsOnscreen: @(onCurrentSpace)
    };
    CFRelease(rect);
    return CFBridgingRetain(@[window]);
}
static void fixturePost(pid_t pid, CGEventRef event) {
    if (pid == 4242 && CGEventGetType(event) == kCGEventMouseMoved &&
        CGEventGetIntegerValueField(event, kCGMouseEventWindowUnderMousePointer) == 31415 &&
        CGEventGetIntegerValueField(event, kCGMouseEventClickState) == 0) posted++;
}
#define CGWindowListCopyWindowInfo fixtureWindows
#define CGEventPostToPid fixturePost
#import "../mouse_darwin.m"

int main(void) {
    @autoreleasepool {
        int result = postPlayerFullscreenMouseMove(4242, -1080, 450);
        if (result != 1 || posted != 1 || observedOption != kCGWindowListOptionAll) {
            fprintf(stderr, "fullscreen in another Space rejected: result=%d posted=%d option=%u\n", result, posted, observedOption);
            return 1;
        }
        posted = 0;
        if (postPlayerMouseMove(4242, -1080, 450) != -3 || posted ||
            observedOption != kCGWindowListOptionOnScreenOnly) return 2;
        onCurrentSpace = YES;
        if (postPlayerMouseMove(4242, -1080, 450) != 1 || posted != 1) return 3;
        posted = 0;
        if (postPlayerFullscreenMouseMove(1234, -1080, 450) != -3 || posted) return 4;
        if (postPlayerFullscreenMouseMove(4242, 100, 450) != -3 || posted) return 5;
        hasWindow = NO;
        if (postPlayerFullscreenMouseMove(4242, -1080, 450) != -3 || posted) return 6;
        puts("fullscreen refresh crosses Spaces; normal refresh, PID and bounds remain constrained");
    }
    return 0;
}
