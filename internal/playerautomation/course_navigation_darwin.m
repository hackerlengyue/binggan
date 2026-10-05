//go:build darwin && cgo

#import <Foundation/Foundation.h>
#include <ApplicationServices/ApplicationServices.h>
#include <time.h>
#include <math.h>
#include <unistd.h>

int postPlayerDoubleClick(pid_t pid, double x, double y);

// A locator is a full outline path, never an ordinal retained across UI changes.
// Scroll bars are operated through their public AXValue attribute, just like
// dragging the thumb. No document opening, process injection or private API.
enum { NavMissing = -20, NavAmbiguous = -21, NavIncomplete = -22,
       NavScrollUnsupported = -23, NavNotVisible = -24, NavUncertain = -25,
       NavBlocked = -26 };
typedef struct { double deadline; int visited; int failed; } NavRead;

static double navNow(void) {
    struct timespec now; clock_gettime(CLOCK_MONOTONIC, &now);
    return now.tv_sec + now.tv_nsec / 1e9;
}
static BOOL navBudget(NavRead *read) {
    if (++read->visited > 16000 || navNow() >= read->deadline) read->failed = 1;
    return !read->failed;
}
static CFTypeRef navCopy(AXUIElementRef element, CFStringRef attribute, NavRead *read) {
    if (!element || !navBudget(read)) return NULL;
    AXUIElementSetMessagingTimeout(element, 0.2);
    CFTypeRef value = NULL;
    AXError err = AXUIElementCopyAttributeValue(element, attribute, &value);
    if (err == kAXErrorSuccess) return value;
    if (value) CFRelease(value);
    if (err != kAXErrorNoValue && err != kAXErrorAttributeUnsupported) read->failed = 1;
    return NULL;
}
static BOOL navRole(AXUIElementRef element, CFStringRef role, NavRead *read) {
    CFTypeRef value = navCopy(element, kAXRoleAttribute, read);
    BOOL match = value && CFEqual(value, role);
    if (value) CFRelease(value);
    return match;
}
static double navNumber(AXUIElementRef element, CFStringRef attribute, double fallback, NavRead *read) {
    CFTypeRef value = navCopy(element, attribute, read);
    double result = fallback;
    if (value && CFGetTypeID(value) == CFNumberGetTypeID()) CFNumberGetValue((CFNumberRef)value, kCFNumberDoubleType, &result);
    if (value) CFRelease(value);
    return result;
}
static void navFindOutline(AXUIElementRef element, AXUIElementRef *found, int *matches, int depth, NavRead *read) {
    if (depth > 18) { read->failed = 1; return; }
    if (!navBudget(read)) return;
    if (navRole(element, kAXSheetRole, read)) { read->failed = 1; return; }
    if (navRole(element, kAXOutlineRole, read)) {
        (*matches)++;
        if (!*found) { *found = element; CFRetain(element); }
        return;
    }
    if (navRole(element, kAXRowRole, read)) return;
    CFTypeRef children = navCopy(element, kAXChildrenAttribute, read);
    if (children && CFGetTypeID(children) == CFArrayGetTypeID()) {
        for (CFIndex i = 0; i < CFArrayGetCount(children) && !read->failed; i++)
            navFindOutline((AXUIElementRef)CFArrayGetValueAtIndex(children, i), found, matches, depth + 1, read);
    }
    if (children) CFRelease(children);
}
static NSString *navLabel(NSString *text) {
    NSString *label = [[text precomposedStringWithCanonicalMapping] stringByTrimmingCharactersInSet:[NSCharacterSet whitespaceAndNewlineCharacterSet]];
    NSRegularExpression *duration = [NSRegularExpression regularExpressionWithPattern:@"\\s+\\d+(?:\\.\\d+)?\\s*分钟$" options:0 error:NULL];
    label = [duration stringByReplacingMatchesInString:label options:0 range:NSMakeRange(0, label.length) withTemplate:@""];
    if ([[label lowercaseString] hasSuffix:@".sz"]) label = [label substringToIndex:label.length - 3];
    NSCharacterSet *separators = [[NSCharacterSet whitespaceAndNewlineCharacterSet] mutableCopy];
    [(NSMutableCharacterSet *)separators addCharactersInString:@"·"];
    NSString *result = [[label componentsSeparatedByCharactersInSet:separators] componentsJoinedByString:@""];
    [separators release]; return result;
}
static BOOL navTextMatches(AXUIElementRef element, NSString *expected, int depth, NavRead *read) {
    if (depth > 7 || !navBudget(read)) return NO;
    for (NSString *attribute in @[@"AXValue", @"AXTitle"]) {
        CFTypeRef value = navCopy(element, (CFStringRef)attribute, read);
        BOOL match = value && CFGetTypeID(value) == CFStringGetTypeID() &&
            [navLabel((NSString *)value) isEqualToString:navLabel(expected)];
        if (value) CFRelease(value);
        if (match) return YES;
    }
    CFTypeRef children = navCopy(element, kAXChildrenAttribute, read);
    BOOL match = NO;
    if (children && CFGetTypeID(children) == CFArrayGetTypeID()) {
        for (CFIndex i = 0; i < CFArrayGetCount(children) && !match && !read->failed; i++) {
            AXUIElementRef child = (AXUIElementRef)CFArrayGetValueAtIndex(children, i);
            if (!navRole(child, kAXRowRole, read)) match = navTextMatches(child, expected, depth + 1, read);
        }
    }
    if (children) CFRelease(children);
    return match;
}
// Every scan checks the entire exposed row list to reject duplicate paths.
static AXUIElementRef navRow(AXUIElementRef outline, NSArray *path, int *result, long *rowIndex, long *rowCount, NavRead *read) {
    CFTypeRef rows = navCopy(outline, kAXRowsAttribute, read);
    if (!rows) rows = navCopy(outline, kAXChildrenAttribute, read);
    if (!rows || CFGetTypeID(rows) != CFArrayGetTypeID()) { if (rows) CFRelease(rows); *result = NavMissing; return NULL; }
    BOOL ancestors[32] = {0};
    AXUIElementRef found = NULL;
    int matches = 0;
    *rowCount = CFArrayGetCount(rows);
    for (CFIndex i = 0; i < *rowCount && !read->failed; i++) {
        AXUIElementRef row = (AXUIElementRef)CFArrayGetValueAtIndex(rows, i);
        if (!navRole(row, kAXRowRole, read)) continue;
        int level = (int)navNumber(row, kAXDisclosureLevelAttribute, 0, read);
        if (level < 0 || level >= 32) { read->failed = 1; break; }
        for (int child = level; child < 32; child++) ancestors[child] = NO;
        if (level >= (int)path.count || (level > 0 && !ancestors[level - 1])) continue;
        ancestors[level] = navTextMatches(row, path[level], 0, read);
        if (ancestors[level] && level == (int)path.count - 1) {
            matches++;
            if (!found) { found = row; CFRetain(row); *rowIndex = i; }
        }
    }
    CFRelease(rows);
    *result = read->failed ? NavIncomplete : matches > 1 ? NavAmbiguous : matches == 1 ? 1 : NavMissing;
    if (*result != 1 && found) { CFRelease(found); found = NULL; }
    return found;
}
static BOOL navRect(AXUIElementRef element, CGRect *rect, NavRead *read) {
    CFTypeRef position = navCopy(element, kAXPositionAttribute, read), size = navCopy(element, kAXSizeAttribute, read);
    BOOL ok = position && size && CFGetTypeID(position) == AXValueGetTypeID() && CFGetTypeID(size) == AXValueGetTypeID() &&
        AXValueGetValue(position, kAXValueCGPointType, &rect->origin) && AXValueGetValue(size, kAXValueCGSizeType, &rect->size) &&
        rect->size.width > 1 && rect->size.height > 1;
    if (position) CFRelease(position); if (size) CFRelease(size);
    return ok;
}
static BOOL navRowRect(AXUIElementRef row, CGRect *rect, int depth, NavRead *read) {
    if (navRect(row, rect, read)) return YES;
    if (depth >= 6 || read->failed) return NO;
    CFTypeRef children = navCopy(row, kAXChildrenAttribute, read);
    BOOL found = NO;
    if (children && CFGetTypeID(children) == CFArrayGetTypeID())
        for (CFIndex i = 0; i < CFArrayGetCount(children) && !found && !read->failed; i++) {
            AXUIElementRef child = (AXUIElementRef)CFArrayGetValueAtIndex(children, i);
            if (!navRole(child, kAXRowRole, read) && !navRole(child, kAXDisclosureTriangleRole, read))
                found = navRowRect(child, rect, depth + 1, read);
        }
    if (children) CFRelease(children);
    return found;
}
static AXUIElementRef navScrollArea(AXUIElementRef outline, NavRead *read) {
    AXUIElementRef current = outline; CFRetain(current);
    for (int depth = 0; depth < 10 && !read->failed; depth++) {
        if (navRole(current, kAXScrollAreaRole, read)) return current;
        CFTypeRef parent = navCopy(current, kAXParentAttribute, read);
        CFRelease(current);
        if (!parent || CFGetTypeID(parent) != AXUIElementGetTypeID()) { if (parent) CFRelease(parent); return NULL; }
        current = (AXUIElementRef)parent;
    }
    CFRelease(current); return NULL;
}
static AXUIElementRef navDisclosure(AXUIElementRef row, int depth, NavRead *read) {
    if (depth > 6 || !navBudget(read)) return NULL;
    if (navRole(row, kAXDisclosureTriangleRole, read)) { CFRetain(row); return row; }
    CFTypeRef children = navCopy(row, kAXChildrenAttribute, read);
    AXUIElementRef found = NULL;
    if (children && CFGetTypeID(children) == CFArrayGetTypeID())
        for (CFIndex i = 0; i < CFArrayGetCount(children) && !found && !read->failed; i++) {
            AXUIElementRef child = (AXUIElementRef)CFArrayGetValueAtIndex(children, i);
            if (!navRole(child, kAXRowRole, read)) found = navDisclosure(child, depth + 1, read);
        }
    if (children) CFRelease(children);
    return found;
}
// SzPlayer returns kAXErrorFailure for AXDisclosedRows and can leave
// AXDisclosing stale. The outline's exposed row hierarchy is authoritative.
static BOOL navExpanded(AXUIElementRef outline, AXUIElementRef row, NavRead *read) {
    CFTypeRef rows = navCopy(outline, kAXRowsAttribute, read);
    if (!rows) rows = navCopy(outline, kAXChildrenAttribute, read);
    if (!rows || CFGetTypeID(rows) != CFArrayGetTypeID()) {
        if (rows) CFRelease(rows);
        read->failed = 1; return NO;
    }
    BOOL expanded = NO, found = NO;
    double level = navNumber(row, kAXDisclosureLevelAttribute, -1, read);
    if (level < 0) read->failed = 1;
    for (CFIndex i = 0; i < CFArrayGetCount(rows) && !read->failed; i++) {
        AXUIElementRef candidate = (AXUIElementRef)CFArrayGetValueAtIndex(rows, i);
        if (CFEqual(candidate, row)) { found = YES; continue; }
        if (found && navRole(candidate, kAXRowRole, read)) {
            double nextLevel = navNumber(candidate, kAXDisclosureLevelAttribute, -1, read);
            if (nextLevel < 0) read->failed = 1;
            expanded = nextLevel > level; break;
        }
    }
    if (!found) read->failed = 1;
    CFRelease(rows); return expanded;
}
static BOOL navHitBelongsToRow(AXUIElementRef app, AXUIElementRef row, CGPoint point, NavRead *read) {
    AXUIElementRef hit = NULL;
    if (AXUIElementCopyElementAtPosition(app, point.x, point.y, &hit) != kAXErrorSuccess || !hit) return NO;
    for (int depth = 0; depth < 12 && !read->failed; depth++) {
        if (CFEqual(hit, row)) { CFRelease(hit); return YES; }
        CFTypeRef parent = navCopy(hit, kAXParentAttribute, read);
        CFRelease(hit);
        if (!parent || CFGetTypeID(parent) != AXUIElementGetTypeID()) { if (parent) CFRelease(parent); return NO; }
        hit = (AXUIElementRef)parent;
    }
    CFRelease(hit); return NO;
}

static int navLocate(pid_t pid, NSArray *path, BOOL expand, BOOL click, double deadline) {
    double lower = 0, upper = 1;
    BOOL pressed = NO;
    for (int attempt = 0; attempt < 12 && navNow() < deadline; attempt++) {
        NavRead read = {.deadline = fmin(deadline, navNow() + 3.0)};
        AXUIElementRef app = AXUIElementCreateApplication(pid), outline = NULL;
        if (!app) return NavMissing;
        int outlines = 0, result = NavMissing;
        navFindOutline(app, &outline, &outlines, 0, &read);
        if (read.failed || outlines != 1) { if (outline) CFRelease(outline); CFRelease(app); return read.failed ? NavIncomplete : outlines > 1 ? NavAmbiguous : NavMissing; }
        long index = 0, count = 0;
        AXUIElementRef row = navRow(outline, path, &result, &index, &count, &read);
        AXUIElementRef area = row ? navScrollArea(outline, &read) : NULL;
        if (!row) { CFRelease(outline); CFRelease(app); return result; }
        if (expand && navExpanded(outline, row, &read) && !read.failed) result = 1;
        else {
            CGRect rowRect = CGRectZero, viewport = CGRectZero;
            BOOL rectKnown = navRowRect(row, &rowRect, 0, &read) && navRect(area ?: outline, &viewport, &read);
            CGRect visible = rectKnown ? CGRectIntersection(rowRect, viewport) : CGRectNull;
            BOOL onscreen = !CGRectIsNull(visible) && visible.size.width > 2 && visible.size.height > 2;
            if (read.failed) result = NavIncomplete;
            else if (onscreen) {
                CGPoint point = CGPointMake(CGRectGetMidX(visible), CGRectGetMidY(visible));
                if (expand) {
                    if (pressed) result = 0; // Observe the first press; never toggle it again.
                    else {
                        AXUIElementRef triangle = navDisclosure(row, 0, &read);
                        if (!triangle || read.failed) result = NavIncomplete;
                        else { AXError err = AXUIElementPerformAction(triangle, kAXPressAction); result = err == kAXErrorSuccess ? 0 : NavUncertain; pressed = YES; }
                        if (triangle) CFRelease(triangle);
                    }
                } else if (!navHitBelongsToRow(app, row, point, &read)) result = NavBlocked;
                else if (click) result = postPlayerDoubleClick(pid, point.x, point.y) == 1 ? 1 : NavNotVisible;
                else result = 1;
            } else if (click) result = NavNotVisible;
            else {
                AXUIElementRef bar = area ? (AXUIElementRef)navCopy(area, kAXVerticalScrollBarAttribute, &read) : NULL;
                Boolean writable = false;
                if (!bar || AXUIElementIsAttributeSettable(bar, kAXValueAttribute, &writable) != kAXErrorSuccess || !writable) result = NavScrollUnsupported;
                else {
                    double current = navNumber(bar, kAXValueAttribute, 0, &read);
                    double minimum = navNumber(bar, kAXMinValueAttribute, 0, &read), maximum = navNumber(bar, kAXMaxValueAttribute, 1, &read);
                    if (!(maximum > minimum) || read.failed) result = NavIncomplete;
                    else {
                        double fraction = (current - minimum) / (maximum - minimum);
                        if (rectKnown) { if (CGRectGetMaxY(rowRect) <= CGRectGetMinY(viewport)) upper = fmin(upper, fraction); else lower = fmax(lower, fraction); }
                        double target = attempt == 0 && count > 1 ? (double)index / (count - 1) : (lower + upper) / 2;
                        target = minimum + fmax(0, fmin(1, target)) * (maximum - minimum);
                        CFNumberRef number = CFNumberCreate(NULL, kCFNumberDoubleType, &target);
                        AXError err = AXUIElementSetAttributeValue(bar, kAXValueAttribute, number);
                        CFRelease(number); result = err == kAXErrorSuccess ? 0 : NavUncertain;
                    }
                }
                if (bar) CFRelease(bar);
            }
        }
        if (area) CFRelease(area); CFRelease(row); CFRelease(outline); CFRelease(app);
        if (result != 0) return result;
        usleep(100000);
    }
    return NavNotVisible;
}
static NSArray *navPath(const char *json) {
    if (!json) return nil;
    NSData *data = [[NSString stringWithUTF8String:json] dataUsingEncoding:NSUTF8StringEncoding];
    id value = [NSJSONSerialization JSONObjectWithData:data options:0 error:NULL];
    if (![value isKindOfClass:[NSArray class]] || ![value count] || [value count] > 32) return nil;
    for (id part in value) if (![part isKindOfClass:[NSString class]] || ![part length]) return nil;
    return value;
}
int revealPlayerCourse(pid_t pid, const char *json) {
    @autoreleasepool {
        NSArray *path = navPath(json); if (!path) return NavMissing;
        double deadline = navNow() + 15.0;
        for (NSUInteger count = 1; count < path.count; count++) {
            int result = navLocate(pid, [path subarrayWithRange:NSMakeRange(0, count)], YES, NO, deadline);
            if (result != 1) return result;
        }
        return navLocate(pid, path, NO, NO, deadline);
    }
}
int clickPlayerCourse(pid_t pid, const char *json) {
    @autoreleasepool {
        NSArray *path = navPath(json); if (!path) return NavMissing;
        return navLocate(pid, path, NO, YES, navNow() + 3.0);
    }
}
