@echo off
chcp 65001 >nul
setlocal
cd /d "%~dp0"

set "EXE_PATH=%~dp0zhenguojian-web.exe"

if exist "%EXE_PATH%" (
    "%EXE_PATH%" %*
    goto :done
)

echo 未找到 zhenguojian-web.exe，正在自动编译...
call :find_go
if errorlevel 1 (
    echo [错误] 未检测到 Go 编译环境，请先安装 Go 1.24+ 或运行重新编译脚本。
    pause
    exit /b 1
)

set "CGO_ENABLED=0"
if "%GOPROXY%"=="" set "GOPROXY=https://goproxy.cn,direct"
"%GO_BIN%" build -trimpath -ldflags="-s -w -X duanjuapp/native/core.buildAllSources=true -X main.appVersion=1.0.13+113" -o "%EXE_PATH%" ./web
if errorlevel 1 (
    echo [错误] 编译失败，请检查上方错误信息。
    pause
    exit /b 1
)

"%EXE_PATH%" %*

:done
if errorlevel 1 (
    echo.
    echo 程序异常退出（退出码 %ERRORLEVEL%）。
    pause
)
exit /b %ERRORLEVEL%

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
