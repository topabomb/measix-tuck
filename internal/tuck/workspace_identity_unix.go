//go:build !windows && !linux

package tuck

import (
	"fmt"
	"os"
)

func directoryIdentity(file *os.File) (string, error) {
	return "", fmt.Errorf("stable directory identity is unavailable on this platform")
}
