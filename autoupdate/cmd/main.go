package main

import (
	"autoupdate/autoupdate/internal/updater"
	"flag"
	"fmt"
	"log"
	"os"
)

func main() {

	log.SetOutput(os.Stdout)
	log.Println("Auto Update Tool 启动")

	debug := flag.Bool("debug", false, "启用调试模式")
	flag.Parse()

	if *debug {
		fmt.Println("调试模式已启用")
	}

	updater := updater.NewUpdater("yourusername", "yourrepo", *debug)
	os.Exit(updater.Update())
}
