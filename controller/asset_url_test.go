package controller

import "testing"

func TestIsPublicHTTPURL(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want bool
	}{
		{"https public host", "https://example.com/x.png", true},
		{"http public host with query", "http://cdn.example.com/x.png?token=abc", true},
		{"https public ipv4", "https://8.8.8.8/x.png", true},

		{"loopback ipv4", "http://127.0.0.1/x.png", false},
		{"loopback ipv6", "http://[::1]/x.png", false},
		{"localhost hostname", "http://localhost/x.png", false},
		{"mdns .local", "http://server.local/x.png", false},
		{"private 10/8", "http://10.0.0.1/x.png", false},
		{"private 172.16/12", "http://172.20.1.1/x.png", false},
		{"private 192.168/16", "http://192.168.1.1/x.png", false},
		{"link-local 169.254", "http://169.254.169.254/x.png", false},
		{"ipv6 private fc00", "http://[fc00::1]/x.png", false},
		{"unspecified 0.0.0.0", "http://0.0.0.0/x.png", false},

		{"file scheme", "file:///etc/x.png", false},
		{"data url", "data:image/png;base64,AAAA", false},
		{"empty string", "", false},
		{"malformed", "not a url", false},
		{"scheme only", "https://", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isPublicHTTPURL(tc.url)
			if got != tc.want {
				t.Errorf("isPublicHTTPURL(%q) = %v, want %v", tc.url, got, tc.want)
			}
		})
	}
}
