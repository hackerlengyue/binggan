#import "../mouse_darwin.m"
#include <stdio.h>
#include <string.h>

int main(int argc, char **argv) {
    @autoreleasepool {
        WindowLocationSetter setter = (WindowLocationSetter)dlsym(RTLD_DEFAULT, "CGEventSetWindowLocation");
        if (!setter) return 77;
        CGPoint point = CGPointMake(684, -722);
        CGRect bounds = CGRectMake(-199, -995, 1000, 600);
        BOOL legacy = argc > 1 && strcmp(argv[1], "--legacy") == 0;
        for (int click = 1; click <= 2; click++) {
            for (int up = 0; up <= 1; up++) {
                CGEventRef event;
                if (legacy) {
                    event = CGEventCreateMouseEvent(NULL, up ? kCGEventLeftMouseUp : kCGEventLeftMouseDown, point, kCGMouseButtonLeft);
                    CGEventSetIntegerValueField(event, kCGMouseEventClickState, click);
                } else {
                    event = playerMouseEvent(point, bounds, 31415, click, up, NO, setter);
                }
                if (!event) return 1;
                NSEvent *decoded = [NSEvent eventWithCGEvent:event];
                if (decoded.windowNumber != 31415) {
                    fprintf(stderr, "missing AppKit window identity: %ld\n", (long)decoded.windowNumber);
                    CFRelease(event);
                    return 2;
                }
                CGPoint screen = CGEventGetLocation(event);
                BOOL valid = decoded.clickCount == click &&
                    decoded.type == (up ? NSEventTypeLeftMouseUp : NSEventTypeLeftMouseDown) &&
                    CGEventGetIntegerValueField(event, kCGMouseEventWindowUnderMousePointer) == 31415 &&
                    CGEventGetIntegerValueField(event, kCGMouseEventWindowUnderMousePointerThatCanHandleThisEvent) == 31415 &&
                    screen.x == point.x && screen.y == point.y;
                CFRelease(event);
                if (!valid) return 3;
            }
        }
        CGEventRef hover = playerMouseEvent(point, bounds, 31415, 0, 0, YES, setter);
        if (!hover) return 4;
        NSEvent *move = [NSEvent eventWithCGEvent:hover];
        CGPoint screen = CGEventGetLocation(hover);
        BOOL validMove = move.type == NSEventTypeMouseMoved &&
            CGEventGetType(hover) == kCGEventMouseMoved &&
            move.windowNumber == 31415 && move.clickCount == 0 &&
            move.pressure == 0 && CGEventGetFlags(hover) == 0 &&
            CGEventGetIntegerValueField(hover, kCGMouseEventWindowUnderMousePointer) == 31415 &&
            CGEventGetIntegerValueField(hover, kCGMouseEventWindowUnderMousePointerThatCanHandleThisEvent) == 31415 &&
            screen.x == point.x && screen.y == point.y;
        CFRelease(hover);
        if (!validMove) {
            fprintf(stderr, "progress refresh must carry a window-targeted move, never a click\n");
            return 5;
        }
        puts("native double-click carries AppKit window identity, click count and screen position");
        puts("progress refresh carries a window-targeted mouse move without click or modifiers");
        return 0;
    }
}
