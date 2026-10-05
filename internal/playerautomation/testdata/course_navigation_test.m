// Compile the production navigator against a deterministic public-AX transport.
#import <Foundation/Foundation.h>
#include <ApplicationServices/ApplicationServices.h>
#include <assert.h>
static NSMutableDictionary *app, *testOutline, *area, *bar, *hit, *expandRow;
static NSMutableArray *rows;
static int clicks, presses, scrolls;
static BOOL blocked, failRead, uncertainPress, autoExpand, brokenDisclosedRows;
static NSMutableDictionary *node(NSString *role) { return [NSMutableDictionary dictionaryWithObject:role forKey:@"AXRole"]; }
static void frame(NSMutableDictionary *n, double y, double height) {
    CGPoint p = CGPointMake(0, y); CGSize s = CGSizeMake(200, height);
    n[@"AXPosition"] = [(id)AXValueCreate(kAXValueCGPointType, &p) autorelease];
    n[@"AXSize"] = [(id)AXValueCreate(kAXValueCGSizeType, &s) autorelease];
}
static AXUIElementRef mockApp(pid_t pid) { return (AXUIElementRef)CFRetain((CFTypeRef)app); }
static AXError mockTimeout(AXUIElementRef e, float seconds) { return kAXErrorSuccess; }
static AXError mockCopy(AXUIElementRef e, CFStringRef a, CFTypeRef *v) {
    if (failRead) return kAXErrorCannotComplete;
    if (brokenDisclosedRows && CFEqual(a, kAXDisclosedRowsAttribute)) return kAXErrorFailure;
    id value = [(NSDictionary *)e objectForKey:(NSString *)a];
    if (!value) return kAXErrorAttributeUnsupported;
    *v = CFRetain((CFTypeRef)value); return kAXErrorSuccess;
}
static AXError mockHit(AXUIElementRef e, float x, float y, AXUIElementRef *v) {
    id found = blocked ? app : hit;
    *v = (AXUIElementRef)CFRetain((CFTypeRef)found); return kAXErrorSuccess;
}
static AXError mockSettable(AXUIElementRef e, CFStringRef a, Boolean *v) { *v = true; return kAXErrorSuccess; }
static AXError mockSet(AXUIElementRef e, CFStringRef a, CFTypeRef v) {
    scrolls++; [(NSMutableDictionary *)e setObject:(id)v forKey:(NSString *)a];
    double offset = [(NSNumber *)v doubleValue] * 400;
    for (NSMutableDictionary *r in rows) frame(r, [r[@"index"] doubleValue] * 40 - offset, 30);
    return kAXErrorSuccess;
}
static AXError mockPress(AXUIElementRef e, CFStringRef action) {
    presses++;
    if (autoExpand) {
        expandRow[@"AXDisclosedRows"] = @[@{}];
        NSMutableDictionary *child = node(@"AXRow"); child[@"AXDisclosureLevel"] = @1;
        child[@"AXValue"] = @"child"; frame(child, 40, 30); [rows addObject:child];
    }
    return uncertainPress ? kAXErrorCannotComplete : kAXErrorSuccess;
}
#define AXUIElementCreateApplication mockApp
#define AXUIElementSetMessagingTimeout mockTimeout
#define AXUIElementCopyAttributeValue mockCopy
#define AXUIElementCopyElementAtPosition mockHit
#define AXUIElementIsAttributeSettable mockSettable
#define AXUIElementSetAttributeValue mockSet
#define AXUIElementPerformAction mockPress
#define AXUIElementGetTypeID CFDictionaryGetTypeID
#include "../course_navigation_darwin.m"
int postPlayerDoubleClick(pid_t pid, double x, double y) { clicks++; return 1; }
static NSMutableDictionary *row(NSString *title, int level, int index) {
    NSMutableDictionary *r = node(@"AXRow"); r[@"AXValue"] = title;
    r[@"AXDisclosureLevel"] = @(level); r[@"index"] = @(index);
    frame(r, index * 40, 30); [rows addObject:r]; return r;
}
static void setup(void) {
    clicks = presses = scrolls = 0; blocked = failRead = uncertainPress = autoExpand = NO;
    app = node(@"AXApplication"); testOutline = node(@"AXOutline"); area = node(@"AXScrollArea"); bar = node(@"AXScrollBar");
    rows = [NSMutableArray array]; app[@"AXChildren"] = @[area]; area[@"AXChildren"] = @[testOutline];
    testOutline[@"AXRows"] = rows; testOutline[@"AXParent"] = area;
    area[@"AXVerticalScrollBar"] = bar; bar[@"AXValue"] = @0; bar[@"AXMinValue"] = @0; bar[@"AXMaxValue"] = @1;
    frame(area, 0, 100);
}
int main(void) { @autoreleasepool {
    setup(); expandRow = row(@"folder", 0, 0); hit = row(@"lesson .sz", 1, 1);
    brokenDisclosedRows = YES;
    assert(revealPlayerCourse(1, "[\"folder\",\"lesson\"]") == 1);
    assert(presses == 0 && clicks == 0);
    brokenDisclosedRows = NO;
    setup(); row(@"A", 0, 0); row(@"lesson", 1, 1); row(@"B", 0, 2); hit = row(@"lesson", 1, 3);
    assert(clickPlayerCourse(1, "[\"B\",\"lesson\"]") == NavNotVisible); assert(clicks == 0);
    assert(navLocate(1, @[@"B", @"lesson"], NO, NO, navNow()+2) == 1); assert(scrolls > 0); assert(clicks == 0);
    assert(clickPlayerCourse(1, "[\"B\",\"lesson\"]") == 1); assert(clicks == 1);
    // Reordering with the same label count must follow the new full path.
    [rows exchangeObjectAtIndex:0 withObjectAtIndex:2]; hit = rows[1]; frame(hit, 0, 30);
    assert(clickPlayerCourse(1, "[\"B\",\"lesson\"]") == 1); assert(clicks == 2);
    blocked = YES; assert(clickPlayerCourse(1, "[\"B\",\"lesson\"]") == NavBlocked); assert(clicks == 2);
    blocked = NO; failRead = YES; assert(clickPlayerCourse(1, "[\"B\",\"lesson\"]") == NavIncomplete); assert(clicks == 2);
    setup(); row(@"A", 0, 0); row(@"lesson", 1, 1); row(@"A", 0, 2); hit = row(@"lesson", 1, 3);
    assert(clickPlayerCourse(1, "[\"A\",\"lesson\"]") == NavAmbiguous); assert(clicks == 0);
    setup(); expandRow = row(@"folder", 0, 0); expandRow[@"AXChildren"] = @[node(@"AXDisclosureTriangle")];
    expandRow[@"AXDisclosedRows"] = @[]; expandRow[@"AXDisclosing"] = @YES;
    autoExpand = YES; assert(navLocate(1, @[@"folder"], YES, NO, navNow()+2) == 1); assert(presses == 1);
    assert(navLocate(1, @[@"folder"], YES, NO, navNow()+2) == 1); assert(presses == 1);
    [rows removeLastObject];
    expandRow[@"AXDisclosedRows"] = @[]; autoExpand = NO; uncertainPress = YES;
    assert(navLocate(1, @[@"folder"], YES, NO, navNow()+2) == NavUncertain); assert(presses == 2);
    uncertainPress = NO; assert(navLocate(1, @[@"folder"], YES, NO, navNow()+0.35) == NavNotVisible); assert(presses == 3);
    assert(clicks == 0);
    setup(); hit = row(@"第一 · 讲 30分钟", 0, 0);
    NSMutableDictionary *label = node(@"AXStaticText"); frame(label, 10, 20);
    frame(hit, 0, 0); hit[@"AXChildren"] = @[label];
    assert(clickPlayerCourse(1, "[\"第一讲\"]") == 1); assert(clicks == 1);
    hit[@"AXValue"] = @"第一讲 补充";
    assert(clickPlayerCourse(1, "[\"第一讲\"]") == NavMissing); assert(clicks == 1);
    puts("native course navigation: passed");
} return 0; }
