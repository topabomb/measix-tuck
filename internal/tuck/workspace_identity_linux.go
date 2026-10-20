package tuck

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func directoryIdentity(file *os.File) (string, error) {
	var stat unix.Statx_t
	if err := unix.Statx(int(file.Fd()), "", unix.AT_EMPTY_PATH|unix.AT_SYMLINK_NOFOLLOW, unix.STATX_INO|unix.STATX_BTIME, &stat); err != nil {
		return "", err
	}
	if stat.Mask&unix.STATX_BTIME == 0 {
		return "", fmt.Errorf("filesystem does not expose a stable directory birth time")
	}
	return fmt.Sprintf("%x:%x:%x:%x:%x", stat.Dev_major, stat.Dev_minor, stat.Ino, stat.Btime.Sec, stat.Btime.Nsec), nil
}
