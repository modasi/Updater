//go:build windows
// +build windows

package updater

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	user32   = syscall.NewLazyDLL("user32.dll")
	ole32    = syscall.NewLazyDLL("ole32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")

	procCoInitializeEx  = ole32.NewProc("CoInitializeEx")
	procCreateWindowExW = user32.NewProc("CreateWindowExW")
	procShowWindow      = user32.NewProc("ShowWindow")
	procDestroyWindow   = user32.NewProc("DestroyWindow")
	procSendMessageW    = user32.NewProc("SendMessageW")
	procMessageBoxW     = user32.NewProc("MessageBoxW")
	procPostQuitMessage = user32.NewProc("PostQuitMessage")
	procGetLastError    = kernel32.NewProc("GetLastError")

	isUpdateCancelled     bool
	currentProgressWindow *ProgressWindow

	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
	procGetMessageW      = user32.NewProc("GetMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")

	comctl32                 = syscall.NewLazyDLL("comctl32.dll")
	procInitCommonControlsEx = comctl32.NewProc("InitCommonControlsEx")
	procSetClassLongPtrW     = user32.NewProc("SetClassLongPtrW")
	procGetSysColorBrush     = user32.NewProc("GetSysColorBrush")
	procCreateSolidBrush     = gdi32.NewProc("CreateSolidBrush")
	procGetStockObject       = gdi32.NewProc("GetStockObject")
	procSetBkColor           = gdi32.NewProc("SetBkColor")
)

type INITCOMMONCONTROLSEX struct {
	dwSize uint32
	dwICC  uint32
}

type POINT struct {
	X, Y int32
}

type MSG struct {
	Hwnd    syscall.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      POINT
}

const (
	WS_OVERLAPPEDWINDOW  = 0x00CF0000
	WS_CHILD             = 0x40000000
	WS_VISIBLE           = 0x10000000
	WS_OVERLAPPED        = 0x00000000
	WS_CAPTION           = 0x00C00000
	WS_SYSMENU           = 0x00080000
	SW_SHOW              = 5
	SW_HIDE              = 0
	SM_CXSCREEN          = 0
	SM_CYSCREEN          = 1
	PBM_SETPOS           = 0x0402
	MB_ICONERROR         = 0x00000010
	MB_YESNO             = 0x00000004
	MB_ICONQUESTION      = 0x00000020
	IDYES                = 6
	COINIT_MULTITHREADED = 0x0

	WM_CLOSE       = 0x0010
	WM_QUIT        = 0x0012
	WM_DESTROY     = 0x0002
	WM_SETFONT     = 0x0030
	WM_CTLCOLORBTN = 0x0013

	WM_COMMAND = 0x0111
	BN_CLICKED = 0
	IDCANCEL   = 2

	ICC_STANDARD_CLASSES = 0x00004000
	COLOR_WINDOW         = 5
	COLOR_WHITE          = 0xFFFFFF
	COLOR_BTNFACE        = 15
	GWL_STYLE            = -16
	GWL_EXSTYLE          = -20
	WS_EX_COMPOSITED     = 0x02000000
	BS_PUSHBUTTON        = 0x00000000
	BS_DEFPUSHBUTTON     = 0x00000001
	BS_FLAT              = 0x00000800
	DEFAULT_GUI_FONT     = 17
)

var (
	GCLP_HBRBACKGROUND = -10
)

func init() {
	coInitializeEx(nil, COINIT_MULTITHREADED)
	// 初始化 Common Controls
	var icc INITCOMMONCONTROLSEX
	icc.dwSize = uint32(unsafe.Sizeof(icc))
	icc.dwICC = ICC_STANDARD_CLASSES
	procInitCommonControlsEx.Call(uintptr(unsafe.Pointer(&icc)))
}

type ProgressWindow struct {
	hwnd         syscall.Handle
	progressBar  syscall.Handle
	cancelButton syscall.Handle
}

type WNDCLASSEXW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     syscall.Handle
	HIcon         syscall.Handle
	HCursor       syscall.Handle
	HbrBackground syscall.Handle
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       syscall.Handle
}

func NewProgressWindow() (*ProgressWindow, error) {
	className := syscall.StringToUTF16Ptr("ProgressWindowClass")
	windowName := syscall.StringToUTF16Ptr("更新进度")
	// 获取屏幕尺寸
	screenWidth, _, _ := procGetSystemMetrics.Call(uintptr(SM_CXSCREEN))
	screenHeight, _, _ := procGetSystemMetrics.Call(uintptr(SM_CYSCREEN))

	// 设置窗口尺寸
	windowWidth := uintptr(480)
	windowHeight := uintptr(240)

	// 计算窗口位置
	x := (screenWidth - windowWidth) / 2
	y := (screenHeight - windowHeight) / 2

	// 注册窗口类
	wcx := WNDCLASSEXW{
		CbSize:        uint32(unsafe.Sizeof(WNDCLASSEXW{})),
		LpfnWndProc:   syscall.NewCallback(defWindowProc),
		LpszClassName: className,
	}

	atom, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wcx)))

	if atom == 0 {
		return nil, fmt.Errorf("RegisterClassEx failed: %v", err)
	}

	hwnd, _, _ := procCreateWindowExW.Call(
		WS_EX_COMPOSITED,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowName)),
		WS_OVERLAPPED|WS_CAPTION|WS_SYSMENU,
		x, y, 480, 240,
		0, 0, 0, 0,
	)

	// 设置窗口背景色
	hbrWhite, _, _ := procCreateSolidBrush.Call(uintptr(COLOR_WHITE)) // 创建白色画刷
	procSetClassLongPtrW.Call(
		uintptr(hwnd),
		uintptr(GCLP_HBRBACKGROUND),
		hbrWhite,
	)

	progressBar, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("msctls_progress32"))),
		0,
		WS_CHILD|WS_VISIBLE,
		10, 20, 440, 24,
		hwnd, 0, 0, 0,
	)

	// 创建取消按钮
	cancelButton, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("BUTTON"))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("取消"))),
		WS_CHILD|WS_VISIBLE|BS_DEFPUSHBUTTON,
		345, 150, 100, 25, // 调整位置和大小
		hwnd,
		IDCANCEL, // 按钮 ID
		0, 0,
	)

	return &ProgressWindow{
		hwnd:         syscall.Handle(hwnd),
		progressBar:  syscall.Handle(progressBar),
		cancelButton: syscall.Handle(cancelButton),
	}, nil
}

func LOWORD(w uintptr) uint16 {
	return uint16(w & 0xFFFF)
}

func HIWORD(w uintptr) uint16 {
	return uint16((w >> 16) & 0xFFFF)
}

func defWindowProc(hwnd syscall.Handle, msg uint32, wparam, lparam uintptr) uintptr {
	switch msg {
	case WM_COMMAND:
		if LOWORD(wparam) == IDCANCEL && HIWORD(wparam) == BN_CLICKED {
			// 用户点击了取消按钮
			SetUpdateCancelled(true)
			procPostQuitMessage.Call(0)
			return 0
		}
	case WM_CLOSE:
		procDestroyWindow.Call(uintptr(hwnd))
		return 0
	case WM_DESTROY:
		procPostQuitMessage.Call(0)
		return 0
	}

	ret, _, _ := procDefWindowProcW.Call(
		uintptr(hwnd),
		uintptr(msg),
		wparam,
		lparam,
	)

	return ret

}

func (pw *ProgressWindow) SetProgress(progress float64) {
	procSendMessageW.Call(
		uintptr(pw.progressBar),
		PBM_SETPOS,
		uintptr(int(progress*100)),
		0,
	)
}

func (pw *ProgressWindow) Show() {
	procShowWindow.Call(uintptr(pw.hwnd), SW_SHOW)
}

func (pw *ProgressWindow) Hide() {
	procShowWindow.Call(uintptr(pw.hwnd), SW_HIDE)
}

func (pw *ProgressWindow) Close() {
	procSendMessageW.Call(uintptr(pw.hwnd), WM_CLOSE, 0, 0)
}

func ShowWindow(progressChan <-chan float64, cb1 func(), cb2 func(), cb3 func()) {
	if currentProgressWindow == nil {
		var err error
		currentProgressWindow, err = NewProgressWindow()
		if err != nil {
			// 处理错误，可能需要记录日志
			return
		}
	}
	currentProgressWindow.Show()

	cb1()
	cb2()
	cb3()

	var msg MSG
	for {

		ret, _, _ := procGetMessageW.Call(
			uintptr(unsafe.Pointer(&msg)),
			0,
			0,
			0,
		)

		if ret == 0 {
			break
		}

		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))

	}
}

func SetUpdateProgress(progress float64) {
	if currentProgressWindow != nil {
		currentProgressWindow.SetProgress(progress)
	}
}

func CloseWindow() {
	if currentProgressWindow != nil {
		currentProgressWindow.Close()
	}
}

func ShowUpdateErrorDialog(message string) {
	ShowMessageBox("更新错误", message, MB_ICONERROR)
}

func ShowUpdateConfirmDialog(message string) bool {
	return ShowMessageBox("更新确认", message, MB_YESNO|MB_ICONQUESTION) == IDYES
}

func ShowMessageBox(title, message string, uType uint32) int32 {
	var hwnd uintptr
	if currentProgressWindow != nil {
		hwnd = uintptr(currentProgressWindow.hwnd)
	}
	ret, _, _ := procMessageBoxW.Call(
		uintptr(hwnd),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(message))),
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(title))),
		uintptr(uType),
	)
	return int32(ret)
}

func IsUpdateCancelled() bool {
	return isUpdateCancelled
}

func SetUpdateCancelled(cancelled bool) {
	isUpdateCancelled = cancelled
}

func coInitializeEx(pvReserved unsafe.Pointer, dwCoInit uint32) error {
	ret, _, _ := procCoInitializeEx.Call(uintptr(pvReserved), uintptr(dwCoInit))
	if ret != 0 {
		return syscall.GetLastError()
	}
	return nil
}
