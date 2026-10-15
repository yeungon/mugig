//go:build !darwin

package library

// scanWorkers reads in parallel, as most computers now have SSDs; there is
// no drive check on these systems yet.
func ScanWorkers(string) int { return parallelWorkers() }
