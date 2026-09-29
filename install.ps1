$ErrorActionPreference = 'Stop'

$repository = 'bulletsrip/img2ssh'
$architecture = switch ($env:PROCESSOR_ARCHITECTURE) {
    'AMD64' { 'amd64' }
    'ARM64' { 'arm64' }
    default { throw "Unsupported Windows architecture: $env:PROCESSOR_ARCHITECTURE" }
}

$release = Invoke-RestMethod -Headers @{ 'User-Agent' = 'img2ssh-installer' } `
    -Uri "https://api.github.com/repos/$repository/releases/latest"
$assetName = "img2ssh_windows_${architecture}.zip"
$asset = $release.assets | Where-Object { $_.name -eq $assetName } | Select-Object -First 1
if (-not $asset) {
    throw "Release $($release.tag_name) does not contain $assetName"
}

$temporaryDirectory = Join-Path ([System.IO.Path]::GetTempPath()) ([guid]::NewGuid().ToString())
$archivePath = Join-Path $temporaryDirectory $assetName
$installDirectory = Join-Path $env:LOCALAPPDATA 'Programs\img2ssh'
try {
    New-Item -ItemType Directory -Path $temporaryDirectory -Force | Out-Null
    Invoke-WebRequest -Uri $asset.browser_download_url -OutFile $archivePath
    Expand-Archive -LiteralPath $archivePath -DestinationPath $temporaryDirectory -Force

    # Stop the running daemon and wait for Windows to release the executable.
    schtasks /End /TN 'img2ssh' 2>$null | Out-Null
    $runningDaemon = Get-Process -Name 'img2ssh' -ErrorAction SilentlyContinue
    if ($runningDaemon) {
        $runningDaemon | Stop-Process -Force
        Start-Sleep -Milliseconds 500
    }

    $binary = Get-ChildItem -LiteralPath $temporaryDirectory -Filter 'img2ssh.exe' -Recurse | Select-Object -First 1
    if (-not $binary) {
        throw 'img2ssh.exe was not found in the downloaded archive.'
    }

    New-Item -ItemType Directory -Path $installDirectory -Force | Out-Null
    $installedBinary = Join-Path $installDirectory 'img2ssh.exe'
    $copied = $false
    for ($attempt = 1; $attempt -le 10; $attempt++) {
        try {
            Copy-Item -LiteralPath $binary.FullName -Destination $installedBinary -Force
            $copied = $true
            break
        } catch {
            if ($attempt -eq 10) { throw }
            Start-Sleep -Milliseconds 500
        }
    }
    if (-not $copied) {
        throw 'Could not replace the running img2ssh.exe.'
    }

    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $pathEntries = @($userPath -split ';' | Where-Object { $_ })
    if (-not ($pathEntries | Where-Object { $_.TrimEnd('\') -ieq $installDirectory.TrimEnd('\') })) {
        $pathEntries += $installDirectory
        [Environment]::SetEnvironmentVariable('Path', ($pathEntries -join ';'), 'User')
    }
    $env:Path = "$installDirectory;$env:Path"

    Write-Host "img2ssh $($release.tag_name) installed to $installDirectory"
    Write-Host 'Run: img2ssh setup'
} finally {
    Remove-Item -LiteralPath $temporaryDirectory -Recurse -Force -ErrorAction SilentlyContinue
}
