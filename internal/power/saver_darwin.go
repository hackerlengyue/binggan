//go:build darwin && cgo

package power

/*
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation -framework ApplicationServices
#include <IOKit/pwr_mgt/IOPMLib.h>
#include <ApplicationServices/ApplicationServices.h>
// Do not wake a display that the user has explicitly locked or put to sleep.
static int pulseUserActivity(IOPMAssertionID *assertion) {
 if (CGDisplayIsAsleep(CGMainDisplayID())) return 0;
 CFDictionaryRef session = CGSessionCopyCurrentDictionary();
 if (!session) return 0;
 CFBooleanRef locked = CFDictionaryGetValue(session, CFSTR("CGSSessionScreenIsLocked"));
 int isLocked = locked && CFBooleanGetValue(locked);
 CFRelease(session);
 if (isLocked) return 0;
 return IOPMAssertionDeclareUserActivity(CFSTR("Binggan: keep screen awake"), kIOPMUserActiveLocal, assertion);
}
*/
import "C"
import "fmt"

func startScreenSaverGuard() (func() error, func(), error) {
	var id C.IOPMAssertionID
	pulse := func() error {
		if code := C.pulseUserActivity(&id); code != 0 {
			return fmt.Errorf("无法阻止屏保：IOKit %d", code)
		}
		return nil
	}
	return pulse, func() {
		if id != 0 {
			C.IOPMAssertionRelease(id)
		}
	}, nil
}
