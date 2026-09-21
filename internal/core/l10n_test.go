package core

import "testing"

func TestCompact(t *testing.T) {
	cases := map[int64]string{
		999:           "999",
		1_200:         "1.20k",
		12_500:        "12.5k",
		999_999:       "1.00M",
		3_400_000:     "3.40M",
		999_999_999:   "1.00B",
		2_100_000_000: "2.10B",
	}
	for value, want := range cases {
		if got := Compact(value); got != want {
			t.Errorf("Compact(%d) = %q, want %q", value, got, want)
		}
	}
}
