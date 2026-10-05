package playerautomation

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	BundleID       = "ZZDispatch.szPlayer-Mac"
	ExecutableName = "SzPlayer"
)

var (
	ErrPermission  = errors.New("需要在系统设置中允许饼干大小姐控制其他应用")
	ErrNotFound    = errors.New("未找到 SzPlayer")
	ErrUnverified  = errors.New("当前 SzPlayer 版本尚未完成静态适配验证")
	ErrUnsupported = errors.New("当前系统尚不支持播放编排")
)

type Status struct {
	Platform           string `json:"platform"`
	Installed          bool   `json:"installed"`
	Running            bool   `json:"running"`
	AccessibilityReady bool   `json:"accessibilityReady"`
	VerifiedProfile    bool   `json:"verifiedProfile"`
	Version            string `json:"version,omitempty"`
	Message            string `json:"message"`
}

type Element struct {
	Role        string  `json:"role"`
	Title       string  `json:"title,omitempty"`
	Value       string  `json:"value,omitempty"`
	Placeholder string  `json:"placeholder,omitempty"`
	Identifier  string  `json:"identifier,omitempty"`
	Width       float64 `json:"width,omitempty"`
	Depth       int     `json:"depth,omitempty"`
	Level       int     `json:"level,omitempty"`
	CanExpand   bool    `json:"canExpand,omitempty"`
	Expanded    bool    `json:"expanded,omitempty"`
}

// ReadIssue marks a retryable observation failure. Elements remain available
// for blocking-dialog detection, but must not advance playback or completion.
type ReadIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Snapshot struct {
	Running    bool       `json:"running"`
	Elements   []Element  `json:"elements"`
	At         string     `json:"at"`
	PID        int        `json:"pid,omitempty"`
	WindowMode string     `json:"windowMode,omitempty"`
	ReadIssue  *ReadIssue `json:"readIssue,omitempty"`
}

type retryablePlaybackRead struct{ issue ReadIssue }

func (e *retryablePlaybackRead) Error() string { return e.issue.Message }
func retryableRead(code, message string) error {
	return &retryablePlaybackRead{issue: ReadIssue{Code: code, Message: message}}
}
func markReadIssue(current Snapshot, err error) (Snapshot, error) {
	var retry *retryablePlaybackRead
	if errors.As(err, &retry) {
		current.ReadIssue = &retry.issue
		return current, nil
	}
	return current, err
}

type CourseTargetRequest struct {
	Path        []string `json:"path"`
	ExpectedPID int      `json:"expectedPid,omitempty"`
}

func validateCourseTarget(request CourseTargetRequest) (CourseTargetRequest, error) {
	if len(request.Path) == 0 || len(request.Path) > 32 || request.ExpectedPID < 0 {
		return request, fmt.Errorf("课程定位路径无效")
	}
	request.Path = append([]string(nil), request.Path...)
	for i, part := range request.Path {
		part = strings.TrimSpace(part)
		if part == "" || len(part) > 1024 || strings.ContainsAny(part, "\x00\n\r") {
			return request, fmt.Errorf("课程定位路径包含无效名称")
		}
		request.Path[i] = part
	}
	return request, nil
}

type ClickRequest struct {
	Path         []string `json:"path,omitempty"`
	ExpectedPID  int      `json:"expectedPid,omitempty"`
	Text         string   `json:"text"`
	ClickCount   int      `json:"clickCount"`
	MatchOrdinal int      `json:"matchOrdinal,omitempty"`
	MatchCount   int      `json:"matchCount,omitempty"`
}

type Driver interface {
	Status() Status
	Snapshot() (Snapshot, error)
	PlaybackSnapshot() (Snapshot, error)
	CourseSnapshot() (Snapshot, error)
	RevealCourse(CourseTargetRequest) (Snapshot, error)
	OpenCourse(string) error
	ExpandFolder(string) error
	SetVolume(int) error
	SetFullscreen(bool) error
	SetPlaybackRate(float64) error
	TogglePlayback() error
	Click(ClickRequest) error
	RequestAccessibilityPermission() error
}

func New() Driver { return newDriver() }

func snapshot(running bool, elements []Element) Snapshot {
	if elements == nil {
		elements = []Element{}
	}
	return Snapshot{Running: running, Elements: elements, At: time.Now().UTC().Format(time.RFC3339Nano)}
}

// The 26.06.54 finish observer writes these two controls even when the bar is
// hidden. Calling showMediaBar afterwards would replace them with the final
// decoded frame's position, which can be less than the media duration.
func hasPlaybackCompletionMarker(current Snapshot) bool {
	var sliderEnded, clockEnded bool
	var terminalDuration int
	for _, element := range current.Elements {
		if element.Role == "AXSlider" && element.Identifier == "_NS:26" {
			value, err := strconv.ParseFloat(strings.TrimSpace(element.Value), 64)
			if err != nil || value != 100 {
				return false
			}
			sliderEnded = true
		}
		if element.Role != "AXStaticText" || element.Identifier != "_NS:8" {
			continue
		}
		for _, label := range []string{element.Value, element.Title} {
			pair := strings.Split(label, "/")
			if len(pair) != 2 {
				continue
			}
			elapsed, validElapsed := playbackClockSeconds(pair[0])
			duration, validDuration := playbackClockSeconds(pair[1])
			if !validElapsed || !validDuration || duration <= 0 || elapsed != duration {
				return false
			}
			if clockEnded && terminalDuration != duration {
				return false
			}
			terminalDuration = duration
			clockEnded = true
		}
	}
	return sliderEnded && clockEnded
}

func playbackClockSeconds(label string) (int, bool) {
	parts := strings.Split(strings.TrimSpace(label), ":")
	if len(parts) != 3 {
		return 0, false
	}
	seconds := 0
	for i, part := range parts {
		if len(part) < 2 || len(part) > 6 || (i > 0 && len(part) != 2) {
			return 0, false
		}
		for _, digit := range part {
			if digit < '0' || digit > '9' {
				return 0, false
			}
		}
		value, err := strconv.Atoi(part)
		if err != nil || (i > 0 && value >= 60) {
			return 0, false
		}
		seconds = seconds*60 + value
	}
	return seconds, true
}

func refreshedPlaybackSnapshot(read func() (Snapshot, error), refresh func() error) (Snapshot, error) {
	current, err := read()
	if err != nil || !current.Running || current.ReadIssue != nil || hasPlaybackCompletionMarker(current) {
		return markReadIssue(current, err)
	}
	// Windowed controls update without pointer input, including in background.
	// For fullscreen, return the observed state: the orchestrator recovers to a
	// window if controls disappear/stall. A posted hover cannot ensure delivery.
	if current.WindowMode == "windowed" || current.WindowMode == "fullscreen" {
		return current, nil
	}
	if err := refresh(); err != nil {
		return markReadIssue(current, err)
	}
	current, err = read()
	return markReadIssue(current, err)
}

func courseSnapshotFromRoots(roots []string, running bool) (Snapshot, error) {
	elements := []Element{{Role: "AXOutline", Depth: 1}}
	for _, root := range roots {
		root = filepath.Clean(strings.TrimSpace(root))
		if root == "." || root == "" {
			continue
		}
		entryElements, _, err := courseElements(root, 0)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return Snapshot{}, fmt.Errorf("无法读取课程目录 %s：%w", filepath.Base(root), err)
		}
		elements = append(elements, entryElements...)
	}
	return snapshot(running, elements), nil
}

type folderKey struct {
	ordinal int
	name    string
}

type visibleFolder struct {
	key      folderKey
	expanded bool
}

func visibleFolders(current Snapshot) []visibleFolder {
	rowOrdinal := -1
	folders := make([]visibleFolder, 0)
	for index, row := range current.Elements {
		if row.Role != "AXRow" {
			continue
		}
		rowOrdinal++
		if !row.CanExpand {
			continue
		}
		name := strings.TrimSpace(row.Value)
		for _, child := range current.Elements[index+1:] {
			if name != "" {
				break
			}
			if child.Depth <= row.Depth {
				break
			}
			if child.Role == "AXStaticText" {
				name = strings.TrimSpace(child.Value)
				if name == "" {
					name = strings.TrimSpace(child.Title)
				}
				if name != "" {
					break
				}
			}
		}
		if name == "" && !strings.EqualFold(strings.TrimSpace(row.Title), "levelFoldIcon") {
			name = strings.TrimSpace(row.Title)
		}
		key := folderKey{ordinal: rowOrdinal, name: name}
		folders = append(folders, visibleFolder{key: key, expanded: row.Expanded})
	}
	return folders
}

func nextCollapsedFolder(current Snapshot, attempted map[folderKey]bool) (folderKey, bool, bool) {
	collapsed := false
	for _, folder := range visibleFolders(current) {
		if folder.expanded {
			continue
		}
		collapsed = true
		if folder.key.name != "" && !attempted[folder.key] {
			return folder.key, true, true
		}
	}
	return folderKey{}, false, collapsed
}

func courseElements(path string, level int) ([]Element, int, error) {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		return nil, 0, err
	}
	if !info.IsDir() {
		if !strings.EqualFold(filepath.Ext(info.Name()), ".sz") {
			return nil, 0, nil
		}
		name := strings.TrimSpace(strings.TrimSuffix(info.Name(), filepath.Ext(info.Name())))
		return []Element{
			{Role: "AXRow", Identifier: path, Depth: 2, Level: level},
			{Role: "AXStaticText", Value: name, Depth: 3},
		}, 1, nil
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, 0, err
	}
	sort.Slice(entries, func(i, j int) bool {
		return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name())
	})
	children := make([]Element, 0)
	count := 0
	for _, entry := range entries {
		entryElements, entryCount, entryErr := courseElements(filepath.Join(path, entry.Name()), level+1)
		if errors.Is(entryErr, os.ErrNotExist) {
			continue
		}
		if entryErr != nil {
			return nil, 0, entryErr
		}
		children = append(children, entryElements...)
		count += entryCount
	}
	if count == 0 {
		return nil, 0, nil
	}
	name := filepath.Base(path)
	elements := []Element{
		{Role: "AXRow", Depth: 2, Level: level, CanExpand: true, Expanded: true},
		{Role: "AXStaticText", Value: name, Depth: 3},
	}
	return append(elements, children...), count, nil
}

func validateClick(request ClickRequest) (ClickRequest, error) {
	if len(request.Path) > 0 {
		target, err := validateCourseTarget(CourseTargetRequest{Path: request.Path, ExpectedPID: request.ExpectedPID})
		if err != nil {
			return request, err
		}
		if request.ClickCount != 2 {
			return request, fmt.Errorf("课程路径仅可用于双击课程")
		}
		request.Path = target.Path
		// Full paths are resolved afresh; legacy text ordinals do not apply.
		request.MatchCount, request.MatchOrdinal = 0, 0
		return request, nil
	}

	request.Text = strings.TrimSpace(request.Text)
	if request.Text == "" || len([]rune(request.Text)) > 120 {
		return request, fmt.Errorf("控件文字须为 1–120 个字符")
	}
	if request.ClickCount == 0 {
		request.ClickCount = 1
	}
	if request.ClickCount != 1 && request.ClickCount != 2 {
		return request, fmt.Errorf("只支持单击或双击")
	}
	if request.MatchCount != 0 {
		if request.ClickCount != 2 || request.MatchCount < 2 || request.MatchCount > 1000 ||
			request.MatchOrdinal < 0 || request.MatchOrdinal >= request.MatchCount {
			return request, fmt.Errorf("同名课程匹配参数无效")
		}
	} else if request.MatchOrdinal != 0 {
		return request, fmt.Errorf("同名课程匹配参数不完整")
	}
	return request, nil
}

func validatePlaybackRate(rate float64) error {
	if rate < 0.5 || rate > 2 || rate != rate ||
		math.Abs(rate*10-math.Round(rate*10)) > 0.000001 {
		return fmt.Errorf("播放倍速须为 0.5–2.0，间隔 0.1")
	}
	return nil
}

func platformStatus() Status { return Status{Platform: runtime.GOOS} }
