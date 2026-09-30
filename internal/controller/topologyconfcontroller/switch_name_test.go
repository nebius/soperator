package topologyconfcontroller

import "testing"

func TestSlurmSafeSwitchName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "no trailing digits", in: "spine-a", want: "spine-a"},
		{name: "short digit tail", in: "switch1", want: "switch1"},
		{name: "hex hash ending in letter", in: "66b2be03e8b30ab5bcf8c9fd57d6c293", want: "66b2be03e8b30ab5bcf8c9fd57d6c293"},
		{name: "18-digit tail stays", in: "sw123456789012345678", want: "sw123456789012345678"},
		// 19 digits fit uint64 but exceed our 18-digit safety margin, so they are terminated.
		{name: "19-digit tail terminated", in: "sw1234567890123456789", want: "sw1234567890123456789_"},
		{
			name: "overflowing bug value terminated",
			in:   "6f84b74219aa22869602735141708147",
			want: "6f84b74219aa22869602735141708147_",
		},
		{name: "all digits overflowing", in: "123456789012345678901", want: "123456789012345678901_"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := slurmSafeSwitchName(tt.in); got != tt.want {
				t.Errorf("slurmSafeSwitchName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestTrailingDigitCount(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{in: "", want: 0},
		{in: "abc", want: 0},
		{in: "abc1", want: 1},
		{in: "1a2b3", want: 1},
		{in: "12345", want: 5},
		{in: "6f84b74219aa22869602735141708147", want: 20},
	}
	for _, tt := range tests {
		if got := trailingDigitCount(tt.in); got != tt.want {
			t.Errorf("trailingDigitCount(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}
