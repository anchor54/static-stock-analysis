package scanner

import (
	"math"
	"strconv"
)

// Num is a float64 that encodes NaN and ±Inf as JSON null.
type Num float64

// MarshalJSON rounds to 4 decimals and writes null for non-finite values.
func (n Num) MarshalJSON() ([]byte, error) {
	f := float64(n)
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return []byte("null"), nil
	}
	return strconv.AppendFloat(nil, math.Round(f*1e4)/1e4, 'f', -1, 64), nil
}
