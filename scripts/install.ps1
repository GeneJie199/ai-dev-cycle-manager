[CmdletBinding()]
param(
    [ValidateSet('Install', 'Doctor', 'Uninstall', 'Purge')]
    [string]$Action = 'Install',
    [string]$Source = '.\devcycle.exe',
    [string]$Checksums = '',
    [string]$Prefix = "$env:LOCALAPPDATA\DevCycle",
    [string]$ConfirmPurge = '',
    [switch]$NoShortcut
)

$ErrorActionPreference = 'Stop'
$root = [System.IO.Path]::GetFullPath($Prefix)
$binDir = Join-Path $root 'bin'
$dataDir = Join-Path $root 'data'
$binary = Join-Path $binDir 'devcycle.exe'
$shortcut = Join-Path $env:APPDATA 'Microsoft\Windows\Start Menu\Programs\DevCycle.lnk'

switch ($Action) {
    'Install' {
        $sourcePath = [System.IO.Path]::GetFullPath($Source)
        if (-not (Test-Path -LiteralPath $sourcePath -PathType Leaf)) { throw "Binary not found: $sourcePath" }
        if ($Checksums) {
            $name = Split-Path -Leaf $sourcePath
            $entry = Get-Content -LiteralPath $Checksums | Where-Object { $_ -match ('^[0-9a-fA-F]{64}\s+\*?' + [regex]::Escape($name) + '$') } | Select-Object -First 1
            if (-not $entry) { throw "Checksum entry not found for $name" }
            $expected = ($entry -split '\s+')[0].ToLowerInvariant()
            $actual = (Get-FileHash -LiteralPath $sourcePath -Algorithm SHA256).Hash.ToLowerInvariant()
            if ($actual -ne $expected) { throw "SHA-256 verification failed for $sourcePath" }
        }
        New-Item -ItemType Directory -Force -Path $binDir, $dataDir | Out-Null
        Copy-Item -LiteralPath $sourcePath -Destination $binary -Force
        if (-not $NoShortcut) {
            $shell = New-Object -ComObject WScript.Shell
            $link = $shell.CreateShortcut($shortcut)
            $link.TargetPath = $binary
            $link.Arguments = "serve --db `"$(Join-Path $dataDir 'devcycle.db')`" --addr 127.0.0.1:8766"
            $link.WorkingDirectory = $dataDir
            $link.Description = 'Start the DevCycle workbench'
            $link.WindowStyle = 7
            $link.Save()
        }
        & $binary version
        Write-Host "Installed $binary"
        Write-Host 'Open http://127.0.0.1:8766/ after starting DevCycle.'
    }
    'Doctor' {
        if (-not (Test-Path -LiteralPath $binary -PathType Leaf)) { throw "DevCycle is not installed at $binary" }
        if (-not (Get-Command git -ErrorAction SilentlyContinue)) { throw 'Git is required but was not found.' }
        if (-not (Test-Path -LiteralPath $dataDir -PathType Container)) { throw "Data directory is missing: $dataDir" }
        & $binary version
        Write-Host 'Doctor: binary, Git, and data directory are ready.'
    }
    'Uninstall' {
        Remove-Item -LiteralPath $shortcut -Force -ErrorAction SilentlyContinue
        Remove-Item -LiteralPath $binary -Force -ErrorAction SilentlyContinue
        if ((Test-Path -LiteralPath $binDir -PathType Container) -and -not (Get-ChildItem -LiteralPath $binDir -Force | Select-Object -First 1)) { Remove-Item -LiteralPath $binDir -Force }
        Write-Host "Removed DevCycle binaries; preserved $dataDir"
    }
    'Purge' {
        if ($ConfirmPurge -ne $root) { throw "Pass -ConfirmPurge with the exact path: $root" }
        if ((Split-Path -Leaf $root) -ne 'DevCycle') { throw "Refusing to purge unexpected path: $root" }
        Remove-Item -LiteralPath $root -Recurse -Force
        Write-Host "Purged $root"
    }
}
