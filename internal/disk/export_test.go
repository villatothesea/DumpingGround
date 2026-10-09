package disk

import (
	"bytes"
	"context"
	"encoding/csv"
	"os"
	"path/filepath"
	"testing"
)

func TestCSVExportsRealPathsAndSizes(t *testing.T) {
	root := t.TempDir()
	name := "逗号,文件.txt"
	if err := os.WriteFile(filepath.Join(root, name), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	s, err := (&Scanner{}).Scan(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err = s.WriteCSV(&b); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&b).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[2][0] != filepath.Join(root, name) || rows[2][2] != "5" {
		t.Fatalf("CSV=%v", rows)
	}
}
