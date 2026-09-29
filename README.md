# img2ssh

Copy an image on Windows, paste into any terminal connected to your SSH server, and let the remote terminal application read the image from disk.

`img2ssh` watches the Windows clipboard, uploads copied images over SSH, and puts a text path back into the clipboard alongside the original image. Terminal paste receives the path; image applications still receive the image.

```text
[copy an image on Windows]
          ↓
img2ssh uploads it → user@server:~/.img2ssh/img1.png
          ↓
clipboard contains: image + [~/.img2ssh/img1.png]
          ↓
paste into any SSH terminal → the terminal receives the path
          ↓
the application running in that remote terminal reads the image file
```

## Requirements

- Windows 10 or Windows 11
- Windows OpenSSH Client (`ssh.exe`)
- An SSH server reachable with a key; password authentication is not supported
- A terminal application connected to that SSH server

The Windows implementation uses native Win32 clipboard formats and a per-user interactive Task Scheduler task, so clipboard access works after login.

## Install

Run this in PowerShell:

```powershell
irm https://raw.githubusercontent.com/bulletsrip/img2ssh/main/install.ps1 | iex
```

Then configure your server:

```powershell
img2ssh setup
```

Setup asks for the SSH host, username, private-key path, and remote image directory. It installs the clipboard watcher as a per-user Windows Task Scheduler daemon that runs silently in the background after login. You do not need to keep PowerShell, `img2ssh logs`, or an SSH session open.

The daemon:

- Starts automatically when your Windows user logs in
- Runs without opening a terminal window
- Watches the clipboard in the background
- Connects to the configured SSH server only when an image is copied
- Can be checked with `img2ssh status`

Test SSH first if needed:

```powershell
ssh -i $env:USERPROFILE\.ssh\id_ed25519 user@your-server.example
```

For an AWS `.pem` key, enter its full path during setup.

## How to use

1. Press **Win + Shift + S** and select an area, or copy any image to the Windows clipboard.
2. Paste into any terminal session connected to your SSH server.
3. The terminal receives a path such as `[~/.img2ssh/img1.png]`.
4. The application running in that remote terminal can read the image at that path.

The upload runs in the background. You can copy and paste multiple images; filenames cycle through `img1.png` to `img10.png`. By default, the remote server keeps the latest **10 images** and automatically removes older files. Configure this from PowerShell:

```powershell
img2ssh retention       # Show the current limit
img2ssh retention 5    # Keep the latest 5 images
```

The retention limit can be set from 1 to 10 images.

## Commands

```text
img2ssh setup          Configure a server and install startup task
img2ssh status         Show watcher status and last sync
img2ssh add-server     Add another SSH server
img2ssh remove-server  Remove a server
img2ssh pause          Pause syncing
img2ssh resume         Resume syncing
img2ssh restart        Restart the watcher
img2ssh logs           Show the watcher log
img2ssh retention      Show or set remote image retention
img2ssh uninstall      Remove the watcher and configuration
```

## Configuration

Configuration is stored at:

```text
%APPDATA%\img2ssh\config.json
```

Example:

```json
{
  "servers": [
    {
      "host": "your-server.example",
      "user": "user",
      "ssh_key": "C:\\Users\\you\\.ssh\\id_ed25519",
      "sync_path": "$HOME/.img2ssh"
    }
  ],
  "settings": {
    "poll_interval_ms": 300,
    "max_file_size_kb": 10240,
    "compress_above_kb": 4096,
    "keep_last_n_files": 10,
    "paused": false
  }
}
```

## Build from source

Install Go 1.26.3 or newer, then run in PowerShell:

```powershell
git clone https://github.com/bulletsrip/img2ssh
cd img2ssh
go build -o img2ssh.exe .
```

## License

MIT
