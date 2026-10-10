package disk

type Volume struct {
	Path  string
	Label string
	FS    string
	Total uint64
	Free  uint64
}
