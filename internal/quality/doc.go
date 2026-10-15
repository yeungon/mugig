// Package quality checks what an audio file really contains.
//
// For lossless files, Analyse decodes all the audio: it compares a FLAC
// file's audio with the MD5 checksum stored in it, finds WAV files cut
// short, and looks in the spectrum for the cut-off and the gaps a lossy
// encoder leaves, so a "lossless" file made from an MP3 shows. It also
// spots 24-bit files padded from 16 bits and hi-res files upsampled from
// CD quality, measures loudness (ITU-R BS.1770-4), true peak, the DR
// dynamic range and clipping, and draws a spectrogram.
//
// Assess turns the measurements into a verdict (a Level) and plain words,
// worded as cautiously as the evidence calls for. Store keeps results on
// disk, so each file is checked once until it changes.
package quality
