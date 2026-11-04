package main

import (
	"os"

	"github.com/davidalecrim1/amx/internal/cli"
)

func main() {
	os.Exit(cli.New().Run(os.Args[1:]))
}
