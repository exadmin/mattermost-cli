@echo off
setlocal
pushd "%~dp0" || exit /b 1
go build -o mmcli.exe ./cmd/mmcli
set "build_exit_code=%errorlevel%"
popd
exit /b %build_exit_code%
