//go:build windows

package app

import (
	"image"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/egoist/mygo/ui"
)

type shFileInfo struct {
	Icon        uintptr
	IconIndex   int32
	Attributes  uint32
	DisplayName [260]uint16
	TypeName    [80]uint16
}
type dibHeader struct {
	Size                   uint32
	Width, Height          int32
	Planes, BitCount       uint16
	Compression, SizeImage uint32
	XPels, YPels           int32
	ClrUsed, ClrImportant  uint32
}

var (
	shell32Icon        = syscall.NewLazyDLL("shell32.dll")
	user32Icon         = syscall.NewLazyDLL("user32.dll")
	gdi32Icon          = syscall.NewLazyDLL("gdi32.dll")
	ole32Icon          = syscall.NewLazyDLL("ole32.dll")
	shGetFileInfo      = shell32Icon.NewProc("SHGetFileInfoW")
	destroyIcon        = user32Icon.NewProc("DestroyIcon")
	loadIcon           = user32Icon.NewProc("LoadIconW")
	drawIcon           = user32Icon.NewProc("DrawIconEx")
	createDC           = gdi32Icon.NewProc("CreateCompatibleDC")
	deleteDC           = gdi32Icon.NewProc("DeleteDC")
	createDIB          = gdi32Icon.NewProc("CreateDIBSection")
	selectObject       = gdi32Icon.NewProc("SelectObject")
	deleteObject       = gdi32Icon.NewProc("DeleteObject")
	gdiFlush           = gdi32Icon.NewProc("GdiFlush")
	coInitializeIcon   = ole32Icon.NewProc("CoInitializeEx")
	coUninitializeIcon = ole32Icon.NewProc("CoUninitialize")
)

// systemIcon uses Windows' actual executable icon, falling back to the stock
// application icon. Call on the icon worker, not while constructing UI frames.
func systemIcon(path string) *ui.Bitmap {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hr, _, _ := coInitializeIcon.Call(0, 2)
	if hr == 0 || hr == 1 {
		defer coUninitializeIcon.Call()
	}
	var icon uintptr
	if path != "" {
		if p, err := syscall.UTF16PtrFromString(path); err == nil {
			var info shFileInfo
			ret, _, _ := shGetFileInfo.Call(uintptr(unsafe.Pointer(p)), 0, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info), 0x101)
			if ret != 0 && info.Icon != 0 {
				icon = info.Icon
				defer destroyIcon.Call(icon)
			}
		}
	}
	if icon == 0 {
		icon, _, _ = loadIcon.Call(0, 32512)
	} // shared IDI_APPLICATION; don't destroy
	if icon == 0 {
		return nil
	}
	img, err := rasterIcon(icon, 24)
	if err != nil {
		return nil
	}
	return ui.NewBitmap(img)
}

// Render on black and white backgrounds to recover alpha correctly for both
// modern alpha icons and legacy AND/XOR-mask icons. Never mistake hbmMask for
// hbmColor or force transparent black pixels opaque.
func rasterIcon(icon uintptr, size int) (*image.RGBA, error) {
	dc, _, err := createDC.Call(0)
	if dc == 0 {
		return nil, err
	}
	defer deleteDC.Call(dc)
	header := dibHeader{Size: uint32(unsafe.Sizeof(dibHeader{})), Width: int32(size), Height: -int32(size), Planes: 1, BitCount: 32}
	var pixels unsafe.Pointer
	bitmap, _, err := createDIB.Call(dc, uintptr(unsafe.Pointer(&header)), 0, uintptr(unsafe.Pointer(&pixels)), 0, 0)
	if bitmap == 0 {
		return nil, err
	}
	defer deleteObject.Call(bitmap)
	previous, _, _ := selectObject.Call(dc, bitmap)
	defer selectObject.Call(dc, previous)
	bytes := unsafe.Slice((*byte)(pixels), size*size*4)
	render := func(background byte) ([]byte, error) {
		for i := range bytes {
			bytes[i] = background
		}
		ok, _, e := drawIcon.Call(dc, 0, 0, icon, uintptr(size), uintptr(size), 0, 0, 3)
		if ok == 0 {
			return nil, e
		}
		gdiFlush.Call()
		return append([]byte(nil), bytes...), nil
	}
	black, e := render(0)
	if e != nil {
		return nil, e
	}
	white, e := render(255)
	if e != nil {
		return nil, e
	}
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for i := 0; i < len(black); i += 4 {
		delta := max(int(white[i])-int(black[i]), int(white[i+1])-int(black[i+1]), int(white[i+2])-int(black[i+2]))
		alpha := byte(255 - min(max(delta, 0), 255))
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = min(black[i+2], alpha), min(black[i+1], alpha), min(black[i], alpha), alpha
	}
	return img, nil
}
