package main

import (
	"log"
	"os"
)

func main() {

	if len(os.Args) < 2 {
		log.Fatalln("Usage: nanobox run [-hostname <name>] <command>")
	}

	switch os.Args[1] {
	case "run":
		runParent()
	case "child":
		runChild()
	default:
		log.Fatalln("Unknown command")
	}
}
