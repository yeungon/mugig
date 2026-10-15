## Download

| System | File |
|---|---|
| macOS 11 or newer, Apple silicon and Intel | **Mugig-macOS.zip** |
| Windows 10 or 11 (64-bit, Intel/AMD) | **Mugig-Windows.zip** |
| Linux (64-bit, Intel/AMD) | **Mugig-Linux.tar.xz** |

`SHA256SUMS` lists each file's checksum, to check a download is complete.

## Opening it the first time

Mugig isn't signed with a paid Apple or Microsoft certificate, so each
system warns about it once.

- **macOS:** unzip, and move Mugig to Applications. Open it; macOS says it
  can't verify the app. Click Done, then open **System Settings → Privacy &
  Security**, scroll down, and click **Open Anyway** beside the message about
  Mugig. Later, it opens like any other app.
- **Windows:** unzip, and run Mugig.exe. If SmartScreen says "Windows
  protected your PC", click **More info**, then **Run anyway**.
- **Linux:** unpack with `tar -xf Mugig-Linux.tar.xz`, then run
  `sudo make install` in the unpacked folder to add Mugig to your apps menu.
  It needs OpenGL, which desktop systems have.

Mugig is developed on macOS; the Windows and Linux builds pass the same
tests but have had less use.
