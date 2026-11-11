package engine

import (
	"errors"
	"maps"
	"strings"
	"sync"
)

// This value comes from the Windows player's fallback constant and has been
// verified against a real legacy capture and video. It is a candidate, never a
// version-to-hash mapping: each encrypted password must validate independently.
const legacyWindowsAppHash = "a93b82a7f6277f8a30a48f460dcb87c2"

type KeyResolution struct {
	Key      Key
	AppHash  string
	Source   string
	Adjusted bool
}

// NewKeyResolver shares one native player lookup across a batch. The lookup is
// lazy, so portable records with their own valid hash need no installed player.
func NewKeyResolver(env string, fallback *Player) func(Key) (KeyResolution, error) {
	return newKeyResolver(env, fallback, DetectPlayer)
}

func newKeyResolver(env string, fallback *Player, lookup func() (Player, error)) func(Key) (KeyResolution, error) {
	var once sync.Once
	var player Player
	var detectionErr error
	detect := func() (Player, error) {
		once.Do(func() { player, detectionErr = lookup() })
		return player, detectionErr
	}
	return func(key Key) (KeyResolution, error) {
		return resolveCompatibleKey(key, env, fallback, detect)
	}
}

func resolveCompatibleKey(key Key, env string, fallback *Player, detect func() (Player, error)) (KeyResolution, error) {
	if err := key.Validate(); err != nil {
		return KeyResolution{}, err
	}
	if str(key.Data["pattern"]) == "4" || key.IsSZPD() {
		return KeyResolution{Key: key, Source: "not-needed"}, nil
	}
	if _, err := Mima(key.Data, ""); err == nil {
		return KeyResolution{Key: key, Source: "not-needed"}, nil
	}
	seen := make(map[string]bool)
	var result KeyResolution
	try := func(hash, source string) bool {
		hash = strings.ToLower(strings.TrimSpace(hash))
		if !HashValid(hash) || seen[hash] {
			return false
		}
		seen[hash] = true
		if _, err := Mima(key.Data, hash); err != nil {
			return false
		}
		// Only appMd5 changes. The ciphertext, uit, protocol identity, slot
		// indexes and all media parameters stay bound to their original capture.
		prepared := Key{Data: maps.Clone(key.Data), Passwords: key.Passwords}
		prepared.Data["appMd5"] = hash
		result = KeyResolution{
			Key: prepared, AppHash: hash, Source: source,
			Adjusted: str(key.Data["appMd5"]) != hash,
		}
		return true
	}
	for _, field := range []string{"appMd5", "app_md5", "softMd5", "softmd5", "soft_md5"} {
		if try(pick(key.Data, field), "record") {
			return result, nil
		}
	}
	version := pick(key.Data, "softwareName", "softwarename", "software_name")
	if try(version, "record") {
		return result, nil
	}
	if fallback != nil && try(fallback.AppMD5, "settings") {
		return result, nil
	}
	if try(env, "environment") {
		return result, nil
	}
	if player, err := detect(); err == nil && try(player.AppMD5, "player") {
		return result, nil
	}
	if try(KnownPlayers[version], "historical") {
		return result, nil
	}
	if windowsProtocolVersionPattern.MatchString(version) && try(legacyWindowsAppHash, "historical") {
		return result, nil
	}
	return KeyResolution{}, errors.New("无法匹配密钥的 App 校验值，请提供生成这份密钥的播放器校验值；修改版本标识无法替代校验值")
}
