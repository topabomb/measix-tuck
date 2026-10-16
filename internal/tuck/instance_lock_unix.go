//go:build !windows

package tuck

import (
	"os"

	"golang.org/x/sys/unix"
)

func lockInstance(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
}
