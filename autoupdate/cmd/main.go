package main

import (
	. "autoupdate/autoupdate/internal/updater"
	"flag"
	"os"
	"runtime"
)

var (
	appName string
	debug   bool
	silent  bool
)

func init() {
	flag.BoolVar(&debug, "debug", false, "调试模式")
	flag.BoolVar(&silent, "silent", false, "静默模式")
	flag.StringVar(&appName, "app", "", "应用名称")
	flag.Parse()

	if appName == "" {
		appName = "Updater"
	}
}

func main() {

	runtime.LockOSThread()

	worker := NewUpdater(appName, debug, silent)

	var result int

	go func(result *int) {
		*result = worker.Update()
	}(&result)

	AppLoop()

	os.Exit(result)

}
