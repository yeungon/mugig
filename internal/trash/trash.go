// Package trash moves files to the system's Trash (the Recycle Bin on
// Windows), from where the user can put them back, rather than deleting
// them.
package trash

// Move moves the file at path to the Trash. On macOS and Linux, a file on
// a removable disk goes to that disk's own Trash.
func Move(path string) error {
	return move(path)
}
