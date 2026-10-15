// Package library finds the songs in the music folders and keeps track of
// them.
//
// Walk lists a folder's audio files, leaving out folders that aren't
// music; ScanFile reads each one's tags and length, or takes them from the
// Cache when the file hasn't changed. A CUE sheet beside a disc image
// turns the image into its songs. ReadChanges follows a folder after a
// change, and WatchFolder reports changes as they happen. Playlists, and
// smart playlists' rules, are kept here too.
//
// Nothing here knows about the UI.
package library
