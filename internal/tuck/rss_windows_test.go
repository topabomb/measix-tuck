//go:build windows

package tuck

import (
	"encoding/binary"
	"golang.org/x/sys/windows"
	"os"
	"unsafe"
)

// Junctions exercise the same containment boundary without requiring the
// developer-mode privilege that Windows symbolic links require.
func directoryLink(target, link string) error {
	if e := os.Symlink(target, link); e == nil {
		return nil
	}
	if e := os.Mkdir(link, 0700); e != nil {
		return e
	}
	name, e := windows.UTF16PtrFromString(link)
	if e != nil {
		os.Remove(link)
		return e
	}
	handle, e := windows.CreateFile(name, windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if e != nil {
		os.Remove(link)
		return e
	}
	defer windows.CloseHandle(handle)
	substitute, _ := windows.UTF16FromString(`\??\` + target)
	printName, _ := windows.UTF16FromString(target)
	names := append(substitute, printName...)
	data := make([]byte, 16+len(names)*2)
	binary.LittleEndian.PutUint32(data, 0xa0000003) // IO_REPARSE_TAG_MOUNT_POINT
	binary.LittleEndian.PutUint16(data[4:], uint16(len(data)-8))
	binary.LittleEndian.PutUint16(data[10:], uint16((len(substitute)-1)*2))
	binary.LittleEndian.PutUint16(data[12:], uint16(len(substitute)*2))
	binary.LittleEndian.PutUint16(data[14:], uint16((len(printName)-1)*2))
	for i, value := range names {
		binary.LittleEndian.PutUint16(data[16+i*2:], value)
	}
	var returned uint32
	e = windows.DeviceIoControl(handle, 0x900a4, &data[0], uint32(len(data)), nil, 0, &returned, nil) // FSCTL_SET_REPARSE_POINT
	if e != nil {
		windows.CloseHandle(handle)
		os.Remove(link)
	}
	return e
}

func processRSS() (uint64, error) {
	var counters struct {
		Size, Faults                                                                                          uint32
		PeakWorkingSet, WorkingSet, PeakPaged, Paged, PeakNonPaged, NonPaged, Pagefile, PeakPagefile, Private uintptr
	}
	counters.Size = uint32(unsafe.Sizeof(counters))
	result, _, e := windows.NewLazySystemDLL("psapi.dll").NewProc("GetProcessMemoryInfo").Call(uintptr(windows.CurrentProcess()), uintptr(unsafe.Pointer(&counters)), uintptr(counters.Size))
	if result == 0 {
		return 0, e
	}
	return uint64(counters.WorkingSet), nil
}
