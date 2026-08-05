package main

/*
#cgo LDFLAGS: -framework AppKit
extern int clipChangeCount();
extern int clipRead(void** data, int* len);
extern int clipWriteText(const void* data, int len);
extern int clipWriteImage(const void* data, int len);
extern void clipFree(void* data);
*/
import "C"
import (
	"sync/atomic"
	"unsafe"
)

var lastChangeCount atomic.Int32

// afterWriteHook runs inside clipboardWrite between putting data on the
// pasteboard and recording the resulting changeCount. Always nil in
// production; the regression test uses it to make a copy land in exactly that
// window. See TestClipboardWriteDoesNotSwallowConcurrentCopy.
var afterWriteHook func()

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

// externalPasteboardWrite puts text on the pasteboard WITHOUT touching
// lastChangeCount -- exactly what another app does when the user hits Cmd-C.
// Only the regression test calls it; it lives here because cgo is not allowed
// in _test.go files.
func externalPasteboardWrite(b []byte) {
	if len(b) == 0 {
		return
	}
	C.clipWriteText(unsafe.Pointer(&b[0]), C.int(len(b)))
}

func clipboardWrite(content *ClipboardContent) error {
	if len(content.Data) == 0 {
		return nil
	}
	data := unsafe.Pointer(&content.Data[0])
	length := C.int(len(content.Data))

	// Record the changeCount our own write produced, as reported by the write
	// itself. Re-reading clipChangeCount() here instead would consume the count
	// of any copy the user made in the meantime, and the watch loop would never
	// see that copy again (it only reacts to a CHANGED count).
	var count C.int
	switch content.Type {
	case 'I':
		count = C.clipWriteImage(data, length)
	default:
		count = C.clipWriteText(data, length)
	}

	if afterWriteHook != nil {
		afterWriteHook()
	}
	lastChangeCount.Store(int32(count))
	return nil
}
