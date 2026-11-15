@echo off
chcp 65001 >nul
setlocal
cd /d "%~dp0"

set "EXE_PATH=%~dp0zhenguojian-web.exe"
set "GO_BIN="
for /f "delims=" %%i in ('where go 2^>nul') do (
    set "GO_BIN=%%i"
    goto :got_go
)
if exist "%LOCALAPPDATA%\Programs\Go\bin\go.exe" set "GO_BIN=%LOCALAPPDATA%\Programs\Go\bin\go.exe"
if "%GO_BIN%"=="" if exist "C:\Program Files\Go\bin\go.exe" set "GO_BIN=C:\Program Files\Go\bin\go.exe"

:got_go
if "%GO_BIN%"=="" (
    echo [错误] 未检测到 Go 编译环境，请先安装 Go 1.24+。
    pause
    exit /b 1
)

set "APP_VER=1.0.13+113"
if exist "%~dp0VERSION" set /p APP_VER=<"%~dp0VERSION"

echo 正在编译真果鉴网页版 (%APP_VER%)...
set "CGO_ENABLED=0"
if "%GOPROXY%"=="" set "GOPROXY=https://goproxy.cn,direct"
"%GO_BIN%" build -trimpath -ldflags="-s -w -X main.appVersion=%APP_VER%" -o "%EXE_PATH%" ./web
if errorlevel 1 (
    echo [错误] 编译失败。
    pause
    exit /b 1
)

echo 编译完成，正在启动...
"%EXE_PATH%" %*
if errorlevel 1 (
    echo.
    echo 程序异常退出（退出码 %ERRORLEVEL%）。
    pause
)
exit /b %ERRORLEVEL%
