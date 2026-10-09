package disk

type Volume struct {
	Path  string
	Label string
	Total uint64
	Free  uint64
}
