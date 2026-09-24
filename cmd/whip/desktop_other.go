//go:build !darwin && !linux

package main

import (
	"fmt"
	"os"
)

func desktopSSHCLI([]string) int {
	fmt.Fprintln(os.Stderr, "whip desktop: SSH supervision is unavailable on this platform")
	return 1
}

func desktopAskpassCLI([]string) int { return 1 }
