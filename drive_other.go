//go:build !darwin

package main

// scanWorkers reads in parallel, as most computers now have SSDs; there is
// no drive check on these systems yet.
func scanWorkers(string) int { return parallelWorkers() }
