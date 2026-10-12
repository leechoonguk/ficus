# Install ficus on Windows from a GitHub release and enable it in PowerShell:
#
#   irm https://github.com/leechoonguk/ficus/releases/latest/download/install.ps1 | iex
#
# Environment:
#   FICUS_VERSION      release tag to install (default: the latest)
#   FICUS_INSTALL_DIR  where the binaries go (default: %LOCALAPPDATA%\Programs\ficus)
#   FICUS_NO_SETUP=1   only install the binaries; run `ficus setup` yourself
#
# Uninstall: ficus setup --remove; then delete the install directory.
& {
    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'SilentlyContinue'   # Invoke-WebRequest is slow with it
    # Windows PowerShell 5.1 may default to TLS 1.0, which GitHub refuses.
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

    $repo = 'leechoonguk/ficus'
    $base = if ($env:FICUS_VERSION) { "https://github.com/$repo/releases/download/$env:FICUS_VERSION" }
            else { "https://github.com/$repo/releases/latest/download" }
    $arch = switch ($env:PROCESSOR_ARCHITECTURE) {
        'AMD64' { 'amd64' }
        'ARM64' { 'arm64' }
        default { throw "ficus: unsupported CPU $env:PROCESSOR_ARCHITECTURE" }
    }
    $dir = if ($env:FICUS_INSTALL_DIR) { $env:FICUS_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'Programs\ficus' }
    $zip = "ficus_windows_$arch.zip"

    $tmp = Join-Path ([IO.Path]::GetTempPath()) ("ficus-" + [guid]::NewGuid())
    New-Item -ItemType Directory -Path $tmp | Out-Null
    try {
        Write-Host "ficus: downloading $base/$zip"
        Invoke-WebRequest -UseBasicParsing -Uri "$base/$zip" -OutFile (Join-Path $tmp $zip)
        Invoke-WebRequest -UseBasicParsing -Uri "$base/checksums.txt" -OutFile (Join-Path $tmp 'checksums.txt')

        $want = Get-Content (Join-Path $tmp 'checksums.txt') |
            ForEach-Object { $f = $_ -split '\s+'; if ($f[1] -eq $zip) { $f[0] } } | Select-Object -First 1
        if (-not $want) { throw "ficus: $zip is not listed in checksums.txt" }
        $got = (Get-FileHash -Algorithm SHA256 -LiteralPath (Join-Path $tmp $zip)).Hash.ToLowerInvariant()
        if ($got -ne $want) { throw "ficus: checksum mismatch for $zip" }

        Expand-Archive -LiteralPath (Join-Path $tmp $zip) -DestinationPath (Join-Path $tmp 'x') -Force
        New-Item -ItemType Directory -Force -Path $dir | Out-Null
        foreach ($f in 'ficus.exe', 'ficus-carapace.exe') {
            Copy-Item -LiteralPath (Join-Path $tmp "x\$f") -Destination (Join-Path $dir $f) -Force
        }
        $ficus = Join-Path $dir 'ficus.exe'
        Write-Host "ficus: installed $(& $ficus version) in $dir"

        # On PATH for `ficus doctor` and friends; the profile line uses the
        # full path and does not need it.
        $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
        if (-not (($userPath -split ';') -contains $dir)) {
            [Environment]::SetEnvironmentVariable('Path', ((@($userPath, $dir) | Where-Object { $_ }) -join ';'), 'User')
            Write-Host "ficus: added $dir to your PATH (new terminals)"
        }

        if ($env:FICUS_NO_SETUP -eq '1') {
            Write-Host "ficus: run '$ficus setup' to enable it in PowerShell"
        } else {
            & $ficus setup
            if ($LASTEXITCODE -ne 0) { throw "ficus: setup failed" }
        }
    } finally {
        Remove-Item -Recurse -Force -LiteralPath $tmp -ErrorAction SilentlyContinue
    }
}
