## Download

| System | File |
|---|---|
| macOS 11 or newer, Apple silicon and Intel | **Mugig-macOS.zip** |
| Windows 10 or 11 (64-bit, Intel/AMD) | **Mugig-Windows.zip** |
| Linux (64-bit, Intel/AMD): Ubuntu 22.04, Debian 12 or newer | **Mugig-Linux.tar.xz** |

`SHA256SUMS` lists each file's checksum, to check a download is complete.

## Opening it the first time

Mugig isn't signed with a paid Apple or Microsoft certificate, so each
system warns about it once.

- **macOS:** unzip, and move Mugig to Applications. Open it; macOS says it
  can't verify the app. Click Done, then open **System Settings → Privacy &
  Security**, scroll down, and click **Open Anyway** beside the message about
  Mugig. Later, it opens like any other app.
- **Windows:** unzip, and run Mugig.exe; it needs nothing else installed. If SmartScreen says "Windows
  protected your PC", click **More info**, then **Run anyway**.
- **Linux:** unpack with `tar -xf Mugig-Linux.tar.xz`, go into the `mugig`
  folder it makes, and run `make user-install` to add Mugig to your apps
  menu (or `sudo make install` for everyone on the computer). It needs
  OpenGL, which desktop systems have.

Mugig is developed on macOS; the Windows and Linux builds pass the same
tests but have had less use.
