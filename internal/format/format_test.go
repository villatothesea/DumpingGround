package format

import "testing"

func TestBytes(t *testing.T) {
	for _, tt := range []struct {
		value uint64
		want  string
	}{
		{0, "0 B"}, {1024, "1.0 KB"}, {1024 * 1024, "1.0 MB"},
	} {
		if got := Bytes(tt.value); got != tt.want {
			t.Fatalf("Bytes(%d) = %q, want %q", tt.value, got, tt.want)
		}
	}
}
