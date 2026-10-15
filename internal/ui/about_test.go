package ui

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

// aboutTexts returns the About window's lines that are shown.
func aboutTexts(u *ui) string {
	var texts []string
	visit(u.win.Canvas().Overlays().Top(), func(o fyne.CanvasObject) {
		if !o.Visible() {
			return
		}
		switch x := o.(type) {
		case *widget.Label:
			texts = append(texts, x.Text)
		case *widget.Hyperlink:
			texts = append(texts, x.Text+" <"+x.URL.String()+">")
		}
	})
	return strings.Join(texts, "|")
}

func TestAbout(t *testing.T) {
	u, _, _ := newTestUI(t)
	old := appMetadata
	t.Cleanup(func() { appMetadata = old })
	appMetadata = func() fyne.AppMetadata { return fyne.AppMetadata{Name: "Mugig", Version: "0.1.0", Build: 42} }

	// In the Help menu, labelled as Fyne needs to move it to the app menu
	// on macOS.
	if u.mainMenu.Items[len(u.mainMenu.Items)-1].Label != "Help" {
		t.Error("Help is not the last menu")
	}
	menuItem(t, u, "About").Action()
	got := aboutTexts(u)
	for _, want := range []string{"Version 0.1.0 (42)", aboutTag, aboutIntro, "Crafted by Vuong",
		"github.com/yeungon/mugig <https://github.com/yeungon/mugig>", "© 2026 Vuong Nguyen · MIT License"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in %q", want, got)
		}
	}

	// Without metadata, as when run away from FyneApp.toml, there is no
	// version line rather than an empty one.
	appMetadata = func() fyne.AppMetadata { return fyne.AppMetadata{} }
	u.showAbout()
	if got := aboutTexts(u); strings.Contains(got, "Version") || strings.Contains(got, "||") {
		t.Errorf("without metadata: %q", got)
	}
}
