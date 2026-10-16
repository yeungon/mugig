// Package radio reads internet radio: it connects to a station's link and
// hands over its sound as it arrives, still encoded, for package audio to
// decode and play.
//
// A link can be a stream itself, as Icecast and Shoutcast servers send it
// (MP3, AAC or Ogg, with the song playing named now and then), an HLS
// playlist (.m3u8) of short pieces fetched one after another, or a .pls or
// .m3u playlist that points to either. HLS pieces come as MPEG transport
// streams, as plain AAC or MP3, or as fragmented MP4; the audio is taken
// out of each, so what is handed over is always plain MP3, ADTS AAC or Ogg.
//
// It also keeps the list of stations the user has added (stations.go).
//
// Nothing here decodes sound or knows about the UI.
package radio
