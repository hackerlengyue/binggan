package engine

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func compatibilityFixture(t *testing.T, hash, version string) Key {
	t.Helper()
	raw, err := os.ReadFile("testdata/key.json")
	if err != nil {
		t.Fatal(err)
	}
	key, err := ReadKey(raw)
	if err != nil {
		t.Fatal(err)
	}
	master := []byte(str(key.Data["mima"]))
	delete(key.Data, "mima")
	key.Data["softwareName"] = version
	key.Data["md51"], key.Data["md52"], key.Data["md53"] = strings.Repeat("a", 32), strings.Repeat("b", 32), strings.Repeat("c", 32)
	sum := md5.Sum([]byte(hash + str(key.Data["md51"]) + str(key.Data["md52"]) + str(key.Data["md53"])))
	block, err := aes.NewCipher([]byte(hex.EncodeToString(sum[:])))
	if err != nil {
		t.Fatal(err)
	}
	padding := aes.BlockSize - len(master)%aes.BlockSize
	plain := append(master, bytes.Repeat([]byte{byte(padding)}, padding)...)
	ct := make([]byte, len(plain))
	for i := 0; i < len(ct); i += aes.BlockSize {
		block.Encrypt(ct[i:i+aes.BlockSize], plain[i:i+aes.BlockSize])
	}
	key.Data["密码"] = base64.StdEncoding.EncodeToString(ct)
	return key
}

func TestCompatibilityMatchesLegacyWithoutRewritingCapture(t *testing.T) {
	for _, version := range []string{"ShenZaoLocalPlayer41", "ShenZaoLocalPlayer42"} {
		key := compatibilityFixture(t, legacyWindowsAppHash, version)
		key.Data["appMd5"] = strings.Repeat("e", 32) // A newer executable's stale setting.
		before, _ := json.Marshal(key)
		got, err := resolveCompatibleKey(key, "", nil, func() (Player, error) {
			return Player{SoftwareName: strings.Repeat("e", 32), AppMD5: strings.Repeat("e", 32)}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if got.AppHash != legacyWindowsAppHash || got.Source != "historical" || !got.Adjusted {
			t.Fatalf("wrong compatibility result: hash=%s source=%s adjusted=%v", got.AppHash, got.Source, got.Adjusted)
		}
		original, _ := json.Marshal(key)
		if !bytes.Equal(before, original) {
			t.Fatal("matching mutated original capture")
		}
		expected := key.Data["appMd5"]
		delete(key.Data, "appMd5")
		data := got.Key.Data
		delete(data, "appMd5")
		if !reflect.DeepEqual(key.Data, data) || !reflect.DeepEqual(key.Passwords, got.Key.Passwords) {
			t.Fatal("compatibility rewrote password material or media parameters")
		}
		key.Data["appMd5"] = expected
	}
}

func TestCompatibilityUsesCapturedHashWithoutInstalledPlayer(t *testing.T) {
	hash := strings.Repeat("d", 32)
	for _, field := range []string{"appMd5", "app_md5", "softMd5", "softmd5", "soft_md5", "softwareName"} {
		key := compatibilityFixture(t, hash, "ShenZaoLocalPlayer41")
		key.Data[field] = strings.ToUpper(hash)
		got, err := resolveCompatibleKey(key, "", nil, func() (Player, error) { t.Fatal("portable capture looked up a local player"); return Player{}, nil })
		if err != nil || got.AppHash != hash || got.Source != "record" {
			t.Fatalf("field=%s result=%+v error=%v", field, got, err)
		}
	}
}

func TestCompatibilityUsesDynamicConfigurationAndOneLookupPerBatch(t *testing.T) {
	hash := strings.Repeat("b", 32)
	key := compatibilityFixture(t, hash, "ShenZaoLocalPlayer41")
	for _, source := range []string{"settings", "environment", "player"} {
		var manual *Player
		env := ""
		if source == "settings" {
			manual = &Player{SoftwareName: "new", AppMD5: hash}
		}
		if source == "environment" {
			env = hash
		}
		calls := 0
		resolve := newKeyResolver(env, manual, func() (Player, error) { calls++; return Player{AppMD5: hash}, nil })
		for range 2 {
			got, err := resolve(key)
			if err != nil || got.AppHash != hash || got.Source != source {
				t.Fatalf("source=%s result=%+v error=%v", source, got, err)
			}
		}
		if source == "player" && calls != 1 || source != "player" && calls != 0 {
			t.Fatalf("%s: lookup calls=%d", source, calls)
		}
	}
}

func TestCompatibilityDoesNotGuessFromProtocolLabel(t *testing.T) {
	key := compatibilityFixture(t, strings.Repeat("f", 32), "ShenZaoLocalPlayer41")
	before, _ := json.Marshal(key)
	if _, err := resolveCompatibleKey(key, "", nil, func() (Player, error) { return Player{}, errors.New("not installed") }); err == nil {
		t.Fatal("unverified hash accepted based on protocol label")
	}
	after, _ := json.Marshal(key)
	if !bytes.Equal(before, after) {
		t.Fatal("failed compatibility attempt changed source")
	}
	key.Data["appMd5"] = "invalid"
	if _, err := resolveCompatibleKey(key, "", nil, func() (Player, error) { t.Fatal("malformed key triggered lookup"); return Player{}, nil }); err == nil {
		t.Fatal("invalid explicit hash accepted")
	}
}

func TestCompatibilityKeepsPortableResolvedKeysStable(t *testing.T) {
	key := compatibilityFixture(t, legacyWindowsAppHash, "ShenZaoLocalPlayer41")
	key.Data["appMd5"] = legacyWindowsAppHash
	got, err := resolveCompatibleKey(key, "", nil, func() (Player, error) { t.Fatal("resolved record required local player"); return Player{}, nil })
	if err != nil || got.Adjusted {
		t.Fatalf("resolved record changed: %+v %v", got, err)
	}
	if _, err := Mima(got.Key.Data, got.AppHash); err != nil {
		t.Fatal(err)
	}
}

func TestCompatibilityLegacyEnvelopeRunsFullPipeline(t *testing.T) {
	toolsAvailable(t)
	key := compatibilityFixture(t, legacyWindowsAppHash, "ShenZaoLocalPlayer41")
	got, err := resolveCompatibleKey(key, "", nil, func() (Player, error) { return Player{}, errors.New("not installed") })
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "output.mp4")
	result, err := Run(context.Background(), "testdata/encrypted.sz", out, got.Key, got.AppHash, nil, nil)
	if err != nil || result["check"].(map[string]any)["valid"] != true {
		t.Fatalf("legacy pipeline failed: %v", err)
	}
}

func TestCompatibilityRetainsKnownMacParameters(t *testing.T) {
	for version, hash := range KnownPlayers {
		key := compatibilityFixture(t, hash, version)
		got, err := resolveCompatibleKey(key, "", nil, func() (Player, error) { return Player{}, errors.New("not installed") })
		if err != nil || got.AppHash != hash || got.Source != "historical" {
			t.Fatalf("known Mac profile %s failed: %v", version, err)
		}
	}
}

func TestCompatibilitySkipsHashesForPlainAndDirectKeys(t *testing.T) {
	raw, err := os.ReadFile("testdata/key.json")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := ReadKey(raw)
	if err != nil {
		t.Fatal(err)
	}
	keys := []Key{
		plain,
		{Data: map[string]any{"pattern": "4", "mimatuokeqian": "direct-key"}},
		{Data: map[string]any{"pattern": "szpd", "aesKey": strings.Repeat("ab", 32), "aesIV": strings.Repeat("cd", 16)}},
	}
	for _, key := range keys {
		got, err := resolveCompatibleKey(key, "", nil, func() (Player, error) { t.Fatal("direct key attempted player discovery"); return Player{}, nil })
		if err != nil || got.Adjusted || got.AppHash != "" || !reflect.DeepEqual(key, got.Key) {
			t.Fatalf("direct key changed: %v", err)
		}
	}
}
