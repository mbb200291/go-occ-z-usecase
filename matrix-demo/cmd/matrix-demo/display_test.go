package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestDisplayModes(t *testing.T) {
	for _, live := range []bool{false, true} {
		var out bytes.Buffer
		for i := 1; i <= 2; i++ {
			if err := drawMatrix(&out, live, "matrix demo\n", "in progress", i, 1, 2, []int{0, 1, 1, 0}); err != nil {
				t.Fatal(err)
			}
		}
		text := out.String()
		if strings.Count(text, "\x1b[2J\x1b[H") != map[bool]int{false: 0, true: 2}[live] {
			t.Fatalf("redraw=%q", text)
		}
		if !strings.Contains(text, "0 1\n1 0\n") {
			t.Fatal(text)
		}
	}
	var out bytes.Buffer
	if liveOutput(&out) {
		t.Fatal("redirected output must not redraw")
	}
}
