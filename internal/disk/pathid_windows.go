//go:build windows

package disk

import (
	"errors"
	"syscall"
	"unsafe"
)

// PathID pins a path to the volume:file-index identity of every
// ancestor up to the drive root, plus the leaf's link-ness. Deletion
// helpers bind a path when it is first measured and verify the binding
// before removing anything: a junction swapped into the chain between
// scan and delete rewrites an ancestor's identity and the check
// refuses to run on the impostor. The leaf itself may be rebuilt — a
// cache dir recreated between scan and clean is still the same cache
// dir — but it must never turn into a link.
type PathID struct {
	path      string
	ents      []pathEnt // ancestors only, root first, leaf excluded
	allowLeaf bool      // the leaf itself may be a reparse point
}

type pathEnt struct {
	vol, idxHi, idxLo uint32
}

var procGetFileInfoByHandle = syscall.NewLazyDLL("kernel32.dll").NewProc("GetFileInformationByHandle")

type byHandleInfo struct {
	attrs               uint32
	ctime, atime, wtime syscall.Filetime
	vol, sizeHi, sizeLo uint32
	links, idxHi, idxLo uint32
}

const (
	pathIDReadAttrs   = 0x80       // FILE_READ_ATTRIBUTES
	pathIDShareAll    = 0x7        // FILE_SHARE_READ|WRITE|DELETE
	pathIDOpenExist   = 3          // OPEN_EXISTING
	pathIDFlags       = 0x02200000 // FILE_FLAG_BACKUP_SEMANTICS | FILE_FLAG_OPEN_REPARSE_POINT
	pathIDAttrReparse = 0x400      // FILE_ATTRIBUTE_REPARSE_POINT
)

// pathIdentity opens one path component — without following a link —
// and returns its volume:file-index plus whether the object itself is
// a reparse point.
func pathIdentity(p string) (ent pathEnt, reparse bool, err error) {
	p16, err := syscall.UTF16PtrFromString(p)
	if err != nil {
		return pathEnt{}, false, err
	}
	h, err := syscall.CreateFile(p16, pathIDReadAttrs, pathIDShareAll, nil,
		pathIDOpenExist, pathIDFlags, 0)
	if err != nil {
		return pathEnt{}, false, err
	}
	defer syscall.CloseHandle(h)
	var info byHandleInfo
	r, _, e := procGetFileInfoByHandle.Call(uintptr(h), uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		return pathEnt{}, false, e
	}
	return pathEnt{vol: info.vol, idxHi: info.idxHi, idxLo: info.idxLo},
		info.attrs&pathIDAttrReparse != 0, nil
}

// BindPath captures the ancestor-chain identity of p. Every ancestor
// must open and none may be a reparse point; the leaf must exist, and
// may be a link only when allowLeafLink says so (a link is removed as
// a link, never followed). A path that cannot be pinned fails the
// bind — callers must treat an error as "do not touch".
func BindPath(p string, allowLeafLink bool) (*PathID, error) {
	_, reparse, err := pathIdentity(p)
	if err != nil {
		return nil, err
	}
	if reparse && !allowLeafLink {
		return nil, errReparse
	}
	id := &PathID{path: p, allowLeaf: allowLeafLink}
	cur := parentPath(p)
	for cur != "" {
		ent, reparse, err := pathIdentity(cur)
		if err != nil {
			return nil, err
		}
		if reparse {
			return nil, errReparse
		}
		id.ents = append([]pathEnt{ent}, id.ents...)
		parent := parentPath(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return id, nil
}

// parentPath is filepath.Dir without the allocs — "C:\" has no
// parent, everything else loses its last segment.
func parentPath(p string) string {
	for len(p) > 0 && (p[len(p)-1] == '\\' || p[len(p)-1] == '/') {
		p = p[:len(p)-1]
	}
	vol := 0
	if len(p) >= 2 && p[1] == ':' {
		vol = 2
	}
	i := len(p) - 1
	for i >= vol && p[i] != '\\' && p[i] != '/' {
		i--
	}
	if i < vol {
		if vol > 0 {
			return p[:vol] + `\` // drive root
		}
		return p
	}
	parent := p[:i]
	for len(parent) > vol && (parent[len(parent)-1] == '\\' || parent[len(parent)-1] == '/') {
		parent = parent[:len(parent)-1]
	}
	if len(parent) <= vol && vol > 0 {
		return p[:vol] + `\`
	}
	return parent
}

// Verify re-reads the chain. Every ancestor must still match identity-
// for-identity and stay reparse-free; a shallower or deeper chain is a
// mismatch too. The leaf may be gone — the deletion sink re-checks
// existence and kind — but if it exists it must still satisfy the
// link rule the binding set.
func (id *PathID) Verify() bool {
	if id == nil {
		return false
	}
	if _, reparse, err := pathIdentity(id.path); err == nil {
		if reparse && !id.allowLeaf {
			return false
		}
	} else if !errors.Is(err, syscall.ERROR_FILE_NOT_FOUND) &&
		!errors.Is(err, syscall.ERROR_PATH_NOT_FOUND) {
		return false // unreadable leaf — cannot prove it is still a file
	}
	cur := parentPath(id.path)
	for i := len(id.ents) - 1; i >= 0; i-- {
		ent, reparse, err := pathIdentity(cur)
		if err != nil || reparse || ent != id.ents[i] {
			return false
		}
		parent := parentPath(cur)
		if parent == cur {
			return i == 0 // the drive root must be the last captured entry
		}
		cur = parent
	}
	return len(id.ents) == 0
}
