# Fyne's packaging tool, run at a pinned version so it needn't be installed.
FYNE := go run fyne.io/tools/cmd/fyne@v1.7.3
APP  := Mugig.app

.PHONY: run test fuzz build package install clean

# Builds every .go file in the package (main.go alone misses the rest).
run:
	go run .

test:
	go test -race ./...

# Feeds corrupt files to everything that reads them, a minute per target.
# Crashers found are saved under testdata/fuzz and replayed by `make test`.
fuzz:
	for t in FuzzProbe FuzzReadTrack FuzzDecode FuzzAnalyse; do \
		go test -run '^$$' -fuzz "^$$t$$" -fuzztime 1m . || exit 1; \
	done

build:
	go build -o mugig .

# Makes "Mugig.app" with the icon and details from FyneApp.toml.
package:
	$(FYNE) package --release

# Copies the app into /Applications so it shows in Launchpad and Spotlight.
install: package
	rm -rf "/Applications/$(APP)"
	cp -R "$(APP)" /Applications/

clean:
	rm -rf mugig "$(APP)"
