//go:build !windows

package elevate

func SelfElevated() bool { return false }
func Ensure() error      { return ErrUnsupported }
func Active() bool       { return false }
func Shutdown()          {}

func OpenVolume(drive string) (uintptr, error) { return 0, ErrUnsupported }
func OpenProcessHandle(pid uint32, access uint32) (uintptr, error) {
	return 0, ErrUnsupported
}
func RegDelete(key, value string, wow32 bool) error { return ErrUnsupported }
func DeletePath(path string, recycle bool) error    { return ErrUnsupported }
func ListKids(path string) ([]Kid, error)           { return nil, ErrUnsupported }
func ListTree(path string) ([]Kid, error)           { return nil, ErrUnsupported }
func IsAccessDenied(err error) bool                 { return false }
