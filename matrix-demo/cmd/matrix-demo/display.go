package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// Keep redirected output readable; only redraw interactive terminal output.
func liveOutput(out io.Writer) bool {
	file, ok := out.(*os.File)
	if !ok || os.Getenv("TERM") == "dumb" {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func drawMatrix(out io.Writer, live bool, header, label string, number int, attempts int64, n int, values []int) error {
	var frame strings.Builder
	if live {
		frame.WriteString("\x1b[2J\x1b[H")
		frame.WriteString(header)
	}
	fmt.Fprintf(&frame, "\n%s (validated read #%d, attempts=%d)\n", label, number, attempts)
	for row := 0; row < n; row++ {
		for col := 0; col < n; col++ {
			if col > 0 {
				frame.WriteByte(' ')
			}
			fmt.Fprint(&frame, values[row*n+col])
		}
		frame.WriteByte('\n')
	}
	_, err := io.WriteString(out, frame.String())
	return err
}
