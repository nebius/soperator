package v1alpha1

import "testing"

func TestImageGetURI(t *testing.T) {
	tests := []struct {
		name     string
		image    Image
		expected string
	}{
		{
			name: "tagged image",
			image: Image{
				Repository: "registry.example.com/example/image",
				Tag:        "v1.2.3",
			},
			expected: "registry.example.com/example/image:v1.2.3",
		},
		{
			name: "digest image",
			image: Image{
				Repository: "registry.example.com/example/image@sha256:0123456789abcdef",
			},
			expected: "registry.example.com/example/image@sha256:0123456789abcdef",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if actual := test.image.GetURI(); actual != test.expected {
				t.Fatalf("expected %q, got %q", test.expected, actual)
			}
		})
	}
}
