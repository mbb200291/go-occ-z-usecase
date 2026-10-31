package main

import "math"

// Probability that one uniformly sampled k-target request overlaps at least
// one of concurrent-1 independently sampled requests. This is not a conflict rate.
func overlapProbability(total, k, concurrent int) float64 {
	if concurrent <= 1 {
		return 0
	}
	if 2*k > total {
		return 1
	}
	logDisjoint := 0.0
	for i := 0; i < k; i++ {
		logDisjoint += math.Log1p(-float64(k) / float64(total-i))
	}
	return -math.Expm1(float64(concurrent-1) * logDisjoint)
}

func nearestOverlapSize(total, concurrent int, wanted float64) int {
	lo, hi := 1, total/2+1
	for lo < hi {
		mid := (lo + hi) / 2
		if overlapProbability(total, mid, concurrent) < wanted {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo > 1 && math.Abs(overlapProbability(total, lo-1, concurrent)-wanted) <= math.Abs(overlapProbability(total, lo, concurrent)-wanted) {
		return lo - 1
	}
	return lo
}
