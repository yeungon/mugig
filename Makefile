# Fyne's packaging tool, run at a pinned version so it needn't be installed.
FYNE := go run fyne.io/tools/cmd/fyne@v1.7.3
APP  := Mugig.app

.PHONY: run test fuzz build package icon install clean \
	mac mac-arm64 mac-intel windows windows-arm64 linux linux-arm64 all-platforms

# The app is main.go and the packages under internal/.
run:
	go run .

# Fyne drops cached drawing objects it thinks unused after a minute; the
# race detector makes the tests run longer than that, and the test window
# then draws without them. FYNE_CACHE keeps them for the whole run.
test:
	FYNE_CACHE=1h go test -race ./...

# Feeds corrupt files to everything that reads them, a minute per target.
# Crashers found are saved under each package's testdata/fuzz and replayed
# by `make test`.
fuzz:
	for t in audio:FuzzProbe audio:FuzzDecode library:FuzzReadTrack quality:FuzzAnalyse; do \
		go test -run '^$$' -fuzz "^$${t#*:}$$" -fuzztime 1m ./internal/$${t%%:*} || exit 1; \
	done

build:
	go build -o mugig .

# Packages the app for this computer: "Mugig.app" on macOS. macOS gets the
# square Icon.png, which fyne rounds to Apple's shape itself; Windows and
# Linux show the icon as it is, so they get the rounded one.
ifeq ($(shell uname -s),Darwin)
ICON := Icon.png
else
ICON := IconRounded.png
endif

package:
	$(FYNE) package --release --icon $(ICON)

# Redraws Icon.png and IconRounded.png after a change to tools/mkicon.
icon:
	go run ./tools/mkicon

# Copies the app into /Applications so it shows in Launchpad and Spotlight.
install: package
	rm -rf "/Applications/$(APP)"
	cp -R "$(APP)" /Applications/

clean:
	rm -rf mugig "$(APP)" dist fyne-cross

# ---------------------------------------------------------------------------
# Builds for other systems, collected in dist/.
#
# macOS builds are made here. Windows and Linux builds need those systems'
# C compilers, as Fyne and the audio output use C, so fyne-cross makes them
# in Docker containers: Docker Desktop must be running, and the first build
# downloads its build image (about 1-2 GB per system).
# ---------------------------------------------------------------------------

FYNE_CROSS := go run github.com/fyne-io/fyne-cross@v1.6.3
DIST       := dist
SRC        := $(CURDIR)

# The release workflow stamps the version and build number with these, as
# in `make mac APP_VERSION=0.1.0 APP_BUILD=7`. Otherwise FyneApp.toml's are
# used.
VERSION_FLAGS := $(if $(APP_VERSION),--app-version $(APP_VERSION)) $(if $(APP_BUILD),--app-build $(APP_BUILD))

# mac-finish APP: Go programs need macOS 11, though fyne writes 10.11, so an
# older Mac would crash rather than say why. Then the whole app is signed ad
# hoc: fyne leaves only the program inside signed, and a downloaded app with
# an unsigned bundle is called "damaged" by macOS, with no way to open it.
# Signed, it gets the usual warning for apps without an Apple developer ID,
# which Privacy & Security's Open Anyway gets past.
define mac-finish
	/usr/libexec/PlistBuddy -c "Set :LSMinimumSystemVersion 11.0" "$(1)/Contents/Info.plist"
	codesign --force --deep --sign - --identifier org.kiwipanel.musicscan "$(1)"
	codesign --verify --deep --strict "$(1)"
endef

# fyne package uses an existing binary only when given its full path;
# otherwise it builds one for this Mac.
#
# mac-build ARCH: builds the app for Apple silicon (arm64) or Intel (amd64)
# into dist/macos-ARCH/Mugig.app.
define mac-build
	rm -rf "$(DIST)/macos-$(1)" && mkdir -p "$(DIST)/macos-$(1)"
	GOOS=darwin GOARCH=$(1) CGO_ENABLED=1 go build -trimpath -ldflags "-s -w" -o "$(DIST)/macos-$(1)/mugig" .
	cd "$(DIST)/macos-$(1)" && $(FYNE) package --release --src "$(SRC)" --exe "$(CURDIR)/$(DIST)/macos-$(1)/mugig" --name Mugig --icon "$(SRC)/Icon.png" $(VERSION_FLAGS) && rm mugig
	$(call mac-finish,$(DIST)/macos-$(1)/Mugig.app)
endef

# One app for both Apple silicon and Intel Macs.
mac:
	rm -rf "$(DIST)/macos" "$(DIST)/macos-tmp" && mkdir -p "$(DIST)/macos" "$(DIST)/macos-tmp"
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=1 go build -trimpath -ldflags "-s -w" -o "$(DIST)/macos-tmp/arm64" .
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=1 go build -trimpath -ldflags "-s -w" -o "$(DIST)/macos-tmp/amd64" .
	lipo -create -output "$(DIST)/macos/mugig" "$(DIST)/macos-tmp/arm64" "$(DIST)/macos-tmp/amd64"
	cd "$(DIST)/macos" && $(FYNE) package --release --src "$(SRC)" --exe "$(CURDIR)/$(DIST)/macos/mugig" --name Mugig --icon "$(SRC)/Icon.png" $(VERSION_FLAGS) && rm mugig
	rm -rf "$(DIST)/macos-tmp"
	$(call mac-finish,$(DIST)/macos/Mugig.app)
	@echo "Built $(DIST)/macos/Mugig.app (Apple silicon and Intel)"

mac-arm64:
	$(call mac-build,arm64)

mac-intel:
	$(call mac-build,amd64)

# cross OS ARCH: builds with fyne-cross, then copies the package into
# dist/OS-ARCH. Windows and Linux show the icon as it is, so they get the
# rounded one.
define cross
	@docker info >/dev/null 2>&1 || { echo "Docker isn't running: start Docker Desktop, then try again."; exit 1; }
	$(FYNE_CROSS) $(1) -arch=$(2) -icon IconRounded.png
	rm -rf "$(DIST)/$(1)-$(2)" && mkdir -p "$(DIST)"
	cp -R "fyne-cross/dist/$(1)-$(2)" "$(DIST)/$(1)-$(2)"
	@echo "Built $(DIST)/$(1)-$(2):" && ls "$(DIST)/$(1)-$(2)"
endef

# Windows on Intel/AMD PCs, the usual kind: a zip holding Mugig.exe.
windows:
	$(call cross,windows,amd64)

# Windows on ARM, such as Snapdragon laptops.
windows-arm64:
	$(call cross,windows,arm64)

# Linux on Intel/AMD PCs: a .tar.xz with the app, its icon and a menu
# entry, installed with `make install` from inside it.
linux:
	$(call cross,linux,amd64)

# Linux on ARM, such as a Raspberry Pi 4 or 5 with a 64-bit system.
linux-arm64:
	$(call cross,linux,arm64)

# Everything most people need: Mac (both kinds), Windows and Linux PCs.
all-platforms: mac windows linux
