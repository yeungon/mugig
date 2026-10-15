package library

import "runtime"

// How many files a scan reads at once. Reading many at once is fastest on
// SSDs, but makes a spinning disk seek back and forth, which is slow, so
// the scan only goes wide on drives known to be solid-state.

// parallelWorkers is for solid-state drives.
func parallelWorkers() int { return max(4, runtime.NumCPU()) }

// GentleWorkers is for spinning disks, and drives that may be one.
const GentleWorkers = 2
