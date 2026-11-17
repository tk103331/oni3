package wire

import "testing"

func TestNormalizeRole(t *testing.T) {
	cases := map[string]string{
		"user":       RoleUser,
		"ROLE_USER":  RoleUser,
		"role_user":  RoleUser,
		"agent":      RoleAgent,
		"assistant":  RoleAgent,
		"ROLE_AGENT": RoleAgent,
		"unexpected": "unexpected",
	}
	for input, want := range cases {
		if got := NormalizeRole(input); got != want {
			t.Fatalf("NormalizeRole(%q) = %q, want %q", input, got, want)
		}
	}
}
