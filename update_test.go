package main

import "testing"

func TestVerNewer(t *testing.T) {
	cases := []struct {
		r, c string
		want bool
	}{
		{"v1.1.2", "1.1.1", true}, {"v1.1.1", "1.1.1", false}, {"v1.1.0", "1.1.1", false},
		{"v1.2", "1.1.9", true}, {"PC-1.1.2", "1.1.1", true}, {"v1.10.0", "1.9.9", true}, {"junk", "1.1.1", false},
	}
	for _, c := range cases {
		if got := verNewer(c.r, c.c); got != c.want {
			t.Errorf("verNewer(%q,%q)=%v want %v", c.r, c.c, got, c.want)
		}
	}
}
