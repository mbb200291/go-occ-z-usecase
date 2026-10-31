package matrixdemo

import (
	"fmt"
	"math/rand"
)

type Config struct {
	Size, Blocks, BlockSize, Reads, ReadSize int
	Seed                                     int64
}

// Random local blocks are followed by local parity corrections. Then all jobs
// are shuffled, so corrections have no special execution order.
func Generate(c Config) ([]Job, error) {
	if c.Size < 2 || c.Size > 256 || c.BlockSize < 1 || c.BlockSize >= c.Size || c.Blocks < 0 || c.Reads < 0 || c.ReadSize < 1 || c.ReadSize > c.Size*c.Size {
		return nil, fmt.Errorf("require 2 <= n <= 256, 1 <= block-size < n, nonnegative blocks/reads, 1 <= read-size <= n*n")
	}
	rng := rand.New(rand.NewSource(c.Seed))
	parity := make([]byte, c.Size*c.Size)
	var jobs []Job
	add := func(kind string, cells []Cell) { jobs = append(jobs, Job{ID: len(jobs), Kind: kind, Cells: cells}) }
	for i := 0; i < c.Blocks; i++ {
		height, width := 1+rng.Intn(c.BlockSize), 1+rng.Intn(c.BlockSize)
		row, col := rng.Intn(c.Size-height+1), rng.Intn(c.Size-width+1)
		var cells []Cell
		for r := row; r < row+height; r++ {
			for cc := col; cc < col+width; cc++ {
				bit := byte(rng.Intn(2))
				cells = append(cells, Cell{r, cc, bit})
				parity[r*c.Size+cc] ^= bit
			}
		}
		add("xor", cells)
	}
	for row := 0; row < c.Size; row += c.BlockSize {
		for col := 0; col < c.Size; col += c.BlockSize {
			var cells []Cell
			for r := row; r < min(row+c.BlockSize, c.Size); r++ {
				for cc := col; cc < min(col+c.BlockSize, c.Size); cc++ {
					cells = append(cells, Cell{r, cc, 1 ^ parity[r*c.Size+cc]})
				}
			}
			add("xor", cells)
		}
	}
	for i := 0; i < c.Reads; i++ {
		ids := rng.Perm(c.Size * c.Size)[:c.ReadSize]
		cells := make([]Cell, len(ids))
		for j, id := range ids {
			cells[j] = Cell{Row: id / c.Size, Col: id % c.Size}
		}
		add("read", cells)
	}
	rng.Shuffle(len(jobs), func(i, j int) { jobs[i], jobs[j] = jobs[j], jobs[i] })
	return jobs, nil
}
