package library

import (
	"testing"

	"mugig/internal/quality"
)

func TestFold(t *testing.T) {
	for in, want := range map[string]string{
		"Trịnh Công Sơn": "trinh cong son",
		"Đặng Thái Sơn":  "dang thai son",
		"Khánh Ly":       "khanh ly",
		"Beyoncé":        "beyonce",
		"Motörhead":      "motorhead",
		"Sigur Rós":      "sigur ros",
		"Øystein Sevåg":  "oystein sevag",
		"Straße":         "strasse",
		"Ｔｏｋｙｏ":          "tokyo",    // full-width letters
		"童丽 - 枉凝眉":       "童丽 - 枉凝眉", // Chinese is unchanged
		"ガラスの少年":         "ガラスの少年",   // kana keep their voicing marks
		"ｶﾞﾗｽ":           "ガラス",      // half-width kana become full-width
		"Ελληνικά":       "ελληνικα", // Greek accents too
		"":               "",
	} {
		if got := Fold(in); got != want {
			t.Errorf("Fold(%q) = %q, want %q", in, got, want)
		}
	}
}

// Searching without accents finds songs with them, and the other way
// round; words can come in any order.
func TestSearchIgnoresAccents(t *testing.T) {
	song := Track{Title: "Diễm Xưa", Artist: "Trịnh Công Sơn", Album: "Ca Khúc Da Vàng"}
	for query, want := range map[string]bool{
		"trinh cong son": true,
		"TRỊNH":          true,
		"diem xua":       true,
		"xưa trinh":      true,
		"da vang":        true,
		"son tung":       false,
		"diễm xưa khanh": false,
	} {
		if got := Matches(song, SearchWords(query)); got != want {
			t.Errorf("searching %q: %v, want %v", query, got, want)
		}
	}
	scanned := song
	scanned.search = searchKey(song) // as a scan sets it
	for _, q := range []string{"trinh cong son", "son tung"} {
		if Matches(scanned, SearchWords(q)) != Matches(song, SearchWords(q)) {
			t.Errorf("searching %q: a scanned song and one made by hand should match alike", q)
		}
	}
}

// Smart playlists' text rules ignore accents as the search box does.
func TestSmartRulesIgnoreAccents(t *testing.T) {
	song := Track{Path: "/m/a.flac", Title: "Diễm Xưa", Artist: "Trịnh Công Sơn"}
	facts := SongFacts{Level: func(Track) quality.Level { return quality.Unknown }}
	for _, r := range []SmartRule{
		{"Artist", OpContains, "cong son"},
		{"Artist", OpIs, "trinh cong son"},
		{"Title", OpStartsWith, "diem"},
	} {
		if !r.Matches(song, facts) {
			t.Errorf("%s %s %q should match", r.Field, r.Op, r.Value)
		}
	}
	if (SmartRule{"Artist", OpNotContains, "sơn"}).Matches(song, facts) {
		t.Error("does not contain sơn should not match Trịnh Công Sơn")
	}
}
