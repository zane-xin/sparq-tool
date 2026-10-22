package main

import (
	"fmt"
	"os"

	"github.com/zane-xin/sparq-tool/internal/updater"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: sparq-updater <local-plan.json>")
		os.Exit(2)
	}
	plan, err := updater.ReadPlan(os.Args[1])
	if err == nil {
		_, err = updater.Execute(plan)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
