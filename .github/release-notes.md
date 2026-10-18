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
out, a button at the foot of the sidebar says so. Click it, then **Update
Now**: Mugig downloads the new version, checks that it is signed by Mugig's
author, puts it in place of the old one, and opens it. Your library,
playlists, radio stations and settings are kept. **Help → Check for
Updates…** asks straight away, and **Help → Check for Updates
Automatically** turns the daily check off.

Mugig updates itself from version 0.3.0 on. Where it isn't allowed to change
the folder it is in, as on Linux when installed for every user with `sudo
make install`, it opens the new version's page instead: download it from this page
and put it in place of the old one. On macOS, a version downloaded with a
browser needs **Open Anyway** once more; the Terminal command above installs
it without asking. As Mugig isn't signed with an Apple developer ID, macOS
may also ask again, after an update, for access to folders such as an
external disk's.

Mugig is developed on macOS; the Windows and Linux builds pass the same
tests but have had less use.
