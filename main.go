package main

import (
	"flag"
	"os"
)

func main() {
	var hostname string
	flag.StringVar(&hostname, "hostname", "nanobox", "Sets the hostname of the container")
	flag.Parse()

	if os.Getenv("NANOBOX_CHILD") == "1" {
		runChild(hostname, flag.Args())
		return
	}
	runParent(hostname, flag.Args())
}
