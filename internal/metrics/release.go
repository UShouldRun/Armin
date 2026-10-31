//go:build !debug

package metrics

// Debug is false in release builds: only counts are kept.
const Debug = false
