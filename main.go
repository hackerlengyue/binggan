package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/egoist/mygo"
	"time.haomen/binggan/v2/internal/tasks"
	"time.haomen/binggan/v2/internal/tunnel"
)

// Navigate is delivered from the native menu to the Vue router.
var Navigate = mygo.NewEvent[string]("desktop:navigate")
var DecryptTaskFinished = mygo.NewEvent[tasks.Finished]("desktop:decrypt-task-finished")

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--surge-capture-helper" {
		if len(os.Args) != 4 {
			os.Exit(2)
		}
		if err := tunnel.RunSurgeHelper(os.Args[2], os.Args[3]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	// The privileged helper must never open a WebView or the user database.
	if len(os.Args) > 1 && os.Args[1] == "--capture-helper" {
		if len(os.Args) != 4 {
			os.Exit(2)
		}
		if err := tunnel.RunHelper(os.Args[2], os.Args[3]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := configureStableUserDataPath(); err != nil {
		log.Fatal(err)
	}
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	workbench := newWorkbench()
	defer workbench.close()
	var instanceMu sync.Mutex
	var instanceWindow *mygo.Window
	var secondInstancePending bool
	var instanceStopping bool
	mygo.App.OnSecondInstance(func(_ []string, _ string) {
		instanceMu.Lock()
		if instanceStopping {
			instanceMu.Unlock()
			return
		}
		window := instanceWindow
		if window == nil {
			secondInstancePending = true
			instanceMu.Unlock()
			return
		}
		instanceMu.Unlock()
		showMainWindow(window)
	})
	mygo.App.OnWillQuit(func(_ *mygo.QuitEvent) {
		instanceMu.Lock()
		instanceStopping = true
		instanceWindow = nil
		secondInstancePending = false
		instanceMu.Unlock()
		// Finish releasing the database, listener and capture lease before
		// MyGo's OnQuit handler releases the single-instance lock.
		workbench.close()
	})
	mygo.Bind(&CaptureService{workbench})
	mygo.Bind(&PlayerService{workbench})
	mygo.Bind(&NotificationService{workbench})
	mygo.Bind(&WorkspaceService{workbench})
	lifecycle := newLifecycleService()
	mygo.Bind(lifecycle)
	mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
		if !lifecycle.allowQuit() {
			e.PreventDefault()
		}
	})
	if err := mygo.Protocol.Handle("binggan-stream", workbench.streamHandler()); err != nil {
		log.Fatal(err)
	}
	mygo.App.SetMenu(workbench.menu())
	mygo.App.WhenReady(func() {
		workbench.window = mygo.NewWindow(mygo.WindowOptions{
			Title: "饼干大小姐", URL: "/", Width: 1320, Height: 880,
			MinWidth: 1000, MinHeight: 680, StateKey: "main",
			BackgroundColor: "#fafafa", TitleBarStyle: mygo.TitleBarHiddenInset,
		})
		cleanupTray := installWindowTray(workbench.window, lifecycle)
		lifecycle.reveal = func() { showMainWindow(workbench.window) }
		mygo.App.OnWillQuit(func(*mygo.QuitEvent) { cleanupTray() })
		instanceMu.Lock()
		if !instanceStopping {
			instanceWindow = workbench.window
		}
		focus := secondInstancePending && !instanceStopping
		secondInstancePending = false
		instanceMu.Unlock()
		if focus {
			showMainWindow(workbench.window)
		}
		workbench.downloads = installDownloads(workbench.window)
		workbench.window.Page().OnDOMReady(func() {
			go func() {
				<-workbench.ready
				if workbench.monitor != nil {
					workbench.monitor.domReady(workbench.ctx)
				}
			}()
		})
		workbench.started = true
		go workbench.start()
	})
	if err := mygo.App.Run(); err != nil {
		log.Print(err)
	}
}

const v2UserDataDirName = "饼干大小姐 V2"

func configureStableUserDataPath() error {
	// BINGGAN_USER_DATA_DIR points a preview or test run at its own data, so it
	// never opens the installed app's database.
	if dir := os.Getenv("BINGGAN_USER_DATA_DIR"); dir != "" {
		mygo.App.SetPath(mygo.PathUserData, dir)
		return nil
	}
	appData, err := mygo.App.Path(mygo.PathAppData)
	if err != nil {
		return err
	}
	mygo.App.SetPath(mygo.PathUserData, filepath.Join(appData, v2UserDataDirName))
	return nil
}
