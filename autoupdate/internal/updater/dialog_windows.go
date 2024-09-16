//go:build windows
// +build windows

package updater

import (
	"fmt"
	"syscall"
	"unsafe"

	"github.com/JamesHovious/w32"
)

var (
	isUpdateCancelled bool
	MainWindow        *ProgressWindow
	cancelButton      w32.HWND
)

const (
	WindowWidth  = 480
	WindowHeight = 320
)

type ProgressWindow struct {
	hwnd        w32.HWND
	progressBar w32.HWND
	logTextBox  w32.HWND
}

func init() {

	w32.CoInitialize()

	var icc w32.INITCOMMONCONTROLSEX
	icc.DwSize = uint32(unsafe.Sizeof(icc))
	icc.DwICC = w32.ICC_PROGRESS_CLASS | w32.ICC_STANDARD_CLASSES
	w32.InitCommonControlsEx(&icc)
}

func NewMainWindow() (*ProgressWindow, error) {
	className := syscall.StringToUTF16Ptr("ProgressWindowClass")
	windowName := syscall.StringToUTF16Ptr(AppName)

	screenWidth := w32.GetSystemMetrics(w32.SM_CXSCREEN)
	screenHeight := w32.GetSystemMetrics(w32.SM_CYSCREEN)

	var ws uint
	if IsSilentMode {
		ws = w32.WS_CHILD | w32.WS_VSCROLL | w32.ES_MULTILINE | w32.ES_AUTOVSCROLL
	} else {
		ws = w32.WS_CHILD | w32.WS_VISIBLE | w32.WS_VSCROLL | w32.ES_MULTILINE | w32.ES_AUTOVSCROLL
	}

	x := (screenWidth - WindowWidth) / 2
	y := (screenHeight - WindowHeight) / 2

	wcx := w32.WNDCLASSEX{
		Style:     w32.CS_HREDRAW | w32.CS_VREDRAW,
		WndProc:   syscall.NewCallback(defWindowProc),
		Instance:  w32.GetModuleHandle(""),
		ClassName: className,
	}
	wcx.Size = uint32(unsafe.Sizeof(wcx))

	if atom := w32.RegisterClassEx(&wcx); atom == 0 {
		return nil, fmt.Errorf("RegisterClassEx failed: %v", w32.GetLastError())
	}

	hwnd := w32.CreateWindowEx(
		0,
		className,
		windowName,
		w32.WS_OVERLAPPED|w32.WS_CAPTION|w32.WS_SYSMENU,
		int(x), int(y), WindowWidth, WindowHeight,
		0, 0, wcx.Instance, nil)

	if hwnd == 0 {
		return nil, fmt.Errorf("CreateWindowEx failed: %v", w32.GetLastError())
	}

	progressBar := w32.CreateWindowEx(
		0,
		syscall.StringToUTF16Ptr("msctls_progress32"),
		nil,
		w32.WS_CHILD|w32.WS_VISIBLE|w32.PBS_SMOOTH,
		12, 16, 440, 24,
		hwnd, 0, wcx.Instance, nil)

	logTextBox := w32.CreateWindowEx(
		w32.WS_EX_CLIENTEDGE,
		syscall.StringToUTF16Ptr("EDIT"),
		nil,
		ws|w32.ES_READONLY|w32.ES_MULTILINE|w32.ES_AUTOVSCROLL|w32.WS_VSCROLL,
		12, 50, 440, 180,
		hwnd, 0, wcx.Instance, nil)

	w32.SendMessage(logTextBox, w32.EM_SETWORDBREAKPROC, 0, 0)

	cancelButton = w32.CreateWindowEx(
		0,
		syscall.StringToUTF16Ptr("BUTTON"),
		syscall.StringToUTF16Ptr("取消"),
		w32.WS_CHILD|w32.WS_VISIBLE|w32.BS_DEFPUSHBUTTON,
		352, 240, 100, 25,
		hwnd, w32.HMENU(w32.IDCANCEL), wcx.Instance, nil)

	return &ProgressWindow{
		hwnd:        hwnd,
		progressBar: progressBar,
		logTextBox:  logTextBox,
	}, nil
}

func defWindowProc(hwnd w32.HWND, msg uint32, wparam, lparam uintptr) uintptr {
	switch msg {
	case w32.WM_COMMAND:
		if w32.LOWORD(uint32(wparam)) == w32.IDCANCEL && w32.HIWORD(uint32(wparam)) == w32.BN_CLICKED {
			isUpdateCancelled = true
			CloseWindow()
			return 0
		}
	case w32.WM_CLOSE:
		w32.DestroyWindow(hwnd)
		return 0
	case w32.WM_DESTROY:
		w32.PostQuitMessage(0)
		return 0
	}

	return w32.DefWindowProc(hwnd, msg, wparam, lparam)
}

func AppLoop() {

	var msg w32.MSG
	for {
		if w32.GetMessage(&msg, 0, 0, 0) == 0 {
			break
		}
		w32.TranslateMessage(&msg)
		w32.DispatchMessage(&msg)
	}

}

func ShowMainWindow() {
	if MainWindow == nil {
		var err error
		MainWindow, err = NewMainWindow()
		if err != nil {
			return
		}
	}
	w32.ShowWindow(MainWindow.hwnd, w32.SW_SHOW)
}

func SetUpdateProgress(progress float64) {

	if MainWindow != nil {
		w32.SendMessage(MainWindow.progressBar, w32.PBM_SETPOS, uintptr(int(progress*100)), 0)
	}
}

func AppendLogText(text string) {

	if MainWindow != nil {
		currentText := make([]uint16, w32.SendMessage(MainWindow.logTextBox, w32.WM_GETTEXTLENGTH, 0, 0)+1)
		w32.SendMessage(MainWindow.logTextBox, w32.WM_GETTEXT, uintptr(len(currentText)), uintptr(unsafe.Pointer(&currentText[0])))
		newText := syscall.UTF16ToString(currentText) + text + "\r\n"
		w32.SendMessage(MainWindow.logTextBox, w32.WM_SETTEXT, 0, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(newText))))
		// w32.SendMessage(MainWindow.logTextBox, w32.EM_SCROLLCARET, 0, 0)
	}
}

func CloseWindow() {
	if MainWindow != nil {
		w32.SendMessage(MainWindow.hwnd, w32.WM_CLOSE, 0, 0)
	}
}

func SetUpdateComplete() {
	w32.SendMessage(cancelButton, w32.WM_SETTEXT, 0, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("完成"))))
}

func ShowUpdateErrorDialog(message string) {
	AppendLogText("更新错误: " + message)
	ShowMessageBox("更新错误", message, w32.MB_ICONERROR)
}

func ShowUpdateConfirmDialog(message string) bool {
	return ShowMessageBox("更新确认", message, w32.MB_YESNO|w32.MB_ICONQUESTION) == w32.IDYES
}

func ShowMessageBox(title, message string, uType uint) int32 {
	return int32(w32.MessageBox(MainWindow.hwnd, message, title, uType))
}

func IsUpdateCancelled() bool {
	return isUpdateCancelled
}
