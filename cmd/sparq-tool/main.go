package main

import (
	"os"

	"github.com/zane-xin/sparq-tool/cmd/internal/root"
)

func main() {
	if err := rootcmd.New().Execute(); err != nil {
		os.Exit(1)
	}
}
