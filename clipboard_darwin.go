package main

/*
#cgo LDFLAGS: -framework AppKit
extern int clipChangeCount();
extern int clipRead(void** data, int* len);
extern void clipWriteText(const void* data, int len);
extern void clipWriteImage(const void* data, int len);
extern void clipFree(void* data);
*/
import "C"
import (
	"sync/atomic"
	"unsafe"
)

var lastChangeCount atomic.Int32

func clipboardRead() (*ClipboardContent, error) {
	count := C.clipChangeCount()
	if C.int(lastChangeCount.Load()) == count {
		return nil, nil
	}
	lastChangeCount.Store(int32(count))

	var data unsafe.Pointer
	var length C.int
	typ := C.clipRead(&data, &length)
	if typ == 0 {
		return nil, nil
	}
	defer C.clipFree(data)

	goData := C.GoBytes(data, length)
	return &ClipboardContent{Type: byte(typ), Data: goData}, nil
}

func clipboardWrite(content *ClipboardContent) error {
	if len(content.Data) == 0 {
		return nil
	}
	data := unsafe.Pointer(&content.Data[0])
	length := C.int(len(content.Data))

	switch content.Type {
	case 'I':
		C.clipWriteImage(data, length)
	default:
		C.clipWriteText(data, length)
	}

	lastChangeCount.Store(int32(C.clipChangeCount()))
	return nil
}
