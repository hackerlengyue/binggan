package playerautomation

import "testing"

func TestPlayerProfilesRequireAnExplicitVersion(t *testing.T) {
	for _, version := range []string{"26.06.54", "26.10.56"} {
		if _, verified := profileForVersion(version); !verified {
			t.Fatalf("supported version rejected: %s", version)
		}
	}
	for _, version := range []string{"", "26.06.55", "26.10.57", "MAC_sz_26.10.56", "26.10.56-beta"} {
		if _, verified := profileForVersion(version); verified {
			t.Fatalf("unverified version accepted: %s", version)
		}
	}
}
