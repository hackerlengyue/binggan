package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo"
)

func TestRenamedAppKeepsV2UserDataPath(t *testing.T) {
	if os.Getenv("BINGGAN_MYGO_E2E") == "1" {
		t.Skip("the native WebView test owns MyGo's isolated data path")
	}
	base := t.TempDir()
	mygo.App.SetPath(mygo.PathAppData, base)
	mygo.App.SetPath(mygo.PathUserData, "")
	t.Cleanup(func() {
		mygo.App.SetPath(mygo.PathAppData, "")
		mygo.App.SetPath(mygo.PathUserData, "")
	})
	if err := configureStableUserDataPath(); err != nil {
		t.Fatal(err)
	}
	got, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(base, "饼干大小姐 V2")
	if got != want {
		t.Fatalf("renaming the app must preserve v2 data: got %q, want %q", got, want)
	}
}
