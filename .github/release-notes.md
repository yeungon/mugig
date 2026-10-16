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

- **macOS:** unzip, move Mugig to Applications, and double-click it. macOS
  says it can't verify the app: click **Done**. Then, straight away, open
  **System Settings → Privacy & Security**, scroll to the very bottom, and
  click **Open Anyway** next to "Mugig was blocked to protect your Mac".
  Confirm with your password, and click **Open Anyway** once more. The button
  only shows for a while after you try to open Mugig; if it isn't there,
  double-click Mugig again. From then on, it opens like any other app.

  Or install it from Terminal, which skips the warning:

  ```sh
  curl -fL https://github.com/yeungon/mugig/releases/latest/download/Mugig-macOS.zip -o /tmp/Mugig.zip && ditto -x -k /tmp/Mugig.zip /Applications
  ```
- **Windows:** unzip, and run Mugig.exe; it needs nothing else installed. If SmartScreen says "Windows
  protected your PC", click **More info**, then **Run anyway**.
- **Linux:** unpack with `tar -xf Mugig-Linux.tar.xz`, go into the `mugig`
  folder it makes, and run `make user-install` to add Mugig to your apps
  menu (or `sudo make install` for everyone on the computer). It needs
  OpenGL, which desktop systems have.

## Updating

Once a day, Mugig asks GitHub, where it is released, for the newest version's
number (it sends nothing about you or your music). When a newer version is
out, a button at the foot of the sidebar says so and opens its page. **Help →
Check for Updates…** asks straight away, and **Help → Check for Updates
Automatically** turns the daily check off.

To update, download the new version from this page and put it in place of the old
one; your library, playlists, radio stations and settings are kept. On macOS,
a version downloaded with a browser needs **Open Anyway** once more; the
Terminal command above installs it over the old one without asking.

Mugig is developed on macOS; the Windows and Linux builds pass the same
tests but have had less use.
