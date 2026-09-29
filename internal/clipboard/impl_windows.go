//go:build windows

package clipboard

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"image/png"
	"log"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	cfDIB             = 8
	cfUnicodeText     = 13
	cfDIBV5           = 17
	gmemMoveable      = 0x0002
	maxClipboardBlock = 256 * 1024 * 1024
)

var (
	user32                = syscall.NewLazyDLL("user32.dll")
	kernel32              = syscall.NewLazyDLL("kernel32.dll")
	procOpenClipboard     = user32.NewProc("OpenClipboard")
	procCloseClipboard    = user32.NewProc("CloseClipboard")
	procEmptyClipboard    = user32.NewProc("EmptyClipboard")
	procSetClipboardData  = user32.NewProc("SetClipboardData")
	procGetClipboardData  = user32.NewProc("GetClipboardData")
	procIsClipboardFormat = user32.NewProc("IsClipboardFormatAvailable")
	procRegisterFormat    = user32.NewProc("RegisterClipboardFormatW")
	procClipboardSequence = user32.NewProc("GetClipboardSequenceNumber")
	procCreateWindowEx    = user32.NewProc("CreateWindowExW")
	procGlobalAlloc       = kernel32.NewProc("GlobalAlloc")
	procGlobalLock        = kernel32.NewProc("GlobalLock")
	procGlobalUnlock      = kernel32.NewProc("GlobalUnlock")
	procGlobalSize        = kernel32.NewProc("GlobalSize")
	procGlobalFree        = kernel32.NewProc("GlobalFree")
	procGetModuleHandle   = kernel32.NewProc("GetModuleHandleW")
)

var (
	pngFormatOnce  sync.Once
	pngFormatID    uintptr
	ownerOnce      sync.Once
	ownerWindow    uintptr
	ownerWindowErr error
)

type systemClipboard struct {
	mu      sync.Mutex
	seenSeq uint32
}

func New() Clipboard { return &systemClipboard{seenSeq: clipboardSequence()} }

// WriteImageAndText places PNG, DIB, DIBV5, and Unicode text representations
// on the Windows clipboard in one ownership transfer. Terminal paste clients
// request CF_UNICODETEXT; image applications can request the PNG or a DIB.
func (s *systemClipboard) WriteImageAndText(imageData []byte, text string) {
	if len(imageData) == 0 {
		log.Printf("clipboard: refusing to write an empty image")
		return
	}

	unicode := utf16ClipboardText(text)
	textBytes := unsafe.Slice((*byte)(unsafe.Pointer(&unicode[0])), len(unicode)*2)
	formats := make([]clipboardPayload, 0, 4)
	if format := pngClipboardFormat(); format != 0 {
		formats = append(formats, clipboardPayload{format: format, data: imageData})
	}
	if dib, err := encodeDIB(imageData); err == nil {
		formats = append(formats, clipboardPayload{format: cfDIB, data: dib})
	} else {
		log.Printf("clipboard: could not prepare DIB fallback: %v", err)
	}
	if dib, err := encodeDIBV5(imageData); err == nil {
		formats = append(formats, clipboardPayload{format: cfDIBV5, data: dib})
	} else {
		log.Printf("clipboard: could not prepare DIBV5 fallback: %v", err)
	}
	formats = append(formats, clipboardPayload{format: cfUnicodeText, data: textBytes})

	for i := range formats {
		h, err := allocClipboardBlock(formats[i].data)
		if err != nil {
			freeClipboardBlocks(formats)
			log.Printf("clipboard: could not allocate clipboard data: %v", err)
			return
		}
		formats[i].handle = h
	}

	owner, err := clipboardOwnerWindow()
	if err != nil {
		freeClipboardBlocks(formats)
		log.Printf("clipboard: could not create owner window: %v", err)
		return
	}
	if err := openClipboard(owner); err != nil {
		freeClipboardBlocks(formats)
		log.Printf("clipboard: open for write failed: %v", err)
		return
	}
	if ok, err := callBool(procEmptyClipboard, "EmptyClipboard"); !ok {
		_ = closeClipboard()
		freeClipboardBlocks(formats)
		log.Printf("clipboard: clear failed: %v", err)
		return
	}
	for i := range formats {
		result, _, callErr := procSetClipboardData.Call(formats[i].format, formats[i].handle)
		if result == 0 {
			log.Printf("clipboard: set format %d failed: %v", formats[i].format, win32Error("SetClipboardData", callErr))
			continue
		}
		// Windows owns the HGLOBAL after a successful SetClipboardData call.
		formats[i].handle = 0
	}
	if err := closeClipboard(); err != nil {
		log.Printf("clipboard: close after write failed: %v", err)
	}
	s.markSeen(clipboardSequence())
	freeClipboardBlocks(formats)
}

type clipboardPayload struct {
	format uintptr
	data   []byte
	handle uintptr
}

func freeClipboardBlocks(formats []clipboardPayload) {
	for _, payload := range formats {
		if payload.handle != 0 {
			procGlobalFree.Call(payload.handle)
		}
	}
}

func (s *systemClipboard) WatchForImage(interval time.Duration) <-chan []byte {
	if interval <= 0 {
		interval = 300 * time.Millisecond
	}
	ch := make(chan []byte, 1)
	go func() {
		log.Printf("clipboard watching for images...")
		var lastImageHash [32]byte
		var lastImageAt time.Time
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			if clipboardSequence() == s.lastSeen() {
				continue
			}
			data, seq, opened, err := readClipboardImage()
			if err != nil {
				log.Printf("clipboard: read failed: %v", err)
			}
			if !opened {
				continue
			}
			s.markSeen(seq)
			if len(data) == 0 {
				continue
			}
			now := time.Now()
			hash := sha256.Sum256(data)
			if hash == lastImageHash && now.Sub(lastImageAt) < 2*time.Second {
				log.Printf("clipboard: ignoring duplicate image event")
				continue
			}
			lastImageHash = hash
			lastImageAt = now
			log.Printf("clipboard: image detected (%d KB)", len(data)/1024)
			ch <- data
		}
	}()
	return ch
}

func readClipboardImage() ([]byte, uint32, bool, error) {
	if err := openClipboard(0); err != nil {
		return nil, 0, false, err
	}
	defer closeClipboard()
	seq := clipboardSequence()
	for _, format := range []uintptr{pngClipboardFormat(), cfDIBV5, cfDIB} {
		if format == 0 || !clipboardFormatAvailable(format) {
			continue
		}
		block, err := readClipboardBlock(format)
		if err != nil {
			continue
		}
		if format == pngClipboardFormat() {
			if len(block) >= 8 && string(block[:8]) == "\x89PNG\r\n\x1a\n" {
				return block, seq, true, nil
			}
			continue
		}
		img, err := decodeDIB(block)
		if err != nil {
			continue
		}
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, img); err != nil {
			return nil, seq, true, err
		}
		return encoded.Bytes(), seq, true, nil
	}
	return nil, seq, true, nil
}

func openClipboard(owner uintptr) error {
	deadline := time.Now().Add(500 * time.Millisecond)
	for {
		result, _, callErr := procOpenClipboard.Call(owner)
		if result != 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return win32Error("OpenClipboard", callErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func clipboardOwnerWindow() (uintptr, error) {
	ownerOnce.Do(func() {
		className, err := syscall.UTF16PtrFromString("STATIC")
		if err != nil {
			ownerWindowErr = err
			return
		}
		windowName, err := syscall.UTF16PtrFromString("img2ssh clipboard owner")
		if err != nil {
			ownerWindowErr = err
			return
		}
		module, _, callErr := procGetModuleHandle.Call(0)
		if module == 0 {
			ownerWindowErr = win32Error("GetModuleHandleW", callErr)
			return
		}
		// Use an invisible window owned by img2ssh. Passing NULL to
		// OpenClipboard is valid for reads, but EmptyClipboard followed by
		// SetClipboardData fails unless the caller supplies a real HWND.
		ownerWindow, _, callErr = procCreateWindowEx.Call(
			0,
			uintptr(unsafe.Pointer(className)),
			uintptr(unsafe.Pointer(windowName)),
			0,
			0, 0, 1, 1,
			0, 0,
			module,
			0,
		)
		if ownerWindow == 0 {
			ownerWindowErr = win32Error("CreateWindowExW", callErr)
		}
	})
	if ownerWindowErr != nil {
		return 0, ownerWindowErr
	}
	return ownerWindow, nil
}

func closeClipboard() error {
	if ok, err := callBool(procCloseClipboard, "CloseClipboard"); !ok {
		return err
	}
	return nil
}

func clipboardFormatAvailable(format uintptr) bool {
	result, _, _ := procIsClipboardFormat.Call(format)
	return result != 0
}

func readClipboardBlock(format uintptr) ([]byte, error) {
	handle, _, callErr := procGetClipboardData.Call(format)
	if handle == 0 {
		return nil, win32Error("GetClipboardData", callErr)
	}
	size, _, callErr := procGlobalSize.Call(handle)
	if size == 0 || size > uintptr(maxClipboardBlock) || size > uintptr(^uint(0)>>1) {
		return nil, win32Error("GlobalSize", callErr)
	}
	ptr, _, callErr := procGlobalLock.Call(handle)
	if ptr == 0 {
		return nil, win32Error("GlobalLock", callErr)
	}
	defer procGlobalUnlock.Call(handle)
	return append([]byte(nil), unsafe.Slice((*byte)(unsafe.Pointer(ptr)), int(size))...), nil
}

func allocClipboardBlock(data []byte) (uintptr, error) {
	if len(data) == 0 {
		return 0, errors.New("cannot allocate empty clipboard block")
	}
	handle, _, callErr := procGlobalAlloc.Call(gmemMoveable, uintptr(len(data)))
	if handle == 0 {
		return 0, win32Error("GlobalAlloc", callErr)
	}
	ptr, _, callErr := procGlobalLock.Call(handle)
	if ptr == 0 {
		procGlobalFree.Call(handle)
		return 0, win32Error("GlobalLock", callErr)
	}
	copy(unsafe.Slice((*byte)(unsafe.Pointer(ptr)), len(data)), data)
	procGlobalUnlock.Call(handle)
	return handle, nil
}

func pngClipboardFormat() uintptr {
	pngFormatOnce.Do(func() {
		name, err := syscall.UTF16PtrFromString("PNG")
		if err != nil {
			return
		}
		pngFormatID, _, _ = procRegisterFormat.Call(uintptr(unsafe.Pointer(name)))
	})
	return pngFormatID
}

func clipboardSequence() uint32 {
	seq, _, _ := procClipboardSequence.Call()
	return uint32(seq)
}

func (s *systemClipboard) markSeen(seq uint32) {
	s.mu.Lock()
	s.seenSeq = seq
	s.mu.Unlock()
}

func (s *systemClipboard) lastSeen() uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seenSeq
}

func callBool(proc *syscall.LazyProc, name string) (bool, error) {
	result, _, callErr := proc.Call()
	if result == 0 {
		return false, win32Error(name, callErr)
	}
	return true, nil
}

func win32Error(name string, callErr error) error {
	if errno, ok := callErr.(syscall.Errno); !ok || errno == 0 {
		callErr = syscall.EINVAL
	}
	return fmt.Errorf("%s: %w", name, callErr)
}
