package matrixdemo

import (
	"fmt"
	"sync/atomic"

	"github.com/mbb200291/go-occ-z/occ"
)

type Cell struct {
	Row, Col int
	Bit      byte
}

func (c Cell) key() string { return fmt.Sprintf("%d,%d", c.Row, c.Col) }

type Job struct {
	ID    int
	Kind  string
	Cells []Cell
}

// XOR is a read-modify-write command, even though the user action is a write.
// Attempts is diagnostic only; the transaction data stays in Context.
type Command struct {
	Job
	Attempts atomic.Int64
}

func (c *Command) IsReadOnly() bool  { return c.Kind == "read" }
func (c *Command) IsWriteOnly() bool { return false }
func (c *Command) GetTargets() []string {
	targets := make([]string, len(c.Cells))
	for i, p := range c.Cells {
		targets[i] = p.key()
	}
	return targets
}
func (c *Command) Execute(ctx occ.Context) (any, error) {
	c.Attempts.Add(1)
	if c.Kind != "xor" && c.Kind != "read" {
		return nil, fmt.Errorf("unknown command %q", c.Kind)
	}
	values := make([]byte, 0, len(c.Cells))
	for _, p := range c.Cells {
		data, err := ctx.GetData(p.key())
		if err != nil {
			return nil, err
		}
		v, ok := data.(byte)
		if !ok || v > 1 || p.Bit > 1 {
			return nil, fmt.Errorf("invalid binary cell %s", p.key())
		}
		if c.Kind == "xor" {
			ctx.SetData(p.key(), v^p.Bit)
		} else {
			values = append(values, v)
		}
	}
	if c.IsReadOnly() {
		return values, nil
	}
	return nil, nil
}
func FullRead(n int) *Command {
	c := &Command{Job: Job{Kind: "read"}}
	for row := 0; row < n; row++ {
		for col := 0; col < n; col++ {
			c.Cells = append(c.Cells, Cell{Row: row, Col: col})
		}
	}
	return c
}

var _ occ.Command = (*Command)(nil)
