//go:build !windows

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "This is the native Windows build of Case Studio; build it with GOOS=windows.")
	os.Exit(1)
}
