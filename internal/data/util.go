package data

import (
	"fmt"
	"strconv"
)

// Small local helpers so the ported code reads close to the C# it came from.

func sprintf(format string, args ...any) string { return fmt.Sprintf(format, args...) }

func itoa(v int) string { return strconv.Itoa(v) }

func toString(v any) string {
	if v == nil {
		return "<nil>"
	}
	if err, ok := v.(error); ok {
		return err.Error()
	}
	return fmt.Sprint(v)
}

func maxF(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
