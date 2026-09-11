<#
.SYNOPSIS
Copy the bundled runtimes into the build output directories.

.DESCRIPTION
Invoked by the windows/amd64 postBuildHook in wails.json:

    powershell -NoProfile -ExecutionPolicy Bypass -File scripts/copy-runtimes.ps1 ${bin}

Note: path is relative to the project root (wails executes postBuildHooks with cwd = project dir).
Never hardcode an absolute path here — CI runs the same build from a different directory.

${bin} is the absolute path of the compiled executable (build\bin\WorkBaby.exe).

The project level runtimes/ directory (manifest.json + one archive per asset) is
copied to two generated locations, both ignored by git:

    1. <binDir>\runtimes            -> picked up by internal/runtime.Manager.LocateBundledDir()
                                       when WorkBaby.exe is launched from build/bin
    2. <buildDir>\windows\runtimes  -> input of `File /r "..\runtimes"` in build/windows/installer/project.nsi

Every archive declared in manifest.json must exist; otherwise the build fails fast
instead of silently producing an installer with missing runtimes.
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$BinPath
)

$ErrorActionPreference = 'Stop'

$projectRoot = Split-Path -Parent $PSScriptRoot
$sourceDir = Join-Path $projectRoot 'runtimes'
$manifestFile = Join-Path $sourceDir 'manifest.json'

if (-not (Test-Path -LiteralPath $manifestFile -PathType Leaf)) {
    throw "[copy-runtimes] manifest.json not found: $manifestFile"
}

$manifest = Get-Content -LiteralPath $manifestFile -Raw | ConvertFrom-Json
$assets = @($manifest.assets)
if ($assets.Count -eq 0) {
    throw "[copy-runtimes] manifest.json declares no assets: $manifestFile"
}

$files = @('manifest.json')
foreach ($asset in $assets) {
    $id = $asset.id
    $archive = $asset.archiveFile
    if ([string]::IsNullOrWhiteSpace($archive)) {
        throw "[copy-runtimes] asset '$id' in manifest.json has no archiveFile"
    }
    $archivePath = Join-Path $sourceDir $archive
    if (-not (Test-Path -LiteralPath $archivePath -PathType Leaf)) {
        throw "[copy-runtimes] runtime archive missing for '$id': $archivePath"
    }
    $files += $archive
}

$binDir = [System.IO.Path]::GetFullPath((Split-Path -Parent $BinPath))
$buildDir = Split-Path -Parent $binDir
if ([string]::IsNullOrWhiteSpace($buildDir)) {
    throw "[copy-runtimes] cannot resolve build directory from: $BinPath"
}

# exe 同级目录供直接运行使用；build/windows 供 NSIS 打包使用
$targets = @(
    (Join-Path $binDir 'runtimes'),
    (Join-Path $buildDir (Join-Path 'windows' 'runtimes'))
)

foreach ($target in $targets) {
    if (Test-Path -LiteralPath $target) {
        Remove-Item -LiteralPath $target -Recurse -Force
    }
    New-Item -ItemType Directory -Path $target -Force | Out-Null

    foreach ($file in $files) {
        Copy-Item -LiteralPath (Join-Path $sourceDir $file) -Destination $target -Force
    }

    Write-Host "[copy-runtimes] $target <- $($files.Count) file(s)"
}
