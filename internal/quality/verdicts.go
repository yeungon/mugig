package quality

// levelNames are the column's words for each verdict; unchecked songs stay
// blank.
var LevelNames = map[Level]string{
	Lossy:    "Lossy",
	Lossless: "Lossless",
	HiRes:    "Hi-Res",
	Suspect:  "Lossless?",
	Damaged:  "Damaged",
}

// levelRank orders the column so problems sort first and unchecked songs
// last.
var LevelRank = map[Level]int{
	Damaged:  0,
	Suspect:  1,
	Lossy:    2,
	Lossless: 3,
	HiRes:    4,
	Unknown:  5,
}
