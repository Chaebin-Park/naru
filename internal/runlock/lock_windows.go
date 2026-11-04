//go:build windows

package runlock

import (
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

var lockFileEx = syscall.NewLazyDLL("kernel32.dll").NewProc("LockFileEx")

func lockFile(f *os.File) error {
	var overlapped syscall.Overlapped
	// Lock one byte beyond EOF; FAIL_IMMEDIATELY | EXCLUSIVE_LOCK.
	ok, _, err := lockFileEx.Call(f.Fd(), 3, 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
	runtime.KeepAlive(&overlapped)
	if ok != 0 {
		return nil
	}
	if err == syscall.Errno(33) {
		return ErrBusy
	} // ERROR_LOCK_VIOLATION
	return err
}
