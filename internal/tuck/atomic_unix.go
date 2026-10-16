//go:build !windows

package tuck

import (
	"golang.org/x/sys/unix"
	"os"
)

func replaceFile(source, target string) error { return os.Rename(source, target) }
func syncDirectory(path string) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}

func availableBytes(path string) (uint64, error) {
	var stat unix.Statfs_t
	err := unix.Statfs(path, &stat)
	return uint64(stat.Bavail) * uint64(stat.Bsize), err
}
