// Package audio reads and plays audio files.
//
// It finds a file's format from its own bytes (Probe), decodes FLAC, WAV,
// MP3 and Ogg Vorbis (Decode), and plays songs one after another without
// gaps through the chosen output device (Player), with the speed, the
// equalizer, crossfeed and volume applied on the way. On macOS it can
// switch the device to each song's sample rate and take it for Mugig
// alone, and it can read files with the system's own decoder as well.
//
// A song from a CUE sheet is a stretch of a larger file; RememberSpan
// records where, and everything here then plays and reads just that
// stretch.
//
// Nothing here knows about the UI: the player's callbacks run through the
// function given to NewPlayer, on the UI thread.
package audio
