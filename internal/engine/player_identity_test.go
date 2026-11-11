package engine

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestWindowsFamilyIdentityUsesExecutableHashInAutoAndManualModes(t *testing.T) {
	checksum := strings.Repeat("a", 32)
	local := Player{SoftwareName: checksum, AppMD5: checksum}
	for _, manual := range []bool{false, true} {
		var fallback *Player
		if manual {
			fallback = &local
		}
		calls := 0
		got, err := resolveHash(map[string]any{"softwareName": "ShenZaoLocalPlayer42"}, "", fallback, func() (Player, error) {
			calls++
			return local, nil
		})
		if err != nil || got != checksum || manual && calls != 0 || !manual && calls != 1 {
			t.Errorf("manual=%v: hash=%q error=%v detection calls=%d", manual, got, err, calls)
		}
	}
}

func TestPlayerIdentityStillRejectsDifferentVersionsAndExecutableHashes(t *testing.T) {
	for _, tc := range []struct {
		capture string
		local   Player
	}{
		{strings.Repeat("a", 32), Player{strings.Repeat("b", 32), strings.Repeat("b", 32)}},
		{"MAC_sz_unmatched", Player{"MAC_sz_other", strings.Repeat("a", 32)}},
		{"ShenZaoLocalPlayer42", Player{"ShenZaoLocalPlayer43", strings.Repeat("a", 32)}},
		{"ShenZaoLocalPlayer42", Player{"MAC_sz_other", strings.Repeat("a", 32)}},
	} {
		if _, err := resolveHash(map[string]any{"softwareName": tc.capture}, "", &tc.local, DetectPlayer); err == nil {
			t.Errorf("different identity accepted: capture=%s local=%+v", tc.capture, tc.local)
		}
	}
}

func TestCapturedPlainSoftMd5DoesNotRequireCurrentPlayer(t *testing.T) {
	want := strings.Repeat("b", 32)
	for _, field := range []string{"softMd5", "softmd5", "soft_md5"} {
		got, err := resolveHash(map[string]any{"softwareName": "ShenZaoLocalPlayer42", field: strings.ToUpper(want)}, "", nil, func() (Player, error) {
			return Player{}, errors.New("must use the capture instead of the current player")
		})
		if err != nil || got != want {
			t.Errorf("%s: hash=%q error=%v", field, got, err)
		}
	}
}

func TestWindowsIdentityCompatibilityRetainsAESVerification(t *testing.T) {
	raw, err := os.ReadFile("testdata/key-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]string
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	data := map[string]any{"softwareName": "ShenZaoLocalPlayer42", "mimaEnc": v["encrypted"], "md51": v["md51"], "md52": v["md52"], "md53": v["md53"]}
	local := Player{SoftwareName: v["appMd5"], AppMD5: v["appMd5"]}
	hash, err := resolveHash(data, "", &local, DetectPlayer)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := Mima(data, hash); err != nil || got != v["mima"] {
		t.Fatalf("valid captured AES envelope rejected: %q, %v", got, err)
	}
	if _, err := Mima(data, strings.Repeat("e", 32)); err == nil {
		t.Fatal("wrong executable hash passed AES validation")
	}
}
