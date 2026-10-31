package matrixdemo

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/mbb200291/go-occ-z/occ"
)

// Each CSV cell occupies one byte, followed by a comma or newline.
// Concurrent writes to different cells never rewrite shared CSV separators.
type Adapter struct {
	file *os.File
	size int
}

func Create(path string, size int) (*Adapter, error) {
	if size < 2 || size > 256 {
		return nil, fmt.Errorf("size must be between 2 and 256")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	row := strings.Repeat("0,", size-1) + "0\n"
	if _, err = f.WriteString(strings.Repeat(row, size)); err == nil {
		err = f.Sync()
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return &Adapter{file: f, size: size}, nil
}
func (a *Adapter) Close() error { return a.file.Close() }
func (a *Adapter) offset(target string) (int64, error) {
	row, col, ok := strings.Cut(target, ",")
	r, e1 := strconv.Atoi(row)
	c, e2 := strconv.Atoi(col)
	if !ok || e1 != nil || e2 != nil || r < 0 || c < 0 || r >= a.size || c >= a.size || target != fmt.Sprintf("%d,%d", r, c) {
		return 0, fmt.Errorf("invalid cell %q", target)
	}
	return int64(2 * (r*a.size + c)), nil
}
func (a *Adapter) Read(target string) (any, error) {
	offset, err := a.offset(target)
	if err != nil {
		return nil, err
	}
	var data [1]byte
	if _, err := a.file.ReadAt(data[:], offset); err != nil {
		return nil, err
	}
	return a.UnSerialize(data[:])
}
func (a *Adapter) Write(target string, value any) error {
	offset, err := a.offset(target)
	if err != nil {
		return err
	}
	data, err := a.Serialize(value)
	if err != nil {
		return err
	}
	n, err := a.file.WriteAt(data, offset)
	if err != nil {
		return err
	}
	if n != 1 {
		return io.ErrShortWrite
	}
	return a.file.Sync()
}
func (a *Adapter) Serialize(value any) ([]byte, error) {
	bit, ok := value.(byte)
	if !ok || bit > 1 {
		return nil, fmt.Errorf("expected binary byte, got %v", value)
	}
	return []byte{'0' + bit}, nil
}
func (a *Adapter) UnSerialize(data []byte) (any, error) {
	if len(data) != 1 || (data[0] != '0' && data[0] != '1') {
		return nil, fmt.Errorf("invalid bit backup %q", data)
	}
	return data[0] - '0', nil
}

var _ occ.TargetAdapter = (*Adapter)(nil)
