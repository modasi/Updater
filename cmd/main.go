package main

import (
	"flag"
	"os"
	"runtime"

	"autoupdate/internal/updater"
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

	worker := updater.NewUpdater(appName, debug, silent)

	var result int

	go func(result *int) {
		*result = worker.Update()
	}(&result)

	updater.AppLoop()

	os.Exit(result)

}
