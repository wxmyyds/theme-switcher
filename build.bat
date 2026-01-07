@echo off
chcp 65001 > nul
title Windows Theme Switcher 构建脚本
echo ==============================================
echo          Windows 主题切换器 - 构建脚本
echo ==============================================
echo.

REM 清理旧的构建文件（避免缓存干扰）
echo [1/3] 清理旧文件...
if exist theme-switcher.exe (
    del /f /q theme-switcher.exe
    echo   ✅ 已删除旧程序文件 theme-switcher.exe
) else (
    echo   ℹ️  未检测到旧程序文件
)

if exist theme_switcher.log (
    del /f /q theme_switcher.log
    echo   ✅ 已删除旧日志文件 theme_switcher.log
) else (
    echo   ℹ️  未检测到旧日志文件
)
echo.

REM 编译 Go 程序为 Windows GUI 可执行文件
echo [2/3] 开始编译程序...
go build -ldflags="-H windowsgui -w -s" -o theme-switcher.exe

REM 检查编译是否成功
if errorlevel 1 (
    echo.
    echo ❌ 编译失败！请检查：
    echo   1. 是否安装了 Go 环境（go version 可验证）
    echo   2. 代码是否有语法错误
    echo   3. 是否在代码所在目录运行此脚本
    echo.
    pause
    exit /b 1
)

echo   ✅ 编译成功！生成文件：theme-switcher.exe
echo.

REM 输出运行提示
echo [3/3] 构建完成！
echo ==============================================
echo 📌 运行说明：
echo   1. 右键 theme-switcher.exe → 以管理员身份运行
echo   2. 程序后台运行，无控制台窗口
echo   3. 运行日志会保存在 theme_switcher.log 文件中
echo ==============================================
echo.
pause