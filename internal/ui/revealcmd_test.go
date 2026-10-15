package ui

import (
	"slices"
	"testing"
)

func TestExplorerCommandLine(t *testing.T) {
	got := explorerCmdLine(`D:\Music\ABBA, Greatest\01 - SOS.flac`)
	if want := `explorer /select,"D:\Music\ABBA, Greatest\01 - SOS.flac"`; got != want {
		t.Errorf("%s\nwant %s", got, want)
	}
}

func TestShowItemsArgs(t *testing.T) {
	args := showItemsArgs("/media/usb/Nhạc Bất Hủ/ABBA - SOS (Album Version).flac")
	want := "array:string:file:///media/usb/Nh%E1%BA%A1c%20B%E1%BA%A5t%20H%E1%BB%A7/ABBA%20-%20SOS%20%28Album%20Version%29.flac"
	if !slices.Contains(args, want) || args[len(args)-1] != "string:" {
		t.Errorf("%q", args)
	}
	if !slices.Contains(args, "org.freedesktop.FileManager1.ShowItems") {
		t.Errorf("%q", args)
	}
}
