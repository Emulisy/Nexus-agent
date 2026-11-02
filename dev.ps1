param(
    [ValidateSet('start', 'stop', 'status')][string]$Action = 'start',
    [ValidateRange(1024, 65535)][int]$ApiPort = 8080,
    [ValidateRange(1024, 65535)][int]$FrontendPort = 5173,
    [switch]$Search,
    [switch]$KeepDependencies
)

$ErrorActionPreference = 'Stop'
$projectRoot = $PSScriptRoot
$backendDir = Join-Path $projectRoot 'backend'
$frontendDir = Join-Path $projectRoot 'frontend'
$runtimeDir = Join-Path $projectRoot '.local'
$stateFile = Join-Path $runtimeDir 'processes.json'
$logDir = Join-Path $runtimeDir 'logs'
$binDir = Join-Path $runtimeDir 'bin'
$ownedProcesses = @()

function Read-State {
    if (Test-Path -LiteralPath $stateFile) {
        return Get-Content -LiteralPath $stateFile -Raw | ConvertFrom-Json
    }
    return $null
}

# Check both executable identity and creation time before using a saved PID.
function Get-OwnedProcess($record) {
    $expectedExecutable = switch ($record.name) {
        'api' { Join-Path $binDir 'api.exe' }
        'worker' { Join-Path $binDir 'worker.exe' }
        'frontend' { (Get-Command node -ErrorAction SilentlyContinue).Source }
        default { $null }
    }
    if (-not $expectedExecutable -or $record.executable -ne $expectedExecutable) { return $null }
    $candidate = Get-Process -Id $record.id -ErrorAction SilentlyContinue
    if ($candidate -and $candidate.Path -eq $record.executable -and
        $candidate.StartTime.ToUniversalTime().ToString('o') -eq $record.started) {
        return $candidate
    }
    return $null
}

function Stop-OwnedProcesses($records) {
    foreach ($record in @($records)) {
        if ($null -eq $record) { continue }
        $candidate = Get-OwnedProcess $record
        if ($candidate) {
            Stop-Process -Id $candidate.Id -Force -ErrorAction Stop
            $candidate.WaitForExit()
            Write-Host "Stopped $($record.name)."
        }
    }
}

function Invoke-Checked([string]$command, [string[]]$arguments, [string]$directory) {
    Push-Location -LiteralPath $directory
    try {
        & $command @arguments
        if ($LASTEXITCODE -ne 0) { throw "$command failed (exit $LASTEXITCODE)." }
    } finally { Pop-Location }
}

function Test-Port([int]$port) {
    $client = New-Object System.Net.Sockets.TcpClient
    try {
        $connect = $client.ConnectAsync('127.0.0.1', $port)
        return $connect.Wait(500) -and $client.Connected
    } catch { return $false } finally { $client.Dispose() }
}

function Test-DockerReady {
    $previousPreference = $ErrorActionPreference
    try {
        # Windows PowerShell treats redirected native stderr as error records.
        $ErrorActionPreference = 'Continue'
        & docker info --format '{{.ServerVersion}}' 2>$null | Out-Null
        return $LASTEXITCODE -eq 0
    } finally { $ErrorActionPreference = $previousPreference }
}

function Wait-Endpoint([string]$url, $process, [string]$name) {
    $deadline = [DateTime]::UtcNow.AddSeconds(90)
    while ([DateTime]::UtcNow -lt $deadline) {
        if ($process.HasExited) { throw "$name exited. See $logDir." }
        try {
            $response = Invoke-WebRequest -Uri $url -UseBasicParsing -TimeoutSec 2
            if ($response.StatusCode -eq 200) { return }
        } catch { }
        Start-Sleep -Milliseconds 500
    }
    throw "$name did not become ready. See $logDir."
}

function Start-LocalProcess([string]$name, [string]$executable, [string[]]$arguments, [string]$directory) {
    $options = @{
        FilePath = $executable
        WorkingDirectory = $directory
        WindowStyle = 'Hidden'
        PassThru = $true
        RedirectStandardOutput = (Join-Path $logDir "$name.log")
        RedirectStandardError = (Join-Path $logDir "$name.error.log")
    }
    if ($arguments.Count -gt 0) { $options.ArgumentList = $arguments }
    $child = Start-Process @options
    $record = [PSCustomObject]@{
        name = $name
        id = $child.Id
        executable = $executable
        started = $child.StartTime.ToUniversalTime().ToString('o')
    }
    $script:ownedProcesses += $record
    @{ apiPort = $ApiPort; frontendPort = $FrontendPort; processes = @($script:ownedProcesses) } |
        ConvertTo-Json -Depth 4 | Set-Content -LiteralPath $stateFile -Encoding UTF8
    return $child
}

New-Item -ItemType Directory -Path $runtimeDir -Force | Out-Null
$launcherLock = $null
try {
    try {
        $launcherLock = [System.IO.File]::Open((Join-Path $runtimeDir 'launcher.lock'), 'OpenOrCreate', 'ReadWrite', 'None')
    } catch { throw 'Another launcher command is running. Wait for it to finish.' }
    $state = Read-State
    if ($Action -eq 'status') {
        if (-not $state) { Write-Host 'SupaAgent is stopped.'; exit 0 }
        foreach ($record in $state.processes) {
            $status = if (Get-OwnedProcess $record) { 'running' } else { 'stopped' }
            Write-Host "$($record.name): $status"
        }
        Write-Host "Workspace: http://localhost:$($state.frontendPort)"
        exit 0
    }
    if ($Action -eq 'stop') {
        if ($state) { Stop-OwnedProcesses $state.processes }
        Remove-Item -LiteralPath $stateFile -Force -ErrorAction SilentlyContinue
        if (-not $KeepDependencies) {
            Invoke-Checked 'docker' @('compose', '--profile', 'search', 'stop') $backendDir
        }
        Write-Host 'SupaAgent stopped. Your data is preserved.'
        exit 0
    }
    if ($state) {
        $running = @($state.processes | Where-Object { Get-OwnedProcess $_ })
        if ($running.Count -eq 3) {
            Write-Host "SupaAgent is already running: http://localhost:$($state.frontendPort)"
            Write-Host 'Use stop before changing ports or enabling search.'
            exit 0
        }
        Stop-OwnedProcesses $state.processes
        Remove-Item -LiteralPath $stateFile -Force
    }
    foreach ($command in @('go', 'node', 'npm.cmd', 'docker')) {
        if (-not (Get-Command $command -ErrorAction SilentlyContinue)) { throw "Install $command and make it available on PATH." }
    }
    if ($ApiPort -eq $FrontendPort) { throw 'API and frontend ports must be different.' }
    foreach ($port in @($ApiPort, $FrontendPort)) {
        if (Test-Port $port) { throw "Port $port is already in use. Stop that server or choose another port." }
    }
    New-Item -ItemType Directory -Path $logDir, $binDir -Force | Out-Null
    $envFile = Join-Path $backendDir '.env'
    if (-not (Test-Path -LiteralPath $envFile)) {
        Copy-Item -LiteralPath (Join-Path $backendDir '.env.example') -Destination $envFile
        Write-Host 'Created backend/.env. Add your model API key before chatting.'
    }

    Write-Host '[1/5] Starting Docker dependencies...'
    if (-not (Test-DockerReady)) {
        $dockerDesktop = Join-Path $env:ProgramFiles 'Docker\Docker\Docker Desktop.exe'
        if (-not (Test-Path -LiteralPath $dockerDesktop)) { throw 'Start Docker Desktop with Linux containers, then run this command again.' }
        Start-Process -FilePath $dockerDesktop -WindowStyle Hidden | Out-Null
        Write-Host 'Waiting for Docker Desktop...'
        $deadline = [DateTime]::UtcNow.AddSeconds(150)
        do {
            Start-Sleep -Seconds 2
            $dockerReady = Test-DockerReady
        } while (-not $dockerReady -and [DateTime]::UtcNow -lt $deadline)
        if (-not $dockerReady) { throw 'Docker Desktop is not ready. Start it, then try again.' }
    }
    $composeArgs = @('compose')
    if ($Search) { $composeArgs += @('--profile', 'search') }
    $composeArgs += @('up', '-d', '--wait', '--wait-timeout', '180')
    Invoke-Checked 'docker' $composeArgs $backendDir

    Write-Host '[2/5] Preparing application binaries...'
    foreach ($entry in @(@('api', 'server'), @('worker', 'worker'), @('migrate', 'migrate'))) {
        Invoke-Checked 'go' @('build', '-o', (Join-Path $binDir "$($entry[0]).exe"), "./cmd/$($entry[1])") $backendDir
    }
    Write-Host '[3/5] Applying database migrations...'
    Invoke-Checked (Join-Path $binDir 'migrate.exe') @() $backendDir

    Write-Host '[4/5] Preparing frontend dependencies...'
    $lockHash = (Get-FileHash -LiteralPath (Join-Path $frontendDir 'package-lock.json') -Algorithm SHA256).Hash
    $hashFile = Join-Path $runtimeDir 'npm-lock.sha256'
    $vite = Join-Path $frontendDir 'node_modules\vite\bin\vite.js'
    if (-not (Test-Path -LiteralPath $vite) -or -not (Test-Path -LiteralPath $hashFile) -or
        (Get-Content -LiteralPath $hashFile -Raw).Trim() -ne $lockHash) {
        Invoke-Checked 'npm.cmd' @('ci', '--no-audit', '--no-fund') $frontendDir
        Set-Content -LiteralPath $hashFile -Value $lockHash
    }

    Write-Host '[5/5] Starting API, worker, and frontend...'
    $previousApiPort = $env:NX_HTTP__PORT
    $previousBackendURL = $env:NX_BACKEND_URL
    try {
        $env:NX_HTTP__PORT = [string]$ApiPort
        $api = Start-LocalProcess 'api' (Join-Path $binDir 'api.exe') @() $backendDir
        Wait-Endpoint "http://127.0.0.1:$ApiPort/healthz" $api 'API'
        $worker = Start-LocalProcess 'worker' (Join-Path $binDir 'worker.exe') @() $backendDir
        $env:NX_BACKEND_URL = "http://127.0.0.1:$ApiPort"
        # Start-Process joins arguments into a command line, so quote the absolute script path.
        $web = Start-LocalProcess 'frontend' (Get-Command node).Source @(('"' + $vite + '"'), '--host=127.0.0.1', "--port=$FrontendPort", '--strictPort') $frontendDir
        Wait-Endpoint "http://127.0.0.1:$FrontendPort/" $web 'Frontend'
        if ($worker.HasExited) { throw "Worker exited. See $logDir." }
    } finally {
        $env:NX_HTTP__PORT = $previousApiPort
        $env:NX_BACKEND_URL = $previousBackendURL
    }
    Write-Host "SupaAgent is ready: http://localhost:$FrontendPort"
    Write-Host 'Services run in the background. You may close this window.'
    Write-Host "Logs: $logDir"
    Write-Host 'Stop everything with stop.cmd or .\dev.ps1 stop.'
} catch {
    if ($ownedProcesses.Count -gt 0) {
        Stop-OwnedProcesses $ownedProcesses
        Remove-Item -LiteralPath $stateFile -Force -ErrorAction SilentlyContinue
    }
    Write-Host "Startup command failed: $($_.Exception.Message)" -ForegroundColor Red
    exit 1
} finally {
    if ($launcherLock) { $launcherLock.Dispose() }
}
