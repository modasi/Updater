//go:build darwin
// +build darwin

package updater

import (
	"sync/atomic"

	"github.com/mojbro/gocoa"
	"github.com/sqweek/dialog"
)

var (
	isUpdateCancelled uint32
	MainWindow        *ProgressWindow
	cancelButton      gocoa.Button
)

const (
	WindowWidth  = 480
	WindowHeight = 320
)

type ProgressWindow struct {
	window      *gocoa.Window
	progressBar *gocoa.ProgressIndicator
	logTextView *gocoa.TextView
}

func init() {
	gocoa.InitApplication()
}

func NewMainWindow() (*ProgressWindow, error) {

	wnd := gocoa.NewCenteredWindow(AppName, WindowWidth, WindowHeight)

	progressBar := gocoa.NewProgressIndicator(12, 20, 440, 24)
	logTextView := gocoa.NewTextView(12, 100, 440, 180)
	cancelButton := gocoa.NewButton(300, 300, 100, 25)
	cancelButton.SetTitle("Cancel")

	wnd.AddProgressIndicator(progressBar)
	wnd.AddTextView(logTextView)
	wnd.AddButton(cancelButton)

	cancelButton.OnClick(func() {
		atomic.StoreUint32(&isUpdateCancelled, 1)
		gocoa.TerminateApplication()
	})

	return &ProgressWindow{
		window:      wnd,
		progressBar: progressBar,
		logTextView: logTextView,
	}, nil
}

func ShowMainWindow() {
	if MainWindow == nil {
		var err error
		MainWindow, err = NewMainWindow()
		if err != nil {
			return
		}
	}
	MainWindow.window.MakeKeyAndOrderFront()
}

func SetUpdateProgress(progress float64) {
	if MainWindow != nil {
		MainWindow.progressBar.SetValue(progress)
	}
}

var logText string

func AppendLogText(text string) {
	logText += text + "\n"
	if MainWindow != nil {
		MainWindow.logTextView.SetText(logText)
	}
}

func CloseWindow() {
	gocoa.TerminateApplication()
}

func SetUpdateComplete() {
	if MainWindow != nil {
		cancelButton.SetTitle("Done")
	}
}

func ShowUpdateErrorDialog(message string) {
	AppendLogText("Update Error: " + message)
	ShowMessageBox(AppName, message, 1)
}

func ShowUpdateConfirmDialog(message string) bool {

	return ShowMessageBox(AppName, message, 2) != 0
}

func ShowMessageBox(title, message string, uType uint) int32 {
	switch uType {
	case 1:
		dialog.Message("%s", message).Title(AppName).Error()
	case 2:
		if dialog.Message("%s", message).Title(AppName).YesNo() {
			return 1
		} else {
			return 0 // IDNO
		}
	default:
		dialog.Message("%s", message).Title(AppName).Info()
	}

	return 0
}

func IsUpdateCancelled() bool {
	return atomic.LoadUint32(&isUpdateCancelled) != 0
}

func AppLoop() {
	gocoa.RunApplication()
}
