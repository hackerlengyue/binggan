//go:build darwin && cgo

package playerautomation

/*
#cgo LDFLAGS: -framework ApplicationServices -framework CoreFoundation -framework AppKit
#include <ApplicationServices/ApplicationServices.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <math.h>
#include <unistd.h>
#include <time.h>

int postPlayerDoubleClick(pid_t pid, double x, double y);
int revealPlayerCourse(pid_t pid, const char *json);
int clickPlayerCourse(pid_t pid, const char *json);
int postPlayerSingleClick(pid_t pid, double x, double y);
int postPlayerMouseMove(pid_t pid, double x, double y);
int postPlayerFullscreenMouseMove(pid_t pid, double x, double y);

static double monotonicSeconds(void);

static void requestAccessibilityPermission(void) {
    const void *keys[] = { kAXTrustedCheckOptionPrompt };
    const void *values[] = { kCFBooleanTrue };
    CFDictionaryRef options = CFDictionaryCreate(NULL, keys, values, 1,
        &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
    if (!options) return;
    AXIsProcessTrustedWithOptions(options);
    CFRelease(options);
}

static int copyString(AXUIElementRef element, CFStringRef attribute, char *out, size_t cap) {
    CFTypeRef value = NULL;
    if (AXUIElementCopyAttributeValue(element, attribute, &value) != kAXErrorSuccess || value == NULL) return 0;
    int ok = 0;
    if (CFGetTypeID(value) == CFStringGetTypeID()) {
        ok = CFStringGetCString((CFStringRef)value, out, cap, kCFStringEncodingUTF8);
    } else if (CFGetTypeID(value) == CFNumberGetTypeID()) {
        double number = 0;
        if (CFNumberGetValue((CFNumberRef)value, kCFNumberDoubleType, &number)) {
            // Preserve near-terminal values; rounding 99.9996 to 100 is not a finish event.
            snprintf(out, cap, "%.17f", number);
            ok = 1;
        }
    }
    CFRelease(value);
    return ok;
}

static void clean(char *value) {
    for (char *p = value; *p; p++) if (*p == '\n' || *p == '\r' || *p == '\t') *p = ' ';
}

static double elementWidth(AXUIElementRef element) {
    CFTypeRef sizeValue = NULL;
    if (AXUIElementCopyAttributeValue(element, kAXSizeAttribute, &sizeValue) != kAXErrorSuccess || !sizeValue) return 0;
    CGSize size = CGSizeZero;
    int ok = CFGetTypeID(sizeValue) == AXValueGetTypeID() &&
             AXValueGetValue((AXValueRef)sizeValue, kAXValueCGSizeType, &size);
    CFRelease(sizeValue);
    return ok ? size.width : 0;
}

static int copyBool(AXUIElementRef element, CFStringRef attribute, int *out) {
    CFTypeRef value = NULL;
    if (AXUIElementCopyAttributeValue(element, attribute, &value) != kAXErrorSuccess || !value) return 0;
    int ok = CFGetTypeID(value) == CFBooleanGetTypeID();
    if (ok) *out = CFBooleanGetValue((CFBooleanRef)value);
    CFRelease(value);
    return ok;
}

static int copyInt(AXUIElementRef element, CFStringRef attribute, int *out) {
    CFTypeRef value = NULL;
    if (AXUIElementCopyAttributeValue(element, attribute, &value) != kAXErrorSuccess || !value) return 0;
    int ok = CFGetTypeID(value) == CFNumberGetTypeID() &&
             CFNumberGetValue((CFNumberRef)value, kCFNumberIntType, out);
    CFRelease(value);
    return ok;
}

static AXUIElementRef disclosureTriangle(AXUIElementRef element, int depth, double deadline) {
    if (depth > 5 || monotonicSeconds() >= deadline) return NULL;
    AXUIElementSetMessagingTimeout(element, 0.2);
    char role[128] = {0};
    copyString(element, kAXRoleAttribute, role, sizeof(role));
    if (strcmp(role, "AXDisclosureTriangle") == 0) { CFRetain(element); return element; }
    CFTypeRef children = NULL;
    if (AXUIElementCopyAttributeValue(element, kAXChildrenAttribute, &children) != kAXErrorSuccess || !children) return NULL;
    AXUIElementRef found = NULL;
    if (CFGetTypeID(children) == CFArrayGetTypeID()) {
        for (CFIndex i = 0; i < CFArrayGetCount(children) && !found; i++)
            found = disclosureTriangle((AXUIElementRef)CFArrayGetValueAtIndex(children, i), depth + 1, deadline);
    }
    CFRelease(children);
    return found;
}

// This player's AXDisclosing flag and disclosure-button value stay true after
// collapse. The actual disclosed rows are the reliable expansion signal.
static int disclosedRowCount(AXUIElementRef row) {
    CFTypeRef value = NULL;
    if (AXUIElementCopyAttributeValue(row, CFSTR("AXDisclosedRows"), &value) != kAXErrorSuccess || !value) return -1;
    int count = CFGetTypeID(value) == CFArrayGetTypeID() ? (int)CFArrayGetCount(value) : -1;
    CFRelease(value);
    return count;
}

static int appendLine(char **buffer, size_t *length, size_t *capacity, const char *role, const char *title, const char *value, const char *identifier, double width, int depth, int level, int canExpand, int expanded, const char *placeholder) {
    size_t need = strlen(role) + strlen(title) + strlen(value) + strlen(identifier) + strlen(placeholder) + 96;
    if (*length + need + 1 > *capacity) {
        size_t next = *capacity;
        while (*length + need + 1 > next) next *= 2;
        char *grown = realloc(*buffer, next);
        if (!grown) return 0;
        *buffer = grown;
        *capacity = next;
    }
    *length += snprintf(*buffer + *length, *capacity - *length, "%s\t%s\t%s\t%s\t%.3f\t%d\t%d\t%d\t%d\t%s\n", role, title, value, identifier, width, depth, level, canExpand, expanded, placeholder);
    return 1;
}

static double monotonicSeconds(void) {
    struct timespec now;
    clock_gettime(CLOCK_MONOTONIC, &now);
    return now.tv_sec + now.tv_nsec / 1e9;
}

// Course rows and the menu bar cannot contain the verified playback controls.
// Reading them on every poll can consume the budget before the control bar.
static int isPlaybackExcludedRole(const char *role) {
    return strcmp(role, "AXOutline") == 0 || strcmp(role, "AXMenuBar") == 0;
}

static int walkSnapshot(AXUIElementRef element, int depth, int *visited, char **buffer, size_t *length, size_t *capacity, double deadline, int *incomplete, int playbackOnly) {
    if (*visited >= 4000 || monotonicSeconds() >= deadline) { *incomplete = 1; return 1; }
    if (depth > 18) return 1;
    (*visited)++;
    AXUIElementSetMessagingTimeout(element, 0.2);
    char role[128] = {0}, title[1024] = {0}, value[1024] = {0}, description[1024] = {0}, identifier[256] = {0}, placeholder[1024] = {0};
    copyString(element, kAXRoleAttribute, role, sizeof(role));
    if (playbackOnly && isPlaybackExcludedRole(role)) return 1;
    copyString(element, kAXTitleAttribute, title, sizeof(title));
    copyString(element, kAXValueAttribute, value, sizeof(value));
    copyString(element, kAXDescriptionAttribute, description, sizeof(description));
    copyString(element, kAXIdentifierAttribute, identifier, sizeof(identifier));
    copyString(element, CFSTR("AXPlaceholderValue"), placeholder, sizeof(placeholder));
    if (!title[0] && description[0]) strncpy(title, description, sizeof(title)-1);
    clean(role); clean(title); clean(value); clean(identifier); clean(placeholder);
    int level = 0, expanded = 0;
    copyBool(element, kAXDisclosingAttribute, &expanded);
    AXUIElementRef triangle = strcmp(role, "AXRow") == 0 ? disclosureTriangle(element, 0, deadline) : NULL;
    int canExpand = triangle != NULL;
    if (triangle) {
        int count = disclosedRowCount(element);
        if (count >= 0) expanded = count > 0;
        CFRelease(triangle);
    }
    copyInt(element, kAXDisclosureLevelAttribute, &level);
    int structural = strcmp(role, "AXRow") == 0 || strcmp(role, "AXOutline") == 0;
    if ((structural || title[0] || value[0] || identifier[0] || placeholder[0]) &&
        !appendLine(buffer, length, capacity, role, title, value, identifier, elementWidth(element), depth, level, canExpand, expanded, placeholder)) return 0;

    CFTypeRef childrenValue = NULL;
    if (AXUIElementCopyAttributeValue(element, kAXChildrenAttribute, &childrenValue) != kAXErrorSuccess || !childrenValue) return 1;
    if (CFGetTypeID(childrenValue) != CFArrayGetTypeID()) { CFRelease(childrenValue); return 1; }
    CFArrayRef children = (CFArrayRef)childrenValue;
    CFIndex count = CFArrayGetCount(children);
    for (CFIndex i = 0; i < count; i++) {
        AXUIElementRef child = (AXUIElementRef)CFArrayGetValueAtIndex(children, i);
        if (!walkSnapshot(child, depth + 1, visited, buffer, length, capacity, deadline, incomplete, playbackOnly)) { CFRelease(childrenValue); return 0; }
    }
    CFRelease(childrenValue);
    return 1;
}

static char* axSnapshot(pid_t pid, int *incomplete, int playbackOnly) {
    AXUIElementRef app = AXUIElementCreateApplication(pid);
    if (!app) return NULL;
    size_t capacity = 8192, length = 0;
    char *buffer = calloc(capacity, 1);
    int visited = 0;
    int ok = buffer && walkSnapshot(app, 0, &visited, &buffer, &length, &capacity, monotonicSeconds() + 3.0, incomplete, playbackOnly);
    CFRelease(app);
    if (!ok) { free(buffer); return NULL; }
    return buffer;
}

static int containsText(AXUIElementRef element, const char *needle, int exact) {
    char value[2048] = {0};
    const CFStringRef attrs[] = {kAXTitleAttribute, kAXValueAttribute, kAXDescriptionAttribute, kAXHelpAttribute};
    for (int i = 0; i < 4; i++) {
        value[0] = 0;
        if (!copyString(element, attrs[i], value, sizeof(value))) continue;
        char *start = value;
        while (*start == ' ' || *start == '\t' || *start == '\r' || *start == '\n') start++;
        char *end = start + strlen(start);
        while (end > start && (end[-1] == ' ' || end[-1] == '\t' || end[-1] == '\r' || end[-1] == '\n')) end--;
        *end = 0;
        clean(start);
        if (strcmp(start, needle) == 0 || (!exact && strstr(start, needle) != NULL)) return 1;
    }
    return 0;
}

static int supportsPress(AXUIElementRef element) {
    CFArrayRef actions = NULL;
    if (AXUIElementCopyActionNames(element, &actions) != kAXErrorSuccess || !actions) return 0;
    int found = CFArrayContainsValue(actions, CFRangeMake(0, CFArrayGetCount(actions)), kAXPressAction);
    CFRelease(actions);
    return found;
}

static AXUIElementRef findElement(AXUIElementRef element, const char *needle, int requirePress, int exact, int depth, int *visited, double deadline) {
    if (depth > 18 || *visited >= 4000 || monotonicSeconds() >= deadline) return NULL;
    (*visited)++;
    AXUIElementSetMessagingTimeout(element, 0.2);
    if (containsText(element, needle, exact) && (!requirePress || supportsPress(element))) { CFRetain(element); return element; }
    CFTypeRef childrenValue = NULL;
    if (AXUIElementCopyAttributeValue(element, kAXChildrenAttribute, &childrenValue) != kAXErrorSuccess || !childrenValue) return NULL;
    if (CFGetTypeID(childrenValue) != CFArrayGetTypeID()) { CFRelease(childrenValue); return NULL; }
    CFArrayRef children = (CFArrayRef)childrenValue;
    AXUIElementRef found = NULL;
    for (CFIndex i = 0; i < CFArrayGetCount(children) && !found; i++) {
        found = findElement((AXUIElementRef)CFArrayGetValueAtIndex(children, i), needle, requirePress, exact, depth + 1, visited, deadline);
    }
    CFRelease(childrenValue);
    return found;
}

static int hasRole(AXUIElementRef element, const char *expected) {
    char role[128] = {0};
    return copyString(element, kAXRoleAttribute, role, sizeof(role)) && strcmp(role, expected) == 0;
}

static AXUIElementRef findVolumeSlider(AXUIElementRef element, int depth, int *visited, double deadline) {
    if (depth > 18 || *visited >= 4000 || monotonicSeconds() >= deadline) return NULL;
    (*visited)++;
    AXUIElementSetMessagingTimeout(element, 0.2);
    char role[128] = {0};
    copyString(element, kAXRoleAttribute, role, sizeof(role));
    if (isPlaybackExcludedRole(role)) return NULL;
    double width = elementWidth(element);
    if (strcmp(role, "AXSlider") == 0 && width >= 40 && width < 140) { CFRetain(element); return element; }
    CFTypeRef childrenValue = NULL;
    if (AXUIElementCopyAttributeValue(element, kAXChildrenAttribute, &childrenValue) != kAXErrorSuccess || !childrenValue) return NULL;
    if (CFGetTypeID(childrenValue) != CFArrayGetTypeID()) { CFRelease(childrenValue); return NULL; }
    AXUIElementRef found = NULL;
    CFArrayRef children = (CFArrayRef)childrenValue;
    for (CFIndex i = 0; i < CFArrayGetCount(children) && !found; i++)
        found = findVolumeSlider((AXUIElementRef)CFArrayGetValueAtIndex(children, i), depth + 1, visited, deadline);
    CFRelease(childrenValue);
    return found;
}

static int setPlayerVolume(pid_t pid, double level) {
    AXUIElementRef app = AXUIElementCreateApplication(pid);
    if (!app) return 0;
    int visited = 0;
    AXUIElementRef slider = findVolumeSlider(app, 0, &visited, monotonicSeconds() + 2.0);
    CFRelease(app);
    if (!slider) return -1;
    CFNumberRef value = CFNumberCreate(NULL, kCFNumberDoubleType, &level);
    AXError err = value ? AXUIElementSetAttributeValue(slider, kAXValueAttribute, value) : kAXErrorFailure;
    if (value) CFRelease(value);
    if (err != kAXErrorSuccess) { CFRelease(slider); return 0; }
    // AX success acknowledges the write; it does not prove that this media
    // instance retained it. Read back before reporting a successful setting.
    AXUIElementSetMessagingTimeout(slider, 0.2);
    for (int attempt = 0; attempt < 5; attempt++) {
        CFTypeRef actual = NULL;
        double observed = NAN;
        if (AXUIElementCopyAttributeValue(slider, kAXValueAttribute, &actual) == kAXErrorSuccess && actual &&
            CFGetTypeID(actual) == CFNumberGetTypeID())
            CFNumberGetValue((CFNumberRef)actual, kCFNumberDoubleType, &observed);
        if (actual) CFRelease(actual);
        if (isfinite(observed) && fabs(observed - level) <= 0.5) { CFRelease(slider); return 1; }
        if (attempt < 4) usleep(50000);
    }
    CFRelease(slider);
    return -2;
}

static int pressPlayerButton(pid_t pid, const char *needle) {
    AXUIElementRef app = AXUIElementCreateApplication(pid);
    if (!app) return 0;
    int visited = 0;
    AXUIElementRef element = findElement(app, needle, 1, 1, 0, &visited, monotonicSeconds() + 2.0);
    CFRelease(app);
    if (!element) return -1;
    AXError err = AXUIElementPerformAction(element, kAXPressAction);
    CFRelease(element);
    return err == kAXErrorSuccess ? 1 : 0;
}

static int elementCenter(AXUIElementRef element, CGPoint *point) {
    CFTypeRef positionValue = NULL, sizeValue = NULL;
    if (AXUIElementCopyAttributeValue(element, kAXPositionAttribute, &positionValue) != kAXErrorSuccess || !positionValue) return 0;
    if (AXUIElementCopyAttributeValue(element, kAXSizeAttribute, &sizeValue) != kAXErrorSuccess || !sizeValue) { CFRelease(positionValue); return 0; }
    CGPoint position = CGPointZero; CGSize size = CGSizeZero;
    int ok = AXValueGetValue((AXValueRef)positionValue, kAXValueCGPointType, &position) &&
             AXValueGetValue((AXValueRef)sizeValue, kAXValueCGSizeType, &size) && size.width > 0 && size.height > 0;
    CFRelease(positionValue); CFRelease(sizeValue);
    if (!ok) return 0;
    point->x = position.x + size.width / 2.0;
    point->y = position.y + size.height / 2.0;
    return 1;
}

// The verified 26.06.54 control bar places its titleless playback button
// immediately above the right end of the wide playback slider. Prefer AXPress
// so the player keeps working while another application's settings is focused.
static AXUIElementRef findPlaybackButton(AXUIElementRef element, CGPoint expected, int depth, int *visited, double deadline) {
    if (depth > 18 || *visited >= 4000 || monotonicSeconds() >= deadline) return NULL;
    (*visited)++;
    AXUIElementSetMessagingTimeout(element, 0.2);
    char role[128] = {0};
    copyString(element, kAXRoleAttribute, role, sizeof(role));
    if (isPlaybackExcludedRole(role)) return NULL;
    CGPoint center = CGPointZero;
    if (strcmp(role, "AXButton") == 0 && supportsPress(element) &&
        elementCenter(element, &center) &&
        fabs(center.x - expected.x) < 28 && fabs(center.y - expected.y) < 28) {
        CFRetain(element);
        return element;
    }
    CFTypeRef childrenValue = NULL;
    if (AXUIElementCopyAttributeValue(element, kAXChildrenAttribute, &childrenValue) != kAXErrorSuccess || !childrenValue) return NULL;
    AXUIElementRef found = NULL;
    if (CFGetTypeID(childrenValue) == CFArrayGetTypeID()) {
        CFArrayRef children = (CFArrayRef)childrenValue;
        for (CFIndex i = 0; i < CFArrayGetCount(children) && !found; i++)
            found = findPlaybackButton((AXUIElementRef)CFArrayGetValueAtIndex(children, i), expected, depth + 1, visited, deadline);
    }
    CFRelease(childrenValue);
    return found;
}

static AXUIElementRef findPlaybackSlider(AXUIElementRef element, int depth, int *visited, double deadline) {
    if (depth > 18 || *visited >= 4000 || monotonicSeconds() >= deadline) return NULL;
    (*visited)++;
    AXUIElementSetMessagingTimeout(element, 0.2);
    char role[128] = {0};
    copyString(element, kAXRoleAttribute, role, sizeof(role));
    if (isPlaybackExcludedRole(role)) return NULL;
    char identifier[128] = {0};
    if (strcmp(role, "AXSlider") == 0 &&
        copyString(element, kAXIdentifierAttribute, identifier, sizeof(identifier)) &&
        strcmp(identifier, "_NS:26") == 0) { CFRetain(element); return element; }
    CFTypeRef childrenValue = NULL;
    if (AXUIElementCopyAttributeValue(element, kAXChildrenAttribute, &childrenValue) != kAXErrorSuccess || !childrenValue) return NULL;
    AXUIElementRef found = NULL;
    if (CFGetTypeID(childrenValue) == CFArrayGetTypeID()) {
        CFArrayRef children = (CFArrayRef)childrenValue;
        for (CFIndex i = 0; i < CFArrayGetCount(children) && !found; i++)
            found = findPlaybackSlider((AXUIElementRef)CFArrayGetValueAtIndex(children, i), depth + 1, visited, deadline);
    }
    CFRelease(childrenValue);
    return found;
}

static int revealPlayerControlBar(pid_t pid);

static int togglePlayerPlayback(pid_t pid) {
    AXUIElementRef app = AXUIElementCreateApplication(pid);
    if (!app) return 0;
    int visited = 0;
    double deadline = monotonicSeconds() + 2.0;
    AXUIElementRef slider = findPlaybackSlider(app, 0, &visited, deadline);
    if (!slider) {
        int missing = visited >= 4000 || monotonicSeconds() >= deadline ? -6 : -1;
        CFRelease(app);
        if (missing == -6) return -6;
        int revealed = revealPlayerControlBar(pid);
        if (revealed == -6) return -8;
        if (revealed == -3) return -3;
        if (revealed != 1) return missing;
        app = AXUIElementCreateApplication(pid);
        if (!app) return 0;
        visited = 0;
        deadline = monotonicSeconds() + 2.0;
        slider = findPlaybackSlider(app, 0, &visited, deadline);
        if (!slider) {
            CFRelease(app);
            return visited >= 4000 || monotonicSeconds() >= deadline ? -6 : -1;
        }
    }
    CGPoint center = CGPointZero;
    double width = elementWidth(slider);
    int located = elementCenter(slider, &center);
    CFRelease(slider);
    if (!located || !isfinite(width) || width <= 0) { CFRelease(app); return -7; }
    CGPoint target = CGPointMake(center.x - width / 2.0 + 597, center.y + 22.5);
    visited = 0;
    AXUIElementRef button = findPlaybackButton(app, target, 0, &visited, deadline);
    CFRelease(app);
    if (button) {
        AXError err = AXUIElementPerformAction(button, kAXPressAction);
        CFRelease(button);
        if (err == kAXErrorSuccess) return 1;
        // CannotComplete can mean the press was delivered but its reply timed
        // out. Only an explicitly unsupported action permits another input.
        if (err != kAXErrorActionUnsupported && err != kAXErrorNotImplemented) return -5;
    }
    if (visited >= 4000 || monotonicSeconds() >= deadline) return -6;
    return postPlayerSingleClick(pid, target.x, target.y);
}

static AXUIElementRef copyPlaybackWindow(AXUIElementRef app) {
    AXUIElementSetMessagingTimeout(app, 0.2);
    double deadline = monotonicSeconds() + 2.0;
    CFTypeRef windowsValue = NULL;
    if (AXUIElementCopyAttributeValue(app, kAXWindowsAttribute, &windowsValue) == kAXErrorSuccess &&
        windowsValue && CFGetTypeID(windowsValue) == CFArrayGetTypeID()) {
        CFArrayRef windows = (CFArrayRef)windowsValue;
        for (CFIndex i = 0; i < CFArrayGetCount(windows); i++) {
            AXUIElementRef window = (AXUIElementRef)CFArrayGetValueAtIndex(windows, i);
            AXUIElementSetMessagingTimeout(window, 0.2);
            int visited = 0;
            AXUIElementRef slider = findPlaybackSlider(window, 0, &visited, deadline);
            if (slider) {
                CFRelease(slider);
                CFRetain(window);
                CFRelease(windowsValue);
                return window;
            }
        }
    }
    if (windowsValue) CFRelease(windowsValue);
    CFTypeRef window = NULL;
    if (AXUIElementCopyAttributeValue(app, kAXMainWindowAttribute, &window) == kAXErrorSuccess &&
        window && CFGetTypeID(window) == AXUIElementGetTypeID()) {
        AXUIElementSetMessagingTimeout((AXUIElementRef)window, 0.2);
        return (AXUIElementRef)window;
    }
    if (window) CFRelease(window);
    return NULL;
}

// Window mode is read independently of whether its Space is currently visible.
static int playerWindowMode(pid_t pid) {
    AXUIElementRef app = AXUIElementCreateApplication(pid);
    if (!app) return 0;
    AXUIElementSetMessagingTimeout(app, 0.2);
    AXUIElementRef window = copyPlaybackWindow(app);
    CFRelease(app);
    if (!window) return 0;
    int minimized = 0, fullscreen = 0;
    copyBool(window, kAXMinimizedAttribute, &minimized);
    copyBool(window, CFSTR("AXFullScreen"), &fullscreen);
    CFRelease(window);
    return minimized ? 3 : (fullscreen ? 2 : 1);
}

// In 26.06.54, setMediaProgress returns immediately while the bar is hidden.
// VideoBackView/ListenMouseView mouseMoved -> showBarCallback -> showMediaBar
// reveals it and refreshes the clock synchronously on the player's UI thread.
static int refreshPlayerProgress(pid_t pid) {
    AXUIElementRef app = AXUIElementCreateApplication(pid);
    if (!app) return 0;
    AXUIElementRef window = copyPlaybackWindow(app);
    CFRelease(app);
    if (!window) return -1;
    char subrole[128] = {0};
    copyString(window, kAXSubroleAttribute, subrole, sizeof(subrole));
    if (strcmp(subrole, "AXStandardWindow") != 0) { CFRelease(window); return -1; }
    CGPoint center = CGPointZero;
    double width = elementWidth(window);
    int located = elementCenter(window, &center);
    int fullscreen = 0, minimized = 0;
    copyBool(window, CFSTR("AXFullScreen"), &fullscreen);
    copyBool(window, kAXMinimizedAttribute, &minimized);
    CFRelease(window);
    if (minimized) return -6;
    if (!located || width <= 0) return -1;
    // Fullscreen gets its own Space-aware path. Do not activate the app or
    // change fullscreen merely to refresh its progress controls.
    // Stay in the video area, left of the course sidebar, clear of controls.
    int result = fullscreen
        ? postPlayerFullscreenMouseMove(pid, center.x - width / 4.0, center.y)
        : postPlayerMouseMove(pid, center.x - width / 4.0, center.y);
    if (fullscreen && result == -3) return -5;
    if (result == 1) usleep(150000); // Let the target's event queue update AX.
    return result;
}

// Pause/resume need the control bar in the AX tree. Polling stays read-only;
// only an actual toggle may send mouseMoved to showPlayContronlBar.
static int revealPlayerControlBar(pid_t pid) {
    AXUIElementRef app = AXUIElementCreateApplication(pid);
    if (!app) return 0;
    AXUIElementRef window = copyPlaybackWindow(app);
    CFRelease(app);
    if (!window) return -1;
    CGPoint center = CGPointZero;
    double width = elementWidth(window);
    int located = elementCenter(window, &center);
    int fullscreen = 0, minimized = 0;
    copyBool(window, CFSTR("AXFullScreen"), &fullscreen);
    copyBool(window, kAXMinimizedAttribute, &minimized);
    CFRelease(window);
    if (minimized) return -6;
    if (!located || width <= 0) return -1;
    int result = fullscreen
        ? postPlayerFullscreenMouseMove(pid, center.x - width / 4.0, center.y)
        : postPlayerMouseMove(pid, center.x - width / 4.0, center.y);
    if (!fullscreen && result == -3)
        result = postPlayerFullscreenMouseMove(pid, center.x - width / 4.0, center.y);
    if (fullscreen && result == -3) return -3;
    if (result == 1) usleep(200000);
    return result;
}

// SzPlayer 26.06.54 exposes a settable AXFullScreen boolean on its JMWindow.
// Setting the desired state is idempotent and does not steal keyboard focus.
static int setPlayerFullscreen(pid_t pid, int enabled) {
    AXUIElementRef app = AXUIElementCreateApplication(pid);
    if (!app) return 0;
    AXUIElementRef window = copyPlaybackWindow(app);
    CFRelease(app);
    if (!window) return -1;
    int current = 0;
    if (!copyBool(window, CFSTR("AXFullScreen"), &current)) { CFRelease(window); return -2; }
    if (current == enabled) { CFRelease(window); return 1; }
    Boolean settable = false;
    if (AXUIElementIsAttributeSettable(window, CFSTR("AXFullScreen"), &settable) != kAXErrorSuccess || !settable) {
        CFRelease(window);
        return -2;
    }
    AXError result = AXUIElementSetAttributeValue(window, CFSTR("AXFullScreen"), enabled ? kCFBooleanTrue : kCFBooleanFalse);
    if (result != kAXErrorSuccess) { CFRelease(window); return 0; }
    for (int attempt = 0; attempt < 35; attempt++) {
        usleep(150000);
        if (copyBool(window, CFSTR("AXFullScreen"), &current) && current == enabled) {
            CFRelease(window);
            return 1;
        }
    }
    CFRelease(window);
    return -3;
}

static AXUIElementRef findPlayerRateMenuItem(AXUIElementRef element, const char *label, int depth, int *visited, double deadline) {
    if (depth > 18 || *visited >= 4000 || monotonicSeconds() >= deadline) return NULL;
    (*visited)++;
    AXUIElementSetMessagingTimeout(element, 0.2);
    if (hasRole(element, "AXMenuItem") && containsText(element, label, 1) && supportsPress(element)) {
        CFRetain(element);
        return element;
    }
    CFTypeRef childrenValue = NULL;
    if (AXUIElementCopyAttributeValue(element, kAXChildrenAttribute, &childrenValue) != kAXErrorSuccess || !childrenValue) return NULL;
    AXUIElementRef found = NULL;
    if (CFGetTypeID(childrenValue) == CFArrayGetTypeID()) {
        CFArrayRef children = (CFArrayRef)childrenValue;
        for (CFIndex i = 0; i < CFArrayGetCount(children) && !found; i++)
            found = findPlayerRateMenuItem((AXUIElementRef)CFArrayGetValueAtIndex(children, i), label, depth + 1, visited, deadline);
    }
    CFRelease(childrenValue);
    return found;
}

static AXUIElementRef findPlayerRateMenuItemInAttribute(AXUIElementRef app, CFStringRef attribute, const char *label, double deadline) {
    if (monotonicSeconds() >= deadline) return NULL;
    AXUIElementSetMessagingTimeout(app, 0.2);
    CFTypeRef value = NULL;
    if (AXUIElementCopyAttributeValue(app, attribute, &value) != kAXErrorSuccess || !value) return NULL;
    AXUIElementRef found = NULL;
    if (CFGetTypeID(value) == AXUIElementGetTypeID()) {
        int visited = 0;
        found = findPlayerRateMenuItem((AXUIElementRef)value, label, 0, &visited, deadline);
    }
    CFRelease(value);
    return found;
}

static int setPlayerPlaybackRate(pid_t pid, const char *rateLabel) {
    AXUIElementRef app = AXUIElementCreateApplication(pid);
    if (!app) return 0;
    int visited = 0;
    AXUIElementRef control = findElement(app, "倍速播放", 0, 1, 0, &visited, monotonicSeconds() + 2.0);
    CFRelease(app);
    if (!control) return -1;
    int opened = 0;
    if (supportsPress(control)) {
        AXError result = AXUIElementPerformAction(control, kAXPressAction);
        opened = result == kAXErrorSuccess;
        if (!opened && result != kAXErrorActionUnsupported && result != kAXErrorNotImplemented) {
            CFRelease(control);
            return -5;
        }
    }
    if (!opened) {
        CGPoint point = CGPointZero;
        double width = elementWidth(control);
        if (!elementCenter(control, &point) || width < 10 || width > 100) {
            CFRelease(control);
            return -3;
        }
        opened = postPlayerSingleClick(pid, point.x, point.y) == 1;
    }
    CFRelease(control);
    if (!opened) return -3;
    double deadline = monotonicSeconds() + 3.0;
    for (int attempt = 0; attempt < 20 && monotonicSeconds() < deadline; attempt++) {
        usleep(100000);
        app = AXUIElementCreateApplication(pid);
        if (!app) return 0;
        visited = 0;
        AXUIElementRef item = findPlayerRateMenuItem(app, rateLabel, 0, &visited, deadline);
        if (!item) item = findPlayerRateMenuItemInAttribute(app, kAXFocusedUIElementAttribute, rateLabel, deadline);
        if (!item) item = findPlayerRateMenuItemInAttribute(app, kAXFocusedWindowAttribute, rateLabel, deadline);
        CFRelease(app);
        if (!item) continue;
        AXError result = AXUIElementPerformAction(item, kAXPressAction);
        CFRelease(item);
        return result == kAXErrorSuccess ? 1 : -4;
    }
    return -2;
}

static AXUIElementRef findDeepestTextElement(AXUIElementRef element, const char *needle, int depth, int *visited, double deadline) {
    if (depth > 18 || *visited >= 4000 || monotonicSeconds() >= deadline) return NULL;
    (*visited)++;
    AXUIElementSetMessagingTimeout(element, 0.2);
    CFTypeRef childrenValue = NULL;
    if (AXUIElementCopyAttributeValue(element, kAXChildrenAttribute, &childrenValue) == kAXErrorSuccess && childrenValue) {
        if (CFGetTypeID(childrenValue) == CFArrayGetTypeID()) {
            CFArrayRef children = (CFArrayRef)childrenValue;
            for (CFIndex i = 0; i < CFArrayGetCount(children); i++) {
                AXUIElementRef found = findDeepestTextElement(
                    (AXUIElementRef)CFArrayGetValueAtIndex(children, i), needle, depth + 1, visited, deadline);
                if (found) { CFRelease(childrenValue); return found; }
            }
        }
        CFRelease(childrenValue);
    }
    if (containsText(element, needle, 1)) { CFRetain(element); return element; }
    return NULL;
}

static int pointWithinCourseRow(AXUIElementRef row, const char *needle, CGPoint *point, double deadline) {
    int visited = 0;
    AXUIElementRef current = findDeepestTextElement(row, needle, 0, &visited, deadline);
    if (!current) return 0;
    for (int depth = 0; depth < 12 && monotonicSeconds() < deadline; depth++) {
        AXUIElementSetMessagingTimeout(current, 0.2);
        if (elementCenter(current, point)) { CFRelease(current); return 1; }
        if (CFEqual(current, row)) break;
        CFTypeRef parentValue = NULL;
        if (AXUIElementCopyAttributeValue(current, kAXParentAttribute, &parentValue) != kAXErrorSuccess || !parentValue) break;
        if (CFGetTypeID(parentValue) != AXUIElementGetTypeID()) {
            CFRelease(parentValue);
            break;
        }
        CFRelease(current);
        current = (AXUIElementRef)parentValue;
    }
    CFRelease(current);
    return -1;
}

typedef struct {
    int matches;
    int selected;
    int incomplete;
    CGPoint point;
} CourseMatch;

static void findCoursePoint(AXUIElementRef element, const char *needle, int ordinal, CourseMatch *match, int depth, int *visited, double deadline) {
    if (depth > 18 || *visited >= 4000 || monotonicSeconds() >= deadline) { match->incomplete = 1; return; }
    (*visited)++;
    AXUIElementSetMessagingTimeout(element, 0.2);
    if (hasRole(element, "AXRow")) {
        CGPoint point = CGPointZero;
        int result = pointWithinCourseRow(element, needle, &point, deadline);
        if (monotonicSeconds() >= deadline) { match->incomplete = 1; return; }
        if (result != 0) {
            if (match->matches == ordinal) {
                match->selected = result;
                match->point = point;
            }
            match->matches++;
        }
        return;
    }
    CFTypeRef childrenValue = NULL;
    if (AXUIElementCopyAttributeValue(element, kAXChildrenAttribute, &childrenValue) != kAXErrorSuccess || !childrenValue) return;
    if (CFGetTypeID(childrenValue) != CFArrayGetTypeID()) { CFRelease(childrenValue); return; }
    CFArrayRef children = (CFArrayRef)childrenValue;
    for (CFIndex i = 0; i < CFArrayGetCount(children); i++)
        findCoursePoint((AXUIElementRef)CFArrayGetValueAtIndex(children, i), needle, ordinal, match, depth + 1, visited, deadline);
    CFRelease(childrenValue);
}

static int expandFolderRowAt(AXUIElementRef element, int ordinal, const char *expectedName, int depth, int *visited, int *rowIndex, double deadline) {
    if (depth > 18 || *visited >= 4000 || monotonicSeconds() >= deadline) return -1;
    (*visited)++;
    AXUIElementSetMessagingTimeout(element, 0.2);
    if (hasRole(element, "AXRow")) {
        if ((*rowIndex)++ == ordinal) {
            int count = 0;
            AXUIElementRef text = findElement(element, expectedName, 0, 1, 0, &count, deadline);
            if (!text) return -2;
            CFRelease(text);
            AXUIElementRef triangle = disclosureTriangle(element, 0, deadline);
            if (!triangle) return -3;
            int expanded = 0;
            copyBool(element, kAXDisclosingAttribute, &expanded);
            int countRows = disclosedRowCount(element);
            if (countRows >= 0) expanded = countRows > 0;
            AXError result = expanded ? kAXErrorSuccess : AXUIElementPerformAction(triangle, kAXPressAction);
            CFRelease(triangle);
            return result == kAXErrorSuccess ? 1 : 0;
        }
    }
    CFTypeRef children = NULL;
    if (AXUIElementCopyAttributeValue(element, kAXChildrenAttribute, &children) != kAXErrorSuccess || !children) return -1;
    int result = -1;
    if (CFGetTypeID(children) == CFArrayGetTypeID()) {
        for (CFIndex i = 0; i < CFArrayGetCount(children) && result == -1; i++)
            result = expandFolderRowAt((AXUIElementRef)CFArrayGetValueAtIndex(children, i), ordinal, expectedName, depth + 1, visited, rowIndex, deadline);
    }
    CFRelease(children);
    return result;
}

static int expandPlayerFolderAt(pid_t pid, int ordinal, const char *expectedName) {
    AXUIElementRef app = AXUIElementCreateApplication(pid);
    if (!app) return 0;
    int visited = 0, rowIndex = 0;
    int result = expandFolderRowAt(app, ordinal, expectedName, 0, &visited, &rowIndex, monotonicSeconds() + 2.0);
    CFRelease(app);
    return result;
}

static int doubleClickPlayerRow(pid_t pid, const char *needle, int ordinal, int expectedMatches) {
    AXUIElementRef app = AXUIElementCreateApplication(pid);
    if (!app) return 0;
    int visited = 0;
    CourseMatch match = {0};
    findCoursePoint(app, needle, ordinal, &match, 0, &visited, monotonicSeconds() + 2.0);
    CFRelease(app);
    if (match.incomplete) return -7;
    if (expectedMatches > 0 && match.matches != expectedMatches) return -6;
    if (expectedMatches == 0 && match.matches > 1) return -5;
    if (match.matches == 0) return -1;
    if (match.selected <= 0) return -2;
    return postPlayerDoubleClick(pid, match.point.x, match.point.y);
}
*/
import "C"

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"howett.net/plist"
)

type darwinDriver struct{ actions sync.Mutex }

const verifiedPlayerVersion = "26.06.54"

func newDriver() Driver { return &darwinDriver{} }

func playerPath() string {
	for _, path := range []string{"/Applications/SzPlayer.app", filepath.Join(os.Getenv("HOME"), "Applications", "SzPlayer.app")} {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return path
		}
	}
	return ""
}

func playerVersion(path string) string {
	raw, err := os.ReadFile(filepath.Join(path, "Contents", "Info.plist"))
	if err != nil {
		return ""
	}
	var values map[string]any
	if _, err = plist.Unmarshal(raw, &values); err != nil {
		return ""
	}
	value, _ := values["CFBundleShortVersionString"].(string)
	return value
}

func playerPID() (int, bool) {
	out, err := exec.Command("/usr/bin/pgrep", "-x", ExecutableName).Output()
	if err != nil {
		return 0, false
	}
	line := strings.TrimSpace(strings.Split(string(out), "\n")[0])
	pid, err := strconv.Atoi(line)
	return pid, err == nil && pid > 0
}

func requireVerifiedPlayer() error {
	path := playerPath()
	if path == "" {
		return ErrNotFound
	}
	if playerVersion(path) != verifiedPlayerVersion {
		return ErrUnverified
	}
	return nil
}

func (d *darwinDriver) Status() Status {
	result := platformStatus()
	path := playerPath()
	result.Installed = path != ""
	result.Version = playerVersion(path)
	result.VerifiedProfile = result.Version == verifiedPlayerVersion
	_, result.Running = playerPID()
	result.AccessibilityReady = C.AXIsProcessTrusted() != 0
	switch {
	case !result.Installed:
		result.Message = ErrNotFound.Error()
	case !result.AccessibilityReady:
		result.Message = ErrPermission.Error()
	case !result.VerifiedProfile:
		result.Message = "已安装，但当前版本尚未完成静态适配验证"
	case result.Running:
		result.Message = "SzPlayer 26.06.54 已静态验证，可以读取可见控件"
	default:
		result.Message = "SzPlayer 26.06.54 已静态验证，请先手动打开并登录播放器"
	}
	return result
}

func (d *darwinDriver) Snapshot() (Snapshot, error) {
	if C.AXIsProcessTrusted() == 0 {
		return snapshot(false, nil), ErrPermission
	}
	pid, running := playerPID()
	if !running {
		return snapshot(false, nil), nil
	}
	return readPlayerSnapshot(pid, false)
}

func readPlayerSnapshot(pid int, playbackOnly bool) (Snapshot, error) {
	if C.AXIsProcessTrusted() == 0 {
		return snapshot(true, nil), ErrPermission
	}
	currentPID, running := playerPID()
	if !running || currentPID != pid {
		return snapshot(false, nil), nil
	}
	var incomplete C.int
	var scoped C.int
	if playbackOnly {
		scoped = 1
	}
	raw := C.axSnapshot(C.pid_t(pid), &incomplete, scoped)
	if raw == nil {
		return snapshot(true, nil), fmt.Errorf("无法读取 SzPlayer 可见控件")
	}
	defer C.free(unsafe.Pointer(raw))
	current, err := parseSnapshot(C.GoString(raw))
	current.PID = pid
	if incomplete != 0 {
		current.ReadIssue = &ReadIssue{Code: "accessibility_busy", Message: "SzPlayer 控件读取超时或内容过多，正在等待播放器响应"}
		return current, err
	}
	switch C.playerWindowMode(C.pid_t(pid)) {
	case 1:
		current.WindowMode = "windowed"
	case 2:
		current.WindowMode = "fullscreen"
	case 3:
		current.WindowMode = "minimized"
	default:
		current.WindowMode = "missing"
	}
	return current, err
}

func (d *darwinDriver) PlaybackSnapshot() (Snapshot, error) {
	d.actions.Lock()
	defer d.actions.Unlock()
	if err := requireVerifiedPlayer(); err != nil {
		return snapshot(false, nil), err
	}
	if C.AXIsProcessTrusted() == 0 {
		return snapshot(false, nil), ErrPermission
	}
	pid, running := playerPID()
	if !running {
		return snapshot(false, nil), nil
	}
	return refreshedPlaybackSnapshot(func() (Snapshot, error) { return readPlayerSnapshot(pid, true) }, func() error {
		switch result := C.refreshPlayerProgress(C.pid_t(pid)); result {
		case 1:
			return nil
		case -1:
			return retryableRead("controls_unavailable", "SzPlayer 播放控件暂时不可用")
		case -3:
			return retryableRead("window_not_visible", "SzPlayer 普通播放窗口不在当前桌面")
		case -5:
			return retryableRead("fullscreen_transition", "SzPlayer 全屏窗口正在切换或暂时不可用")
		case -6:
			return retryableRead("window_minimized", "SzPlayer 播放窗口已最小化，请恢复窗口")
		case 0:
			return retryableRead("refresh_unavailable", "SzPlayer 播放控件暂时无法刷新")
		default:
			return fmt.Errorf("无法刷新 SzPlayer 播放控件（%d）", int(result))
		}
	})
}

func parseSnapshot(raw string) (Snapshot, error) {
	elements := make([]Element, 0, 64)
	scanner := bufio.NewScanner(strings.NewReader(raw))
	scanner.Buffer(make([]byte, 4096), 2<<20)
	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), "\t", 10)
		if len(parts) != 10 {
			continue
		}
		width, _ := strconv.ParseFloat(parts[4], 64)
		depth, _ := strconv.Atoi(parts[5])
		level, _ := strconv.Atoi(parts[6])
		elements = append(elements, Element{
			Role: parts[0], Title: parts[1], Value: parts[2], Identifier: parts[3], Width: width,
			Placeholder: parts[9],
			Depth:       depth, Level: level, CanExpand: parts[7] == "1", Expanded: parts[8] == "1",
		})
	}
	if err := scanner.Err(); err != nil {
		return snapshot(true, nil), err
	}
	return snapshot(true, elements), nil
}

func courseNavigationError(result int) error {
	switch result {
	case 1:
		return nil
	case -20:
		return fmt.Errorf("尚未找到完整课程路径，请确认 SzPlayer 已加载对应目录")
	case -21:
		return fmt.Errorf("SzPlayer 存在多个完全相同的目录路径，无法唯一定位课程")
	case -22:
		return fmt.Errorf("课程控件读取不完整或界面被弹窗阻挡，已停止定位")
	case -23:
		return fmt.Errorf("课程不在可见区域，播放器未提供可操作的列表滚动条")
	case -24:
		return fmt.Errorf("目标课程未能进入可点击区域，已停止双击")
	case -25:
		return fmt.Errorf("目录展开或滚动操作结果不确定，已停止重复操作")
	case -26:
		return fmt.Errorf("目标课程被其他控件遮挡，已停止双击")
	default:
		return fmt.Errorf("无法定位 SzPlayer 课程")
	}
}

func (d *darwinDriver) RevealCourse(request CourseTargetRequest) (Snapshot, error) {
	d.actions.Lock()
	defer d.actions.Unlock()
	request, err := validateCourseTarget(request)
	if err != nil {
		return Snapshot{}, err
	}
	if C.AXIsProcessTrusted() == 0 {
		return Snapshot{}, ErrPermission
	}
	if err := requireVerifiedPlayer(); err != nil {
		return Snapshot{}, err
	}
	pid, running := playerPID()
	if !running {
		return Snapshot{}, fmt.Errorf("SzPlayer 尚未打开")
	}
	if request.ExpectedPID != 0 && request.ExpectedPID != pid {
		return Snapshot{}, fmt.Errorf("SzPlayer 已重启，课程定位已取消")
	}
	encoded, _ := json.Marshal(request.Path)
	path := C.CString(string(encoded))
	defer C.free(unsafe.Pointer(path))
	if err := courseNavigationError(int(C.revealPlayerCourse(C.pid_t(pid), path))); err != nil {
		return Snapshot{}, err
	}
	current, err := d.Snapshot()
	if err == nil && current.PID != pid {
		return current, fmt.Errorf("SzPlayer 已重启，课程定位已失效")
	}
	return current, err
}

func (d *darwinDriver) CourseSnapshot() (Snapshot, error) {
	d.actions.Lock()
	defer d.actions.Unlock()
	roots, err := loadedCourseRoots()
	if err == nil {
		_, running := playerPID()
		return courseSnapshotFromRoots(roots, running)
	}
	return d.visibleCourseSnapshot()
}

func (d *darwinDriver) visibleCourseSnapshot() (Snapshot, error) {
	// Discover courses through the same visible folder controls used by a person.
	attempted := map[folderKey]bool{}
	deadline := time.Now().Add(90 * time.Second)
	for attempt := 0; attempt < 100 && time.Now().Before(deadline); attempt++ {
		current, err := d.Snapshot()
		if err != nil {
			return current, err
		}
		folder, found, collapsed := nextCollapsedFolder(current, attempted)
		if !found && !collapsed {
			return current, nil
		}
		if !found {
			return current, fmt.Errorf("SzPlayer 课程目录未完全展开，请手动展开后重试")
		}
		attempted[folder] = true
		if err := d.expandFolderAt(folder.ordinal, folder.name); err != nil {
			return current, err
		}
		time.Sleep(250 * time.Millisecond)
	}
	return Snapshot{}, fmt.Errorf("SzPlayer 课程目录展开超时，请手动展开后重试")
}

func (d *darwinDriver) expandFolderAt(ordinal int, name string) error {
	if C.AXIsProcessTrusted() == 0 {
		return ErrPermission
	}
	if err := requireVerifiedPlayer(); err != nil {
		return err
	}
	pid, running := playerPID()
	if !running {
		return fmt.Errorf("SzPlayer 尚未打开")
	}
	needle := C.CString(name)
	defer C.free(unsafe.Pointer(needle))
	switch int(C.expandPlayerFolderAt(C.pid_t(pid), C.int(ordinal), needle)) {
	case 1:
		return nil
	case -1, -2:
		return fmt.Errorf("SzPlayer 课程目录已变化，请重新获取课程：%s", name)
	case -3:
		return fmt.Errorf("SzPlayer 文件夹没有可操作的展开控件：%s", name)
	default:
		return fmt.Errorf("无法展开 SzPlayer 文件夹：%s", name)
	}
}

func (d *darwinDriver) ExpandFolder(name string) error {
	d.actions.Lock()
	defer d.actions.Unlock()
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("文件夹名称不能为空")
	}
	if err := requireVerifiedPlayer(); err != nil {
		return err
	}
	attempted := map[folderKey]bool{}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		current, err := d.Snapshot()
		if err != nil {
			return err
		}
		if !current.Running {
			return fmt.Errorf("SzPlayer 尚未打开")
		}
		seen := false
		var candidate *folderKey
		for _, folder := range visibleFolders(current) {
			if folder.key.name != name {
				continue
			}
			seen = true
			if !folder.expanded && !attempted[folder.key] {
				key := folder.key
				candidate = &key
				break
			}
		}
		if candidate == nil {
			if seen {
				return nil
			}
			return fmt.Errorf("未找到可见文件夹：%s", name)
		}
		attempted[*candidate] = true
		if err := d.expandFolderAt(candidate.ordinal, candidate.name); err != nil {
			return err
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("展开文件夹超时：%s", name)
}

func loadedCourseRoots() ([]string, error) {
	prefsPath := filepath.Join(os.Getenv("HOME"), "Library", "Preferences", BundleID+".plist")
	data, err := os.ReadFile(prefsPath)
	if err != nil {
		return nil, fmt.Errorf("无法读取 SzPlayer 已加载的课程目录：%w", err)
	}
	var preferences struct {
		Files []string `plist:"files"`
	}
	if _, err := plist.Unmarshal(data, &preferences); err != nil {
		return nil, fmt.Errorf("SzPlayer 课程目录记录格式无效：%w", err)
	}
	return preferences.Files, nil
}

func (d *darwinDriver) OpenCourse(path string) error {
	d.actions.Lock()
	defer d.actions.Unlock()
	path = strings.TrimSpace(path)
	if !strings.EqualFold(filepath.Ext(path), ".sz") {
		return fmt.Errorf("只允许打开 SzPlayer 已加载目录中的 .sz 课程")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("课程文件不存在：%w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("课程文件无效")
	}
	roots, err := loadedCourseRoots()
	if err != nil {
		return err
	}
	allowed := false
	for _, root := range roots {
		rootResolved, rootErr := filepath.EvalSymlinks(root)
		if rootErr != nil {
			continue
		}
		rootInfo, rootErr := os.Stat(rootResolved)
		if rootErr != nil {
			continue
		}
		if !rootInfo.IsDir() {
			allowed = resolved == rootResolved
		} else if relative, relativeErr := filepath.Rel(rootResolved, resolved); relativeErr == nil {
			allowed = relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
		}
		if allowed {
			break
		}
	}
	if !allowed {
		return fmt.Errorf("课程文件不在 SzPlayer 已加载的目录中")
	}
	if C.AXIsProcessTrusted() == 0 {
		return ErrPermission
	}
	if err := requireVerifiedPlayer(); err != nil {
		return err
	}
	pid, running := playerPID()
	if !running {
		return fmt.Errorf("SzPlayer 尚未打开")
	}
	courseName := strings.TrimSpace(strings.TrimSuffix(filepath.Base(resolved), filepath.Ext(resolved)))
	needle := C.CString(courseName)
	defer C.free(unsafe.Pointer(needle))
	switch result := int(C.doubleClickPlayerRow(C.pid_t(pid), needle, 0, 0)); result {
	case 1:
		return nil
	case -1:
		return fmt.Errorf("未找到可见课程：%s", courseName)
	case -2:
		return fmt.Errorf("无法定位课程行：%s", courseName)
	case -3:
		return fmt.Errorf("课程行不在可见窗口内：%s", courseName)
	case -4:
		return fmt.Errorf("当前 macOS 不支持窗口内点击定位")
	case -5:
		return fmt.Errorf("存在多个同名可见课程：%s，请收起其他同名课程目录", courseName)
	case -7:
		return fmt.Errorf("SzPlayer 可见控件过多，无法安全定位课程：%s", courseName)
	default:
		return fmt.Errorf("无法在后台打开课程：%s", courseName)
	}
}

func (d *darwinDriver) SetVolume(percent int) error {
	d.actions.Lock()
	defer d.actions.Unlock()
	if percent < 0 || percent > 100 {
		return fmt.Errorf("播放音量须为 0–100 的整数")
	}
	if C.AXIsProcessTrusted() == 0 {
		return ErrPermission
	}
	if err := requireVerifiedPlayer(); err != nil {
		return err
	}
	pid, running := playerPID()
	if !running {
		return fmt.Errorf("SzPlayer 尚未打开")
	}
	switch result := int(C.setPlayerVolume(C.pid_t(pid), C.double(percent))); result {
	case 1:
		return nil
	case -1:
		return fmt.Errorf("未找到 SzPlayer 音量滑块")
	case -2:
		return fmt.Errorf("SzPlayer 音量写入后校验不一致，设置尚未生效")
	default:
		return fmt.Errorf("无法调整 SzPlayer 播放音量")
	}
}

func (d *darwinDriver) SetFullscreen(enabled bool) error {
	d.actions.Lock()
	defer d.actions.Unlock()
	if C.AXIsProcessTrusted() == 0 {
		return ErrPermission
	}
	path := playerPath()
	if path == "" {
		return ErrNotFound
	}
	if playerVersion(path) != verifiedPlayerVersion {
		return ErrUnverified
	}
	pid, running := playerPID()
	if !running {
		return fmt.Errorf("SzPlayer 尚未打开")
	}
	want := C.int(0)
	if enabled {
		want = 1
	}
	switch result := int(C.setPlayerFullscreen(C.pid_t(pid), want)); result {
	case 1:
		return nil
	case -1:
		return fmt.Errorf("未找到 SzPlayer 播放窗口，无法切换全屏")
	case -2:
		return fmt.Errorf("SzPlayer 播放窗口没有可设置的全屏控件")
	case -3:
		return fmt.Errorf("SzPlayer 全屏切换未在 5 秒内完成")
	default:
		return fmt.Errorf("无法切换 SzPlayer 全屏状态")
	}
}

func (d *darwinDriver) SetPlaybackRate(rate float64) error {
	d.actions.Lock()
	defer d.actions.Unlock()
	if err := validatePlaybackRate(rate); err != nil {
		return err
	}
	if C.AXIsProcessTrusted() == 0 {
		return ErrPermission
	}
	path := playerPath()
	if path == "" {
		return ErrNotFound
	}
	if playerVersion(path) != verifiedPlayerVersion {
		return ErrUnverified
	}
	pid, running := playerPID()
	if !running {
		return fmt.Errorf("SzPlayer 尚未打开")
	}
	label := fmt.Sprintf("%.1fX", rate)
	needle := C.CString(label)
	defer C.free(unsafe.Pointer(needle))
	switch result := int(C.setPlayerPlaybackRate(C.pid_t(pid), needle)); result {
	case 1:
		return nil
	case -1:
		return fmt.Errorf("未找到 SzPlayer 倍速控件，请保持播放控制栏可见")
	case -2:
		return fmt.Errorf("SzPlayer 倍速菜单中没有找到 %s", label)
	case -3:
		return fmt.Errorf("无法打开 SzPlayer 倍速菜单")
	case -4:
		return fmt.Errorf("SzPlayer 倍速选择结果不确定，已停止后续操作：%s", label)
	case -5:
		return fmt.Errorf("SzPlayer 倍速菜单操作结果不确定，已停止重复点击")
	default:
		return fmt.Errorf("无法设置 SzPlayer 播放倍速")
	}
}

func (d *darwinDriver) TogglePlayback() error {
	d.actions.Lock()
	defer d.actions.Unlock()
	if C.AXIsProcessTrusted() == 0 {
		return ErrPermission
	}
	path := playerPath()
	if path == "" {
		return ErrNotFound
	}
	if playerVersion(path) != verifiedPlayerVersion {
		return ErrUnverified
	}
	pid, running := playerPID()
	if !running {
		return fmt.Errorf("SzPlayer 尚未打开")
	}
	switch result := int(C.togglePlayerPlayback(C.pid_t(pid))); result {
	case 1:
		return nil
	case -1:
		return fmt.Errorf("SzPlayer 未暴露播放控制栏，请恢复播放器窗口并显示控制栏后再操作")
	case -6:
		return fmt.Errorf("SzPlayer 播放控件读取未完成，未发送暂停或继续操作，请等待播放器响应")
	case -7:
		return fmt.Errorf("无法确认 SzPlayer 播放控件位置，未发送暂停或继续操作")
	case -5:
		return fmt.Errorf("SzPlayer 暂停或继续的结果不确定，已停止重复点击，请确认播放器状态")
	case -3:
		return fmt.Errorf("SzPlayer 播放窗口不可见，请保持播放器窗口在当前桌面")
	case -8:
		return fmt.Errorf("SzPlayer 播放窗口已最小化，请恢复窗口后再操作")
	default:
		return fmt.Errorf("无法操作 SzPlayer 播放控件")
	}
}

func (d *darwinDriver) Click(request ClickRequest) error {
	d.actions.Lock()
	defer d.actions.Unlock()
	request, err := validateClick(request)
	if err != nil {
		return err
	}
	if C.AXIsProcessTrusted() == 0 {
		return ErrPermission
	}
	path := playerPath()
	if path == "" {
		return ErrNotFound
	}
	if playerVersion(path) != verifiedPlayerVersion {
		return ErrUnverified
	}
	pid, running := playerPID()
	if !running {
		return fmt.Errorf("SzPlayer 尚未打开")
	}
	if request.ExpectedPID != 0 && request.ExpectedPID != pid {
		return fmt.Errorf("SzPlayer 已重启，课程点击已取消")
	}
	if len(request.Path) > 0 {
		encoded, _ := json.Marshal(request.Path)
		path := C.CString(string(encoded))
		defer C.free(unsafe.Pointer(path))
		return courseNavigationError(int(C.clickPlayerCourse(C.pid_t(pid), path)))
	}
	needle := C.CString(request.Text)
	defer C.free(unsafe.Pointer(needle))
	if request.ClickCount == 2 {
		switch result := int(C.doubleClickPlayerRow(C.pid_t(pid), needle, C.int(request.MatchOrdinal), C.int(request.MatchCount))); result {
		case 1:
			return nil
		case -1:
			return fmt.Errorf("未找到可见课程：%s", request.Text)
		case -2:
			return fmt.Errorf("无法定位课程行：%s", request.Text)
		case -3:
			return fmt.Errorf("课程行不在可见窗口内：%s", request.Text)
		case -4:
			return fmt.Errorf("当前 macOS 不支持窗口内点击定位")
		case -5:
			return fmt.Errorf("存在多个同名可见课程：%s，请收起其他同名课程目录", request.Text)
		case -6:
			return fmt.Errorf("SzPlayer 课程列表已变化，请重新获取课程后重试：%s", request.Text)
		case -7:
			return fmt.Errorf("SzPlayer 可见控件过多，无法安全定位课程：%s", request.Text)
		default:
			return fmt.Errorf("无法在后台打开课程：%s", request.Text)
		}
	}
	switch result := int(C.pressPlayerButton(C.pid_t(pid), needle)); result {
	case 1:
		return nil
	case -1:
		return fmt.Errorf("未找到可见控件：%s", request.Text)
	default:
		return fmt.Errorf("无法在后台操作控件：%s", request.Text)
	}
}

func (d *darwinDriver) RequestAccessibilityPermission() error {
	C.requestAccessibilityPermission()
	return nil
}
