param(
    [string]$Executable = "$PSScriptRoot\..\build\DumpingGround.exe",
    [int]$Seconds = 60,
    [string]$Renderer = "1",
    [string]$Output = "$PSScriptRoot\..\build\memory.json"
)
$ErrorActionPreference = 'Stop'
$env:MYGO_GPU = $Renderer
$app = Start-Process -FilePath (Resolve-Path $Executable) -PassThru
$samples = @()
try {
    $started = Get-Date
    while (((Get-Date) - $started).TotalSeconds -lt $Seconds) {
        Start-Sleep -Seconds 2
        $app.Refresh()
        if ($app.HasExited) { throw "Application exited during sampling" }
        $proc = Get-Process -Id $app.Id
        $samples += [pscustomobject]@{
            seconds = [math]::Round(((Get-Date)-$started).TotalSeconds, 2)
            privateBytes = $proc.PrivateMemorySize64
            workingSet = $proc.WorkingSet64
            cpuSeconds = $proc.TotalProcessorTime.TotalSeconds
            handles = $proc.HandleCount
            threads = $proc.Threads.Count
        }
    }
    $samples | ConvertTo-Json | Set-Content -Path $Output -Encoding UTF8
} finally {
    if (!$app.HasExited) { $app.CloseMainWindow() | Out-Null }
}
