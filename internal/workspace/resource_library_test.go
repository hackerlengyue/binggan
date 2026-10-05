package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"time.haomen/binggan/v2/internal/store"
)

func TestLibraryOnlySearchesAndReturnsVideoInformation(t *testing.T) {
	app := testApp(t)
	source := filepath.Join(t.TempDir(), "result.mp4")
	if err := os.WriteFile(source, []byte("fixture-video"), 0600); err != nil {
		t.Fatal(err)
	}
	resource := store.Resource{ID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", Name: "video-example.mp4", TaskID: "hidden-id", TaskName: "private-task-name", FinishedAt: store.Now()}
	if err := app.Store.PublishResource(app.Config.DataDir, source, resource); err != nil {
		t.Fatal(err)
	}
	page, err := app.Resources(ResourceQuery{Page: 1, PageSize: 20, Search: "video-example"})
	if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != resource.ID || page.Items[0].Name != resource.Name {
		t.Fatalf("resource list = %+v, %v", page, err)
	}
	hidden, err := app.Resources(ResourceQuery{Page: 1, PageSize: 20, Search: "private-task-name"})
	if err != nil || hidden.Total != 0 {
		t.Fatalf("task name matched resource search: %+v, %v", hidden, err)
	}
	if _, err := app.Resources(ResourceQuery{Page: 1, PageSize: 999}); err == nil {
		t.Fatal("invalid resource page size accepted")
	}
}
