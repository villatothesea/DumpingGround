//go:build !windows

package app

import "github.com/egoist/mygo/ui"

func systemIcon(string) *ui.Bitmap { return nil }
