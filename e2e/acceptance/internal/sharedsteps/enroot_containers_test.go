package sharedsteps

import "testing"

func TestOSReleaseID(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{
			name:   "unquoted ID",
			output: "NAME=Ubuntu\nID=ubuntu\nID_LIKE=debian\n",
			want:   "ubuntu",
		},
		{
			name:   "quoted ID",
			output: "NAME=Ubuntu\nID=\"ubuntu\"\n",
			want:   "ubuntu",
		},
		{
			name:   "missing ID",
			output: "NAME=Ubuntu\nID_LIKE=debian\n",
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := osReleaseID(tt.output); got != tt.want {
				t.Fatalf("osReleaseID() = %q, want %q", got, tt.want)
			}
		})
	}
}
