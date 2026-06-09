// Package quantity parses and renders Kubernetes-style storage quantity
// strings (e.g. "10Gi"), matching the chart PV capacity literals. It has no
// Ceph dependency and is fully unit-tested without a cluster.
package quantity

import (
	"fmt"
	"strconv"
	"strings"
)

// binaryUnits and decimalUnits map a suffix to its multiplier. Ordered
// largest-first for FormatBytes' canonical-unit search.
var binaryUnits = []struct {
	suffix string
	mult   uint64
}{
	{"Pi", 1 << 50},
	{"Ti", 1 << 40},
	{"Gi", 1 << 30},
	{"Mi", 1 << 20},
	{"Ki", 1 << 10},
}

var decimalUnits = []struct {
	suffix string
	mult   uint64
}{
	{"P", 1e15},
	{"T", 1e12},
	{"G", 1e9},
	{"M", 1e6},
	{"K", 1e3},
}

// ParseBytes converts a quantity string to a byte count. It accepts binary
// suffixes (Ki Mi Gi Ti Pi), decimal suffixes (K M G T P) and bare bytes.
// Empty, negative, fractional or otherwise unparseable input is an error.
func ParseBytes(s string) (uint64, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return 0, fmt.Errorf("empty quantity")
	}
	if strings.HasPrefix(trimmed, "-") {
		return 0, fmt.Errorf("negative quantity %q", s)
	}

	for _, u := range binaryUnits {
		if strings.HasSuffix(trimmed, u.suffix) {
			return scale(strings.TrimSuffix(trimmed, u.suffix), u.mult, s)
		}
	}
	for _, u := range decimalUnits {
		if strings.HasSuffix(trimmed, u.suffix) {
			return scale(strings.TrimSuffix(trimmed, u.suffix), u.mult, s)
		}
	}

	// Bare byte count (an optional trailing single-byte unit isn't supported;
	// keep it strict so typos surface).
	n, err := strconv.ParseUint(trimmed, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid quantity %q: want an integer optionally suffixed with Ki/Mi/Gi/Ti/Pi or K/M/G/T/P", s)
	}
	return n, nil
}

func scale(digits string, mult uint64, orig string) (uint64, error) {
	digits = strings.TrimSpace(digits)
	if digits == "" {
		return 0, fmt.Errorf("invalid quantity %q: missing number before unit", orig)
	}
	n, err := strconv.ParseUint(digits, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid quantity %q: %q is not a whole number (fractional quantities are not supported)", orig, digits)
	}
	if n != 0 && mult > (^uint64(0))/n {
		return 0, fmt.Errorf("quantity %q overflows uint64", orig)
	}
	return n * mult, nil
}

// FormatBytes renders n using the largest binary unit that divides it evenly,
// falling back to a bare byte count. The result round-trips through
// ParseBytes, so it is safe for state normalization.
func FormatBytes(n uint64) string {
	if n == 0 {
		return "0"
	}
	for _, u := range binaryUnits {
		if n%u.mult == 0 {
			return strconv.FormatUint(n/u.mult, 10) + u.suffix
		}
	}
	return strconv.FormatUint(n, 10)
}
