//go:build !unix

package sweepuntilfilter

// deviceID is unavailable on non-unix platforms; callers fall back to a
// stats-based deduplication key.
func deviceID(string) (uint64, bool) { return 0, false }
