package workspace

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"time.haomen/binggan/v2/internal/engine"
	"time.haomen/binggan/v2/internal/tasks"
)

func legacyKeyJSON(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("../engine/testdata/key-legacy-windows.json")
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestPrepareLegacyKeysCorrectsOnlyVerifiedAppHash(t *testing.T) {
	app := testApp(t)
	app.Config.AppMD5 = strings.Repeat("e", 32)
	raw := legacyKeyJSON(t)
	var original map[string]any
	if err := json.Unmarshal([]byte(raw), &original); err != nil {
		t.Fatal(err)
	}
	original["exportMetadata"] = map[string]any{"keep": "unchanged"}
	data := original["getPwdData"].(map[string]any)
	data["appMd5"] = strings.Repeat("e", 32)
	rawBytes, _ := json.Marshal(original)
	got, err := app.PrepareKeys([]string{"\ufeff" + string(rawBytes)})
	if err != nil || len(got) != 1 {
		t.Fatalf("prepare failed: %v", err)
	}
	if !got[0].Adjusted || got[0].Source != "historical" {
		t.Fatalf("matching source: %+v", got[0])
	}
	var prepared map[string]any
	if err := json.Unmarshal([]byte(got[0].KeyJSON), &prepared); err != nil {
		t.Fatal(err)
	}
	matched := prepared["getPwdData"].(map[string]any)
	if matched["appMd5"] != "a93b82a7f6277f8a30a48f460dcb87c2" {
		t.Fatal("legacy hash not retained")
	}
	delete(data, "appMd5")
	delete(matched, "appMd5")
	if !reflect.DeepEqual(original, prepared) {
		t.Fatal("preparation changed capture data or export metadata")
	}
	second, err := app.PrepareKeys([]string{got[0].KeyJSON})
	if err != nil || second[0].Adjusted || second[0].KeyJSON != got[0].KeyJSON {
		t.Fatalf("prepared key not idempotent: %v", err)
	}
}

func TestPrepareKeysRejectsWholeBatchWithoutChangingHistory(t *testing.T) {
	app := testApp(t)
	before, err := app.KeyHistory()
	if err != nil {
		t.Fatal(err)
	}
	got, err := app.PrepareKeys([]string{legacyKeyJSON(t), `{"getPwdData":{},"passwords":{}}`})
	if err == nil || got != nil {
		t.Fatal("partial batch reported as prepared")
	}
	after, err := app.KeyHistory()
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("preparation changed key history")
	}
	for _, raws := range [][]string{nil, make([]string, 101), {strings.Repeat("x", 2<<20+1)}} {
		if _, err := app.PrepareKeys(raws); err == nil {
			t.Fatal("invalid import batch accepted")
		}
	}
}

func TestPrepareKeysPreservesMultiFileImportSizeLimit(t *testing.T) {
	app := testApp(t)
	raw := legacyKeyJSON(t) + strings.Repeat(" ", 600<<10)
	prepared, err := app.PrepareKeys([]string{raw, raw, raw, raw})
	if err != nil || len(prepared) != 4 {
		t.Fatalf("valid multi-file import over 2 MB rejected: %v", err)
	}
}

func TestLegacyTaskUsesSameCompatibilityAndRetainsPrivateRecipe(t *testing.T) {
	app := testApp(t)
	app.Config.AppMD5 = strings.Repeat("e", 32)
	key, err := engine.ReadKey([]byte(legacyKeyJSON(t)))
	if err != nil {
		t.Fatal(err)
	}
	// The golden encrypted media uses this synthetic slot password.
	key.Passwords = map[string]string{"1": "fixture-password"}
	key.Data["audio"] = 2
	raw, err := json.Marshal(key)
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("../engine/testdata/encrypted.sz")
	if err != nil {
		t.Fatal(err)
	}
	imported, err := app.ImportVideo(context.Background(), videoChoice(t, app, "legacy.sz", source), nil)
	if err != nil {
		t.Fatal(err)
	}
	view, err := app.CreateTask(CreateTaskInput{Name: "Legacy compatibility", Videos: []TaskVideoInput{{UploadID: imported.ID, KeyJSON: string(raw)}}})
	if err != nil {
		t.Fatal(err)
	}
	view = waitTask(t, app, view.ID)
	if view.Completed != 1 || view.Failed != 0 {
		t.Fatalf("legacy task failed: %s", view.Children[0].Error)
	}
	recipeRaw, err := os.ReadFile(app.Tasks.Path(view.Children[0].ID, "request.json"))
	if err != nil {
		t.Fatal(err)
	}
	var recipe tasks.Prepared
	if err := json.Unmarshal(recipeRaw, &recipe); err != nil {
		t.Fatal(err)
	}
	if recipe.AppHash != "a93b82a7f6277f8a30a48f460dcb87c2" || recipe.Key.Data["appMd5"] != recipe.AppHash {
		t.Fatal("retry recipe lost verified hash")
	}
	if recipe.Key.Data["密码"] != key.Data["密码"] || recipe.Key.Data["uit"] != key.Data["uit"] {
		t.Fatal("retry recipe changed envelope")
	}
}
