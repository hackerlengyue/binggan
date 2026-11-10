package workspace

import (
	"context"
	"encoding/binary"
	"strings"
	"testing"
)

func TestWindowsSZPDTaskPreflightBeforeAppHashLookup(t *testing.T) {
	app := testApp(t)
	raw := make([]byte, 0x24+16)
	copy(raw, "SZPD")
	binary.LittleEndian.PutUint32(raw[4:], 582)
	binary.LittleEndian.PutUint32(raw[12:], 8)
	binary.LittleEndian.PutUint32(raw[16:], 16)
	choice := videoChoice(t, app, "课程.sz", raw)
	imported, err := app.ImportVideo(context.Background(), choice, nil)
	if err != nil {
		t.Fatal(err)
	}
	oldKey := `{"passwords":{},"getPwdData":{"pattern":"4","mimatuokeqian":"test"}}`
	_, err = app.CreateTask(CreateTaskInput{Name: "wrong format", Videos: []TaskVideoInput{{UploadID: imported.ID, KeyJSON: oldKey}}})
	if err == nil || !strings.Contains(err.Error(), "SZPD 582") {
		t.Fatalf("format mismatch not reported: %v", err)
	}
	views, _ := app.Tasks.List()
	if len(views) != 0 {
		t.Fatal("invalid task was queued")
	}
	key := `{"passwords":{},"getPwdData":{"pattern":"szpd","aesKey":"` + strings.Repeat("ab", 32) + `","aesIV":"` + strings.Repeat("cd", 16) + `"}}`
	_, err = app.CreateTask(CreateTaskInput{Name: "AES parameters", Videos: []TaskVideoInput{{UploadID: imported.ID, KeyJSON: key}}})
	if err != nil {
		t.Fatalf("SZPD requires irrelevant app hash: %v", err)
	}
}
