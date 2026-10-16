//go:build linux

package tuck

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func directoryLink(target, link string) error { return os.Symlink(target, link) }

func processRSS() (uint64, error) {
	raw, e := os.ReadFile("/proc/self/status")
	if e != nil {
		return 0, e
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			parts := strings.Fields(line)
			n, e := strconv.ParseUint(parts[1], 10, 64)
			return n * 1024, e
		}
	}
	return 0, fmt.Errorf("process RSS unavailable")
}
