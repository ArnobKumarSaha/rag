package embed

import (
	"errors"
	"fmt"
	"math"
)

func Norm(v []float64) float64 {
	var sum float64
	for _, x := range v {
		sum += x * x
	}
	return math.Sqrt(sum)
}

func Cosine(a, b []float64) (float64, error) {
	if len(a) != len(b) {
		return 0, fmt.Errorf("dimension mismatch: %d vs %d", len(a), len(b))
	}
	na, nb := Norm(a), Norm(b)
	if na == 0 || nb == 0 {
		return 0, errors.New("cosine of a zero vector is undefined")
	}
	var dot float64
	for i := range a {
		dot += a[i] * b[i]
	}
	return dot / (na * nb), nil
}
