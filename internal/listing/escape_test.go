package listing

import "testing"

func TestEscapeLike(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"flat", "flat"},
		{"%", `\%`},
		{"_", `\_`},
		{`\`, `\\`},
		{"a%b_c", `a\%b\_c`},
	}
	for _, tt := range tests {
		if got := escapeLike(tt.in); got != tt.want {
			t.Fatalf("escapeLike(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
