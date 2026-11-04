package cli

import (
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

func isTerminal(f *os.File) bool {
	var state syscall.Termios
	// #nosec G103 -- ioctl requires a pointer to the platform termios structure; no arbitrary memory access.
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TCGETS, uintptr(unsafe.Pointer(&state)))
	runtime.KeepAlive(f)
	return errno == 0
}
