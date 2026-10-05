//go:build darwin && cgo

#import <AppKit/AppKit.h>
#include <ApplicationServices/ApplicationServices.h>
#include <dlfcn.h>
#include <unistd.h>

typedef void (*WindowLocationSetter)(CGEventRef, CGPoint);

// NSEvent supplies the AppKit window identity and event metadata that a bare
// CGEventCreateMouseEvent lacks. Both screen and window coordinates are needed
// when posting directly to a process, including windows on a secondary display.
static CGEventRef playerMouseEvent(CGPoint point, CGRect bounds, NSInteger windowID,
                                  int click, int up, BOOL active,
                                  WindowLocationSetter setWindowLocation) {
    CGPoint local = CGPointMake(point.x - bounds.origin.x, point.y - bounds.origin.y);
    NSEventType type = click == 0 ? NSEventTypeMouseMoved :
        (up ? NSEventTypeLeftMouseUp : NSEventTypeLeftMouseDown);
    NSEvent *native = [NSEvent mouseEventWithType:type
                                       location:NSMakePoint(local.x, local.y)
                                  modifierFlags:0
                                      timestamp:NSProcessInfo.processInfo.systemUptime
                                   windowNumber:windowID context:nil eventNumber:click
                                     clickCount:click pressure:(up || click == 0) ? 0 : 1];
    CGEventRef event = native.CGEvent ? CGEventCreateCopy(native.CGEvent) : NULL;
    if (!event) return NULL;
    CGEventSetIntegerValueField(event, kCGMouseEventButtonNumber, kCGMouseButtonLeft);
    CGEventSetIntegerValueField(event, kCGMouseEventSubtype, 3);
    CGEventSetIntegerValueField(event, kCGMouseEventWindowUnderMousePointer, windowID);
    CGEventSetIntegerValueField(event, kCGMouseEventWindowUnderMousePointerThatCanHandleThisEvent, windowID);
    CGEventSetLocation(event, point);
    setWindowLocation(event, local);
    CGEventSetFlags(event, active ? 0 : kCGEventFlagMaskCommand);
    return event;
}

static int postPlayerMouseEvents(pid_t pid, double x, double y, int count, BOOL fullscreen) {
    @autoreleasepool {
        CGPoint point = CGPointMake(x, y);
        CGRect bounds = CGRectZero;
        NSInteger windowID = 0;
        // Fullscreen lives in its own Space. Process-directed hover does not
        // require that Space to be active; normal clicks keep the visible-only gate.
        CGWindowListOption option = fullscreen && count == 0 ?
            kCGWindowListOptionAll : kCGWindowListOptionOnScreenOnly;
        CFArrayRef windows = CGWindowListCopyWindowInfo(option, kCGNullWindowID);
        if (!windows) return -3;
        for (CFIndex i = 0; i < CFArrayGetCount(windows); i++) {
            CFDictionaryRef window = CFArrayGetValueAtIndex(windows, i);
            int owner = 0, layer = 0, number = 0;
            CFNumberRef ownerValue = CFDictionaryGetValue(window, kCGWindowOwnerPID);
            CFNumberRef layerValue = CFDictionaryGetValue(window, kCGWindowLayer);
            CFNumberRef numberValue = CFDictionaryGetValue(window, kCGWindowNumber);
            if (!ownerValue || !layerValue || !numberValue) continue;
            CFNumberGetValue(ownerValue, kCFNumberIntType, &owner);
            CFNumberGetValue(layerValue, kCFNumberIntType, &layer);
            if (owner != pid || layer != 0) continue;
            CGRect candidate;
            CFDictionaryRef rect = CFDictionaryGetValue(window, kCGWindowBounds);
            if (!rect || !CGRectMakeWithDictionaryRepresentation(rect, &candidate) ||
                !CGRectContainsPoint(candidate, point)) continue;
            CFNumberGetValue(numberValue, kCFNumberIntType, &number);
            windowID = number;
            bounds = candidate;
            break;
        }
        CFRelease(windows);
        if (!windowID) return -3;
        WindowLocationSetter setWindowLocation = (WindowLocationSetter)dlsym(RTLD_DEFAULT, "CGEventSetWindowLocation");
        if (!setWindowLocation) return -4;
        if (count == 0) {
            CGEventRef event = playerMouseEvent(point, bounds, windowID, 0, 0, YES, setWindowLocation);
            if (!event) return 0;
            CGEventPostToPid(pid, event);
            CFRelease(event);
            return 1;
        }
        for (int click = 1; click <= count; click++) {
            for (int up = 0; up <= 1; up++) {
                CGEventRef event = playerMouseEvent(point, bounds, windowID, click, up, YES, setWindowLocation);
                if (!event) return 0;
                // Process-directed delivery never moves or captures the global pointer.
                CGEventPostToPid(pid, event);
                CFRelease(event);
                usleep(up ? 90000 : 50000);
            }
        }
        return 1;
    }
}

int postPlayerDoubleClick(pid_t pid, double x, double y) { return postPlayerMouseEvents(pid, x, y, 2, NO); }
int postPlayerSingleClick(pid_t pid, double x, double y) { return postPlayerMouseEvents(pid, x, y, 1, NO); }
int postPlayerMouseMove(pid_t pid, double x, double y) { return postPlayerMouseEvents(pid, x, y, 0, NO); }

int postPlayerFullscreenMouseMove(pid_t pid, double x, double y) { return postPlayerMouseEvents(pid, x, y, 0, YES); }
