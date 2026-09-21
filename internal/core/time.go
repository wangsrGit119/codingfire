package core

import "time"

// Time is local wall-clock time, aliased so model/reader code reads like the
// C# it was ported from without importing time everywhere.
type Time = time.Time
