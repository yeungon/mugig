package ui

// The commands that show a file in Windows Explorer and in Linux file
// managers, built here rather than in reveal_windows.go and reveal_other.go
// so they are tested on every system.

import "net/url"

// explorerCmdLine is the command line that opens Explorer on the file's
// folder with the file selected. Explorer parses its command line itself
// and needs exactly this form, so it is passed whole rather than as
// arguments, which Go would quote differently.
func explorerCmdLine(path string) string {
	return `explorer /select,"` + path + `"`
}

// showItemsArgs are dbus-send's arguments to ask the desktop's file manager
// to show the file selected, through the freedesktop.org FileManager1
// interface, which GNOME Files, Dolphin, Nemo, Caja and others answer.
func showItemsArgs(path string) []string {
	return []string{
		"--session", "--print-reply", "--type=method_call",
		"--dest=org.freedesktop.FileManager1", "/org/freedesktop/FileManager1",
		"org.freedesktop.FileManager1.ShowItems",
		"array:string:" + fileURI(path), "string:",
	}
}

// fileURI is path as a file:// URI, with spaces and other characters
// escaped: "/a b/c.flac" is "file:///a%20b/c.flac".
func fileURI(path string) string {
	return (&url.URL{Scheme: "file", Path: path}).String()
}
