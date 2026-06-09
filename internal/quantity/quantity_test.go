package quantity

import "testing"

func TestParseBytes(t *testing.T) {
	tests := []struct {
		in      string
		want    uint64
		wantErr bool
	}{
		{"1Ki", 1024, false},
		{"4Mi", 4 << 20, false},
		{"10Gi", 10 << 30, false},
		{"2Ti", 2 << 40, false},
		{"1Pi", 1 << 50, false},
		{"1K", 1000, false},
		{"5M", 5_000_000, false},
		{"3G", 3_000_000_000, false},
		{"500", 500, false},
		{"0", 0, false},
		{"0Gi", 0, false},
		{"  8Gi  ", 8 << 30, false},
		{"", 0, true},
		{"-1Gi", 0, true},
		{"1.5Gi", 0, true},
		{"Gi", 0, true},
		{"abc", 0, true},
		{"10GB", 0, true},
		{"10gib", 0, true},
	}
	for _, tt := range tests {
		got, err := ParseBytes(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseBytes(%q) = %d, want error", tt.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseBytes(%q) unexpected error: %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseBytes(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		in   uint64
		want string
	}{
		{0, "0"},
		{1024, "1Ki"},
		{10 << 30, "10Gi"},
		{2 << 40, "2Ti"},
		{1 << 50, "1Pi"},
		{1500, "1500"}, // not a binary multiple → bare bytes
		{(1 << 30) + 1, "1073741825"},
	}
	for _, tt := range tests {
		if got := FormatBytes(tt.in); got != tt.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFormatParseRoundTrip(t *testing.T) {
	for _, n := range []uint64{0, 1, 1023, 1024, 4 << 20, 10 << 30, 1234567} {
		s := FormatBytes(n)
		got, err := ParseBytes(s)
		if err != nil {
			t.Errorf("ParseBytes(FormatBytes(%d)=%q) error: %v", n, s, err)
			continue
		}
		if got != n {
			t.Errorf("round-trip %d -> %q -> %d", n, s, got)
		}
	}
}
