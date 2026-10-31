package main

import (
	"math"
	"testing"
)

func TestOverlapProbability(t *testing.T) {
	// A fixed 2-element subset has one disjoint choice among C(4,2)=6.
	for _, tc := range []struct {
		m, k, c int
		want    float64
	}{{4, 2, 2, 5.0 / 6}, {4, 2, 3, 35.0 / 36}, {4, 3, 2, 1}, {4, 2, 1, 0}, {64, 1, 20, 1 - math.Pow(63.0/64, 19)}} {
		if got := overlapProbability(tc.m, tc.k, tc.c); math.Abs(got-tc.want) > 1e-12 {
			t.Fatalf("%+v: %g", tc, got)
		}
	}
}
func TestNearestOverlap(t *testing.T) {
	for _, p := range []float64{0, .1, .3, .6, .9, 1} {
		k := nearestOverlapSize(256, 10, p)
		distance := math.Abs(overlapProbability(256, k, 10) - p)
		for j := 1; j <= 256; j++ {
			if math.Abs(overlapProbability(256, j, 10)-p) < distance-1e-12 {
				t.Fatalf("not nearest: %g, %d", p, k)
			}
		}
	}
}
