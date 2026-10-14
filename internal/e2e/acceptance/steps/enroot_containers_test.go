package steps

import "testing"

func TestParseEnrootImageDigest(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{
			name:   "digest only",
			output: "sha256:8fae5a12415451c8509319617b28f1253d99e6bd883fe40a95678bda09665e00\n",
			want:   "sha256:8fae5a12415451c8509319617b28f1253d99e6bd883fe40a95678bda09665e00",
		},
		{
			name:   "digest after status output",
			output: "resolving image\n sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef \n",
			want:   "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		},
		{
			name:   "missing digest",
			output: "resolving image\n",
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseEnrootImageDigest(tt.output); got != tt.want {
				t.Fatalf("parseEnrootImageDigest() = %q, want %q", got, tt.want)
			}
		})
	}
}
