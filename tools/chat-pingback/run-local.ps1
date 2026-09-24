param(
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^[A-Za-z0-9._:-]{1,200}$')]
    [string]$ConversationId,
    [string]$BaseUrl = 'http://127.0.0.1:8888',
    [string]$AgentId = 'codex.pingback.e2e',
    [string]$Tenant = 'harborcare-demo',
    [switch]$Background,
    [switch]$Supervisor,
    [switch]$Stop
)

$ErrorActionPreference = 'Stop'
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
$stateDir = Join-Path $repoRoot '.artifacts\chat-pingback'
$stateFile = Join-Path $stateDir "$AgentId-$ConversationId.json"
$pidFile = Join-Path $stateDir "$AgentId-$ConversationId.supervisor.pid"
$logDir = Join-Path $stateDir 'logs'
$supervisorLog = Join-Path $stateDir "$AgentId-$ConversationId.supervisor.log"
$supervisorRedirectOutputLog = Join-Path $stateDir "$AgentId-$ConversationId.supervisor.stdout.log"
$supervisorErrorLog = Join-Path $stateDir "$AgentId-$ConversationId.supervisor.err.log"
$supervisorRedirectErrorLog = Join-Path $stateDir "$AgentId-$ConversationId.supervisor.stderr.log"

if ($BaseUrl -cne 'http://127.0.0.1:8888') {
    throw 'The local pingback supervisor only targets the existing 127.0.0.1:8888 preview.'
}

New-Item -ItemType Directory -Force -Path $stateDir, $logDir | Out-Null

if ($Stop) {
    if (Test-Path $pidFile) {
        $savedPid = [int](Get-Content -Raw $pidFile)
        $existing = Get-CimInstance Win32_Process -Filter "ProcessId = $savedPid" -ErrorAction SilentlyContinue
        $expectedScript = [IO.Path]::GetFullPath($PSCommandPath)
        $expectedId = '"' + $ConversationId + '"'
        if (-not $existing -or -not $existing.CommandLine.Contains($expectedScript) -or
                -not $existing.CommandLine.Contains($expectedId) -or -not $existing.CommandLine.Contains('-Supervisor')) {
            throw "Refusing to stop PID $savedPid because it is not this conversation's pingback supervisor."
        }
        Remove-Item -LiteralPath $pidFile -Force
        & taskkill.exe /PID $savedPid /T /F | Out-Null
    }
    exit 0
}

if (-not $Supervisor) {
    if (Test-Path $pidFile) {
        $savedPid = [int](Get-Content -Raw $pidFile)
        $existing = Get-CimInstance Win32_Process -Filter "ProcessId = $savedPid" -ErrorAction SilentlyContinue
        if ($existing -and $existing.CommandLine.Contains('run-local.ps1')) {
            "Pingback supervisor already running: PID $savedPid"
            exit 0
        }
        Remove-Item -LiteralPath $pidFile -Force
    }

    if ($Background) {
        $arguments = @(
            '-NoLogo', '-NoProfile', '-ExecutionPolicy', 'RemoteSigned', '-File', "`"$PSCommandPath`"",
            '-ConversationId', "`"$ConversationId`"", '-BaseUrl', "`"$BaseUrl`"",
            '-AgentId', "`"$AgentId`"", '-Tenant', "`"$Tenant`"", '-Supervisor'
        )
        $process = Start-Process -FilePath 'powershell.exe' -ArgumentList $arguments -WorkingDirectory $repoRoot `
            -WindowStyle Hidden -RedirectStandardOutput $supervisorRedirectOutputLog -RedirectStandardError $supervisorRedirectErrorLog -PassThru
        Set-Content -LiteralPath $pidFile -Value $process.Id -NoNewline
        "Started pingback supervisor: PID $($process.Id); conversation $ConversationId"
        exit 0
    }

    $Supervisor = $true
    $supervisorPid = $PID
    Set-Content -LiteralPath $pidFile -Value $supervisorPid -NoNewline
} else {
    $supervisorPid = $PID
    Set-Content -LiteralPath $pidFile -Value $supervisorPid -NoNewline
}

Set-Location $repoRoot
$env:HCMNEXT_DEV_HMAC_KEY = 'hcm-next-local-dev-profile-key-only'
$env:HCM_CHAT_AGENT_ID = $AgentId
$refreshAfter = [TimeSpan]::FromMinutes(10)
$tokenIssuer = Join-Path $repoRoot '.artifacts\bin\hcmnext-chat-agent-token.exe'
$tokenSource = Join-Path $repoRoot 'cmd\hcmnext\token.go'

try {
    while ($true) {
        try {
            $issuerNeedsBuild = -not (Test-Path -LiteralPath $tokenIssuer)
            if (-not $issuerNeedsBuild) {
                $issuerNeedsBuild = (Get-Item -LiteralPath $tokenSource).LastWriteTimeUtc -gt (Get-Item -LiteralPath $tokenIssuer).LastWriteTimeUtc
            }
            if ($issuerNeedsBuild) {
                & go build -o $tokenIssuer ./cmd/hcmnext 2>> $supervisorErrorLog
                if ($LASTEXITCODE -ne 0) {
                    throw 'local identity-only token issuer could not be built'
                }
            }
            $token = (& $tokenIssuer token -tenant $Tenant -subject $AgentId -client-id $AgentId -subject-kind agent -identity-only -ttl 15m 2>> $supervisorErrorLog)
            if ($LASTEXITCODE -ne 0 -or -not $token) {
                throw 'local development agent token could not be issued'
            }
            $env:HCM_CHAT_TOKEN = $token.Trim()

            $stamp = Get-Date -Format 'yyyyMMdd-HHmmss'
            $pythonPath = (Get-Command python -ErrorAction Stop).Source
            $agentArgs = @(
                'tools/chat-pingback/agent.py', '--base-url', $BaseUrl,
                '--conversation', $ConversationId, '--state-file', $stateFile, '--wait-ms', '5000'
            )
            $agent = Start-Process -FilePath $pythonPath -ArgumentList $agentArgs -WorkingDirectory $repoRoot `
                -WindowStyle Hidden -RedirectStandardOutput (Join-Path $logDir "$AgentId-$stamp.stdout.log") `
                -RedirectStandardError (Join-Path $logDir "$AgentId-$stamp.stderr.log") -PassThru
            Add-Content -LiteralPath $supervisorLog -Value "$(Get-Date -Format o) started listener PID $($agent.Id)"

            if (-not $agent.WaitForExit([int]$refreshAfter.TotalMilliseconds)) {
                Stop-Process -Id $agent.Id -Force -ErrorAction SilentlyContinue
                Add-Content -LiteralPath $supervisorLog -Value "$(Get-Date -Format o) rotated listener token"
            } else {
                Add-Content -LiteralPath $supervisorLog -Value "$(Get-Date -Format o) listener exited with code $($agent.ExitCode); restarting"
            }
        } catch {
            Add-Content -LiteralPath $supervisorErrorLog -Value "$(Get-Date -Format o) $($_.Exception.Message)"
        } finally {
            Remove-Item Env:\HCM_CHAT_TOKEN -ErrorAction SilentlyContinue
        }
        Start-Sleep -Seconds 2
    }
} finally {
    Remove-Item Env:\HCM_CHAT_TOKEN, Env:\HCM_CHAT_AGENT_ID, Env:\HCMNEXT_DEV_HMAC_KEY -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $pidFile -Force -ErrorAction SilentlyContinue
}
