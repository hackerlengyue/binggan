package playerautomation

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlaybackRateMatchesSzPlayerMenu(t *testing.T) {
	for _, rate := range []float64{0.5, 0.7, 1, 1.5, 2} {
		if err := validatePlaybackRate(rate); err != nil {
			t.Fatalf("rate %.2f rejected: %v", rate, err)
		}
	}
	for _, rate := range []float64{0, 0.49, 0.55, 2.1, math.NaN(), math.Inf(1)} {
		if err := validatePlaybackRate(rate); err == nil {
			t.Fatalf("rate %.2f accepted", rate)
		}
	}
}

func TestEmptySnapshotSerializesElementsAsArray(t *testing.T) {
	data, err := json.Marshal(snapshot(false, nil))
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]json.RawMessage
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	if string(value["elements"]) != "[]" {
		t.Fatalf("elements = %s, want []", value["elements"])
	}
}

func TestValidateClick(t *testing.T) {
	request, err := validateClick(ClickRequest{Text: "  课程一  "})
	if err != nil || request.Text != "课程一" || request.ClickCount != 1 {
		t.Fatalf("%+v %v", request, err)
	}
	if _, err = validateClick(ClickRequest{Text: ""}); err == nil {
		t.Fatal("accepted empty text")
	}
	if _, err = validateClick(ClickRequest{Text: "播放", ClickCount: 3}); err == nil {
		t.Fatal("accepted triple click")
	}
	if _, err = validateClick(ClickRequest{Text: "第一讲", ClickCount: 2, MatchOrdinal: 1, MatchCount: 2}); err != nil {
		t.Fatalf("rejected a bounded course row match: %v", err)
	}
	for _, request := range []ClickRequest{
		{Text: "第一讲", ClickCount: 2, MatchOrdinal: 2, MatchCount: 2},
		{Text: "第一讲", ClickCount: 1, MatchOrdinal: 1, MatchCount: 2},
		{Text: "第一讲", ClickCount: 2, MatchOrdinal: 1},
	} {
		if _, err = validateClick(request); err == nil {
			t.Fatalf("accepted invalid course row match: %+v", request)
		}
	}
}

func TestCourseSnapshotFromLoadedFolders(t *testing.T) {
	root := t.TempDir()
	chapter := filepath.Join(root, "章节二")
	if err := os.Mkdir(chapter, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(root, "课程一 .sz"),
		filepath.Join(chapter, "课程二.sz"),
		filepath.Join(root, "说明.txt"),
	} {
		if err := os.WriteFile(path, []byte("fixture"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	result, err := courseSnapshotFromRoots([]string{root}, false)
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, element := range result.Elements {
		if element.Role == "AXStaticText" {
			texts = append(texts, element.Value)
		}
	}
	want := []string{filepath.Base(root), "章节二", "课程二", "课程一"}
	if len(texts) != len(want) {
		t.Fatalf("texts = %q, want %q", texts, want)
	}
	for index := range want {
		if texts[index] != want[index] {
			t.Fatalf("texts = %q, want %q", texts, want)
		}
	}
}

func TestEmptyAndRemovedCatalogsReturnEmptyOutline(t *testing.T) {
	root := t.TempDir()
	for _, roots := range [][]string{nil, {root}, {filepath.Join(root, "removed")}} {
		result, err := courseSnapshotFromRoots(roots, true)
		if err != nil || !result.Running || len(result.Elements) != 1 || result.Elements[0].Role != "AXOutline" {
			t.Fatalf("roots=%v result=%+v err=%v", roots, result, err)
		}
	}
}

func TestUnreadableCatalogIsNotReportedAsEmpty(t *testing.T) {
	root := t.TempDir()
	blocked := filepath.Join(root, "blocked")
	if err := os.Mkdir(blocked, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(blocked, 0o700)
	if _, err := os.ReadDir(blocked); err == nil {
		t.Skip("filesystem does not enforce fixture permissions")
	}
	if err := os.WriteFile(filepath.Join(root, "readable.sz"), []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, roots := range [][]string{{blocked}, {root}} {
		if _, err := courseSnapshotFromRoots(roots, true); !errors.Is(err, os.ErrPermission) {
			t.Fatalf("unreadable root or child must preserve permission failure, got %v", err)
		}
	}
}

func TestCollapsedFoldersWithSameNameKeepDistinctRows(t *testing.T) {
	current := snapshot(true, []Element{
		{Role: "AXRow", Depth: 2, CanExpand: true},
		{Role: "AXStaticText", Depth: 3, Value: "章节一"},
		{Role: "AXRow", Depth: 2, CanExpand: true},
		{Role: "AXStaticText", Depth: 3, Value: "章节一"},
	})
	attempted := map[folderKey]bool{}
	first, found, collapsed := nextCollapsedFolder(current, attempted)
	if !found || !collapsed || first.ordinal != 0 || first.name != "章节一" {
		t.Fatalf("first=%+v found=%v collapsed=%v", first, found, collapsed)
	}
	attempted[first] = true
	second, found, collapsed := nextCollapsedFolder(current, attempted)
	if !found || !collapsed || second.ordinal != 1 || second.name != "章节一" {
		t.Fatalf("second=%+v found=%v collapsed=%v", second, found, collapsed)
	}
	attempted[second] = true
	_, found, collapsed = nextCollapsedFolder(current, attempted)
	if found || !collapsed {
		t.Fatalf("incomplete folder expansion hidden: found=%v collapsed=%v", found, collapsed)
	}
}

func TestVisibleFolderNameCanBeOnTheRowItself(t *testing.T) {
	current := snapshot(true, []Element{{Role: "AXRow", Depth: 2, Value: "第五阶段", Title: "levelFoldIcon", CanExpand: true}})
	folders := visibleFolders(current)
	if len(folders) != 1 || folders[0].key.name != "第五阶段" {
		t.Fatalf("visible folders = %+v", folders)
	}
}

func TestDarwinCourseClickNeverUsesTheGlobalPointer(t *testing.T) {
	source, err := os.ReadFile("automation_darwin.go")
	if err != nil {
		t.Fatal(err)
	}
	mouse, err := os.ReadFile("mouse_darwin.m")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source) + string(mouse)
	for _, forbidden := range []string{
		"CGEventPost(kCGHIDEventTap",
		"kCGEventMouseMoved",
		`exec.Command("/usr/bin/open", "-g", "-b", BundleID, resolved)`,
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("Darwin course opening contains forbidden global/file-open path %q", forbidden)
		}
	}
	if !strings.Contains(text, "CGEventPostToPid(pid, event)") ||
		!strings.Contains(text, "postPlayerDoubleClick(pid, match.point.x, match.point.y)") {
		t.Fatal("Darwin course opening is not routed directly to the SzPlayer process")
	}
	if !strings.Contains(text, "findCoursePoint(app, needle") {
		t.Fatal("Darwin course opening does not constrain geometry lookup to the matching AXRow")
	}
}

func TestCourseTargetRequiresBoundedPathAndDoubleClick(t *testing.T) {
	for _, request := range []CourseTargetRequest{
		{}, {Path: []string{""}}, {Path: []string{"folder", "\n"}},
		{Path: []string{strings.Repeat("x", 1025)}},
		{Path: []string{"lesson"}, ExpectedPID: -1},
		{Path: strings.Split(strings.Repeat("a/", 33), "/")},
	} {
		if _, err := validateCourseTarget(request); err == nil {
			t.Fatalf("accepted invalid target: %#v", request)
		}
	}
	if _, err := validateClick(ClickRequest{Text: "lesson", ClickCount: 1, Path: []string{"folder", "lesson"}}); err == nil {
		t.Fatal("path locator accepted a single click")
	}
	if _, err := validateClick(ClickRequest{Text: "lesson", ClickCount: 2, Path: []string{"folder", "lesson"}, ExpectedPID: 123}); err != nil {
		t.Fatal(err)
	}
}

func TestPathClickDoesNotUseLegacyDuplicateOrdinals(t *testing.T) {
	for _, request := range []ClickRequest{
		{Text: "第一讲", ClickCount: 2, Path: []string{"目录", "第一讲"}, MatchCount: 1},
		{Text: "第一讲", ClickCount: 2, Path: []string{"目录", "第一讲"}, MatchOrdinal: 1, MatchCount: 2},
		{Text: strings.Repeat("长", 121), ClickCount: 2, Path: []string{"目录", strings.Repeat("长", 121)}},
	} {
		got, err := validateClick(request)
		if err != nil {
			t.Fatalf("valid full path rejected: %v", err)
		}
		if got.MatchCount != 0 || got.MatchOrdinal != 0 {
			t.Fatal("path locator retained stale ordinal constraints")
		}
	}
}
