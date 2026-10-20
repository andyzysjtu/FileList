$ErrorActionPreference = 'Stop'
Push-Location (Join-Path $PSScriptRoot '..')
$PreviousCGO = $env:CGO_ENABLED
$PreviousOS = $env:GOOS
$PreviousArch = $env:GOARCH
try {
    New-Item -ItemType Directory -Force dist | Out-Null
    $env:CGO_ENABLED = '0'
    foreach ($Target in @('windows/amd64', 'windows/arm64', 'darwin/amd64', 'darwin/arm64', 'linux/amd64', 'linux/arm64')) {
        $env:GOOS, $env:GOARCH = $Target.Split('/')
        $Suffix = if ($env:GOOS -eq 'windows') { '.exe' } else { '' }
        $Output = "dist/filelist-$($env:GOOS)-$($env:GOARCH)$Suffix"
        Write-Host "Building $Output"
        go build -trimpath -o $Output ./cmd/filelist
        if ($LASTEXITCODE -ne 0) { throw "Build failed: $Target" }
    }
}
finally {
    $env:CGO_ENABLED = $PreviousCGO
    $env:GOOS = $PreviousOS
    $env:GOARCH = $PreviousArch
    Pop-Location
}
