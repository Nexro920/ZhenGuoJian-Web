@echo off
chcp 65001 >nul
setlocal
cd /d "%~dp0"

set "ZGJ_DIR=%~dp0"
if "%ZGJ_DIR:~-1%"=="\" set "ZGJ_DIR=%ZGJ_DIR:~0,-1%"
powershell -NoProfile -ExecutionPolicy Bypass -Command "$dir = [System.IO.Path]::GetFullPath($env:ZGJ_DIR).TrimEnd('\') + '\'; $exe = [System.IO.Path]::GetFullPath((Join-Path $dir 'zhenguojian-web.exe')); $binDir = [System.IO.Path]::GetFullPath((Join-Path $dir 'bin')).TrimEnd('\') + '\'; $procs = Get-CimInstance Win32_Process | Where-Object { $_.ExecutablePath -and ($_.ExecutablePath -ieq $exe -or ($_.Name -ieq 'ffmpeg.exe' -and $_.ExecutablePath.StartsWith($binDir, [System.StringComparison]::OrdinalIgnoreCase))) }; $count = 0; foreach ($p in $procs) { Stop-Process -Id $p.ProcessId -Force -ErrorAction SilentlyContinue; $count++ }; if ($count -gt 0) { Write-Host ('已停止 ' + $count + ' 个 真果鉴 后台进程。') } else { Write-Host '未检测到正在运行的 真果鉴 后台进程。' }"
exit /b %ERRORLEVEL%
