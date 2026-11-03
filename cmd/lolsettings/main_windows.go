package main

import (
	"fmt"
	"os"

	"github.com/tk-57/lol-settings/internal/lcu"
	"github.com/tk-57/lol-settings/internal/profile"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println(version)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "--diagnose" {
		c, a, err := lcu.Connect()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		defer c.Close()
		p, err := c.Snapshot(a)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		fmt.Printf("LCU connection OK; game settings: %d; input settings: %d\n", profile.Count(p.Game), profile.Count(p.Input))
		return
	}
	app := &application{widgets: make(map[int]uintptr), completions: make(chan func(), 1)}
	app.run()
}
