param(
  [Parameter(Mandatory=$true)][string]$Source,
  [Parameter(Mandatory=$true)][string]$OutputDirectory,
  [Parameter(Mandatory=$true)][string]$Name
)
$ErrorActionPreference = 'Stop'
if ($Name -notmatch '^[A-Za-z0-9_-]+$') { throw 'Name must be a simple build name without path separators' }
$sourcePath = (Resolve-Path -LiteralPath $Source).Path
$outputPath = [System.IO.Path]::GetFullPath($OutputDirectory)
if ($outputPath -notlike '*wendao-site-check-*') { throw 'OutputDirectory must be a new explicit wendao-site-check- directory' }
New-Item -ItemType Directory -Path $outputPath -Force | Out-Null
$mainPath = Join-Path $sourcePath 'main.go'
$original = Get-Content -LiteralPath $mainPath -Raw -Encoding UTF8
$listen = 'Addr:    ":" + port,'
$signal = 'signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)'
if (-not $original.Contains($listen) -or -not $original.Contains($signal)) { throw 'Unexpected main.go layout; inspect before creating the test overlay' }
$stopHook = 'go func() { for { if path := os.Getenv("WENDAO_CHECK_STOP_FILE"); path != "" { if _, err := os.Stat(path); err == nil { quit <- syscall.SIGTERM; return } }; time.Sleep(100 * time.Millisecond) } }()'
$patched = $original.Replace($listen, 'Addr:    "127.0.0.1:" + port,').Replace($signal, $signal + "`n" + $stopHook)
$overlayMain = Join-Path $outputPath ($Name + '-main.go')
$overlayJSON = Join-Path $outputPath ($Name + '-overlay.json')
if ((Test-Path -LiteralPath $overlayMain) -or (Test-Path -LiteralPath $overlayJSON) -or (Test-Path -LiteralPath (Join-Path $outputPath ($Name + '.exe')))) { throw 'Build outputs already exist; choose another Name or directory' }
Set-Content -LiteralPath $overlayMain -Value $patched -Encoding UTF8
@{ Replace = @{ $mainPath = $overlayMain } } | ConvertTo-Json -Depth 4 | Set-Content -LiteralPath $overlayJSON -Encoding UTF8
Push-Location -LiteralPath $sourcePath
try {
  go build -overlay $overlayJSON -o (Join-Path $outputPath ($Name + '.exe')) .
  if ($LASTEXITCODE -ne 0) { throw 'Site fixture build failed' }
} finally { Pop-Location }
