package workspace

import (
	"encoding/json"
	"fmt"
	"strings"

	"time.haomen/binggan/v2/internal/engine"
)

type PreparedKey struct {
	KeyJSON  string `json:"keyJson"`
	Adjusted bool   `json:"adjusted"`
	Source   string `json:"source"`
}

func (a *App) keyResolver() (func(engine.Key) (engine.KeyResolution, error), error) {
	settings, err := a.Settings()
	if err != nil {
		return nil, err
	}
	var manual *engine.Player
	if settings.Mode == "manual" {
		manual = &engine.Player{SoftwareName: settings.SoftwareName, AppMD5: settings.AppMD5}
	}
	return engine.NewKeyResolver(a.Config.AppMD5, manual), nil
}

// PrepareKeys validates the whole upload before history is changed. Each copy
// carries its own verified hash, independently of future player upgrades.
func (a *App) PrepareKeys(raws []string) ([]PreparedKey, error) {
	if len(raws) < 1 || len(raws) > 100 {
		return nil, invalidTask("每次请选择 1–100 个密钥文件。")
	}
	var total int
	for _, raw := range raws {
		if len(raw) > 1<<20 {
			return nil, invalidTask("密钥文件不能超过 1 MB。")
		}
		total += len(raw)
		if total > 32<<20 {
			return nil, invalidTask("密钥文件过大")
		}
	}
	resolve, err := a.keyResolver()
	if err != nil {
		return nil, err
	}
	prepared := make([]PreparedKey, 0, len(raws))
	for index, raw := range raws {
		raw = strings.TrimPrefix(raw, "\ufeff")
		key, err := engine.ReadKey([]byte(raw))
		if err != nil {
			return nil, invalidTask(fmt.Sprintf("第 %d 份密钥：%s", index+1, err))
		}
		result, err := resolve(key)
		if err != nil {
			return nil, invalidTask(fmt.Sprintf("第 %d 份密钥：%s", index+1, err))
		}
		if result.Adjusted {
			// Retain unknown export metadata as well as the original passwords.
			var document map[string]json.RawMessage
			if err := json.Unmarshal([]byte(raw), &document); err != nil {
				return nil, err
			}
			data, err := json.Marshal(result.Key.Data)
			if err != nil {
				return nil, err
			}
			document["getPwdData"] = data
			encoded, err := json.Marshal(document)
			if err != nil {
				return nil, err
			}
			raw = string(encoded)
		}
		prepared = append(prepared, PreparedKey{KeyJSON: raw, Adjusted: result.Adjusted, Source: result.Source})
	}
	return prepared, nil
}
