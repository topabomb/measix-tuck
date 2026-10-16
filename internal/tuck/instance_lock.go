package tuck

import "os"

// Keep the inode in place: removing a lock file lets another process lock a
// different inode while an owner still holds the old one. The OS releases locks
// on exit, including a crash; no PID, heartbeat or stale-file recovery is needed.
func acquireInstanceLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = lockInstance(f); err != nil {
		f.Close()
		return nil, Problem(409, "instance_in_use", "配置或数据正在使用，请先停止使用它的 Tuck 服务或维护命令")
	}
	return f, nil
}
