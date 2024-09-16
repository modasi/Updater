//go:build darwin
// +build darwin

package updater

import (
	"log"
	"os"
)

var (
	isUpdateCancelled bool
	isSilentMode      bool
)

func init() {
	for _, arg := range os.Args {
		if arg == "-q" {
			isSilentMode = true
			break
		}
	}
}

func AppLoop() {

}

func ShowMainWindow() {

}

func SetUpdateComplete() {

}

func UpdateUI(cb1 func(), cb2 func(), cb3 func()) {

	cb1()
	cb2()
	cb3()
}

func SetUpdateProgress(progress float64) {
	if !isSilentMode {
		log.Printf("更新进度: %.2f%%", progress*100)
	}
}

func AppendLogText(text string) {
	if !isSilentMode {
		log.Print(text)
	}
}

func CloseWindow() {
	// 在 macOS 上不需要实现
}

func ShowUpdateErrorDialog(message string) {
	if isSilentMode {
		log.Printf("更新错误: %s", message)
	} else {
		log.Printf("更新错误对话框: %s", message)
	}
}

func ShowUpdateConfirmDialog(message string) bool {
	if isSilentMode {
		return true
	}
	log.Printf("更新确认对话框: %s", message)
	return true // 在实际应用中，您可能需要实现一个真正的对话框
}

func IsUpdateCancelled() bool {
	return isUpdateCancelled
}
