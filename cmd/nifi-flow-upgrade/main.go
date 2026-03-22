package main

import (
	"os"

	"github.com/michaelhutchings-napier/nifi-flow-upgrade-advisor/internal/flowupgrade"
)

func main() {
	os.Exit(flowupgrade.Main(os.Args[1:], os.Stdout, os.Stderr))
}
