param([string]$Volume = 'C:\',[int]$TimeoutSeconds = 600)
$ErrorActionPreference='Stop'
$root=Split-Path -Parent $PSScriptRoot
$exe=Join-Path $root 'build\DumpingGround-verify.exe'
if(!(Test-Path $exe)){throw "Build $exe first"}
$admin=([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
Write-Host "Volume: $Volume"
Write-Host "Elevated: $admin"
Write-Host 'Strict MFT mode: failure or access denial is INCONCLUSIVE; directory fallback is never reported as PASS.'
& $exe -volume $Volume -timeout ("{0}s" -f $TimeoutSeconds)
exit $LASTEXITCODE
