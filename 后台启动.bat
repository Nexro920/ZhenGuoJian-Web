@echo off
chcp 65001 >nul
setlocal
cd /d "%~dp0"

set "EXE_PATH=%~dp0zhenguojian-web.exe"
if exist "%EXE_PATH%" goto :launch

echo 未找到 zhenguojian-web.exe，正在自动编译...
call :find_go
if errorlevel 1 (
    echo [错误] 未检测到 Go 编译环境，请先安装 Go 1.24+ 或运行重新编译脚本。
    pause
    exit /b 1
)

set "APP_VER=1.0.13+113"
if exist "%~dp0VERSION" set /p APP_VER=<"%~dp0VERSION"

set "CGO_ENABLED=0"
if "%GOPROXY%"=="" set "GOPROXY=https://goproxy.cn,direct"
"%GO_BIN%" build -trimpath -ldflags="-s -w -X main.appVersion=%APP_VER%" -o "%EXE_PATH%" ./web
if errorlevel 1 (
    echo [错误] 编译失败，请检查上方错误信息。
    pause
    exit /b 1
)

:launch
set "ZGJ_DIR=%~dp0"
if "%ZGJ_DIR:~-1%"=="\" set "ZGJ_DIR=%ZGJ_DIR:~0,-1%"
set "ZGJ_ARGS=%*"
powershell -NoProfile -ExecutionPolicy Bypass -Command "$dir = $env:ZGJ_DIR; $exe = Join-Path $dir 'zhenguojian-web.exe'; if ([string]::IsNullOrWhiteSpace($env:ZGJ_ARGS)) { Start-Process -FilePath $exe -WorkingDirectory $dir -WindowStyle Hidden } else { Start-Process -FilePath $exe -ArgumentList $env:ZGJ_ARGS -WorkingDirectory $dir -WindowStyle Hidden }"
if errorlevel 1 (
    echo [错误] 后台启动失败。
    pause
    exit /b 1
)
echo 已在后台静默启动 真果鉴 网页版，可使用“停止服务.bat”结束进程。
exit /b 0

:find_go
set "GO_BIN="
for /f "delims=" %%i in ('where go 2^>nul') do (
    set "GO_BIN=%%i"
    goto :eof
)
if exist "%LOCALAPPDATA%\Programs\Go\bin\go.exe" (
    set "GO_BIN=%LOCALAPPDATA%\Programs\Go\bin\go.exe"
    goto :eof
)
if exist "C:\Program Files\Go\bin\go.exe" (
    set "GO_BIN=C:\Program Files\Go\bin\go.exe"
    goto :eof
)
exit /b 1
