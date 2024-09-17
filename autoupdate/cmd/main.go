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
	flag.BoolVar(&debug, "debug", false, "Debug mode")
	flag.BoolVar(&silent, "silent", false, "Silent mode")
	flag.StringVar(&appName, "app", "", "Application name")
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
