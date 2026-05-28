package main

import (
	"fmt"
	"os"

	"github.com/Raa-11/kerno/internal/collector"
	"github.com/Raa-11/kerno/internal/output"
)

func main() {
	c, err := collector.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "kerno: %v\n", err)
		os.Exit(1)
	}
	defer c.Close()

	output.New(c.Stats()).Render()
}
