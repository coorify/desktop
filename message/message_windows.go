package message

import (
	"syscall"
	"unsafe"
)

func MessageBox(title, message string) bool {
	user32 := syscall.NewLazyDLL("user32.dll")
	messageBoxW := user32.NewProc("MessageBoxW")
	mbYesNo := 0x00000004
	mbIconQuestion := 0x00000020
	idYes := 6
	tptr, _ := syscall.UTF16PtrFromString(title)
	mptr, _ := syscall.UTF16PtrFromString(message)
	ret, _, _ := messageBoxW.Call(0, uintptr(unsafe.Pointer(mptr)), uintptr(unsafe.Pointer(tptr)), uintptr(uint(mbYesNo|mbIconQuestion)))
	return int(ret) == idYes
}
