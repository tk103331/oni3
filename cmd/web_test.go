package cmd

import "testing"

func TestWebCommandRegistered(t *testing.T) {
	command, _, err := RootCmd.Find([]string{"web"})
	if err != nil {
		t.Fatal(err)
	}
	if command == nil || command.Name() != "web" {
		t.Fatalf("web command not registered: %+v", command)
	}
	for _, name := range []string{"addr", "agent", "open", "token"} {
		if command.Flags().Lookup(name) == nil {
			t.Fatalf("web command is missing --%s", name)
		}
	}
}

func TestWebURL(t *testing.T) {
	cases := []struct {
		addr string
		want string
	}{
		{"127.0.0.1:8080", "http://127.0.0.1:8080"},
		{"0.0.0.0:9000", "http://127.0.0.1:9000"},
		{"[::]:9000", "http://127.0.0.1:9000"},
		{"[::1]:9000", "http://[::1]:9000"},
	}
	for _, tc := range cases {
		if got := webURL(tc.addr); got != tc.want {
			t.Fatalf("webURL(%q) = %q, want %q", tc.addr, got, tc.want)
		}
	}
}

func TestWebLoopbackDetection(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:8080", true},
		{"[::1]:8080", true},
		{"localhost:8080", true},
		{"0.0.0.0:8080", false},
		{"192.168.1.5:8080", false},
	}
	for _, tc := range cases {
		if got := isLoopbackAddr(tc.addr); got != tc.want {
			t.Fatalf("isLoopbackAddr(%q) = %v, want %v", tc.addr, got, tc.want)
		}
	}
}
