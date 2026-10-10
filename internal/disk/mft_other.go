//go:build !windows

package disk

import (
	"context"
	"errors"
)

func fastVolumeScan(context.Context, string, chan<- Progress) (*Snapshot, error) {
	return nil, errors.New("NTFS MFT scan requires Windows")
}

func ProbeMFT(context.Context, string) (*Snapshot, error) {
	return nil, errors.New("NTFS MFT scan requires Windows")
}
