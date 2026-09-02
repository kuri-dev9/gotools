@echo off
setlocal

cd /d "%~dp0"

echo ========================================
echo  gtools Windows Build
echo ========================================
echo.

where go >nul 2>&1
if errorlevel 1 (
    echo [ERROR] Go was not found in PATH.
    exit /b 1
)

if not exist bin mkdir bin

set CGO_ENABLED=0
set GOOS=windows
set GOARCH=amd64

set GIT_COMMIT=nogit
for /f %%i in ('git rev-parse --short HEAD 2^>nul') do set GIT_COMMIT=%%i
set BUILD_DATE=%DATE% %TIME%

echo [1/3] Building gb64.exe...
go build -mod=vendor -ldflags "-s -w -X 'gtools/pkg/version.GitCommit=%GIT_COMMIT%' -X 'gtools/pkg/version.BuildDate=%BUILD_DATE%'" -o bin\gb64.exe .\cmd\gb64
if errorlevel 1 (
    echo [ERROR] gb64.exe build failed.
    exit /b 1
)
echo [OK] bin\gb64.exe
echo.

echo [2/3] Building gcurl.exe...
go build -mod=vendor -ldflags "-s -w -X 'gtools/pkg/version.GitCommit=%GIT_COMMIT%' -X 'gtools/pkg/version.BuildDate=%BUILD_DATE%'" -o bin\gcurl.exe .\cmd\gcurl
if errorlevel 1 (
    echo [ERROR] gcurl.exe build failed.
    exit /b 1
)
echo [OK] bin\gcurl.exe
echo.

echo [3/3] Building B64Drop.exe...
go build -mod=vendor -ldflags "-s -w -H=windowsgui" -o bin\B64Drop.exe .\windows\B64Drop\app
if errorlevel 1 (
    echo [ERROR] B64Drop.exe build failed.
    exit /b 1
)
echo [OK] bin\B64Drop.exe
echo.

echo ========================================
echo  Windows build completed
echo ========================================
echo.
echo   bin\gb64.exe
echo   bin\gcurl.exe
echo   bin\B64Drop.exe
echo.

endlocal
exit /b 0
