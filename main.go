package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

// Windows API 常量
const (
	HKEY_CURRENT_USER       = 0x80000001
	KEY_SET_VALUE           = 0x0002
	KEY_WOW64_64KEY         = 0x0100
	REG_DWORD               = 0x0004
	WM_SETTINGCHANGE        = 0x001A
	HWND_BROADCAST          = 0xFFFF
	ERROR_SUCCESS           = 0
	SW_HIDE                 = 0
	SW_HIDE_CONSOLE         = 0x00000000
)

// 全局初始化Windows API Proc
var (
	modadvapi32          = syscall.NewLazyDLL("advapi32.dll")
	moduser32            = syscall.NewLazyDLL("user32.dll")
	modkernel32          = syscall.NewLazyDLL("kernel32.dll")
	
	procRegOpenKeyExW    = modadvapi32.NewProc("RegOpenKeyExW")
	procRegSetValueExW   = modadvapi32.NewProc("RegSetValueExW")
	procRegCloseKey      = modadvapi32.NewProc("RegCloseKey")
	procSendMessageW     = moduser32.NewProc("SendMessageW")
	procSendNotifyMessageW = moduser32.NewProc("SendNotifyMessageW")
	procShowWindow       = moduser32.NewProc("ShowWindow")
	procGetConsoleWindow = modkernel32.NewProc("GetConsoleWindow")
)

// 隐藏控制台窗口
func hideConsoleWindow() {
	consoleWindow, _, _ := procGetConsoleWindow.Call()
	if consoleWindow != 0 {
		procShowWindow.Call(consoleWindow, SW_HIDE)
	}
}

// 获取最后一次Windows API错误
func getLastError() error {
	modkernel32 := syscall.NewLazyDLL("kernel32.dll")
	procGetLastError := modkernel32.NewProc("GetLastError")
	ret, _, _ := procGetLastError.Call()
	return syscall.Errno(ret)
}

// 设置注册表DWORD值
func setRegistryValue(keyPath, valueName string, value uint32) error {
	keyPathPtr, err := syscall.UTF16PtrFromString(keyPath)
	if err != nil {
		return fmt.Errorf("转换路径失败: %w", err)
	}
	valueNamePtr, err := syscall.UTF16PtrFromString(valueName)
	if err != nil {
		return fmt.Errorf("转换值名称失败: %w", err)
	}

	var hKey uintptr
	ret, _, err := procRegOpenKeyExW.Call(
		uintptr(HKEY_CURRENT_USER),
		uintptr(unsafe.Pointer(keyPathPtr)),
		0,
		uintptr(KEY_SET_VALUE|KEY_WOW64_64KEY),
		uintptr(unsafe.Pointer(&hKey)),
	)

	if err != syscall.Errno(0) {
		return fmt.Errorf("调用RegOpenKeyExW失败: %w", err)
	}

	if ret != ERROR_SUCCESS {
		return fmt.Errorf("打开注册表键失败: %w (路径: %s)", getLastError(), keyPath)
	}
	defer procRegCloseKey.Call(hKey)

	data := uint32(value)
	ret, _, err = procRegSetValueExW.Call(
		hKey,
		uintptr(unsafe.Pointer(valueNamePtr)),
		0,
		uintptr(REG_DWORD),
		uintptr(unsafe.Pointer(&data)),
		uintptr(4),
	)

	if err != syscall.Errno(0) {
		return fmt.Errorf("调用RegSetValueExW失败: %w", err)
	}

	if ret != ERROR_SUCCESS {
		return fmt.Errorf("设置注册表值失败: %w (值名称: %s)", getLastError(), valueName)
	}

	return nil
}

// 执行系统命令（无窗口版本）
func executeCommandNoWindow(command string) error {
	cmd := exec.Command("cmd", "/c", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow: true, // 隐藏窗口
	}
	return cmd.Run()
}

// 重启Windows资源管理器（修复版）
func restartExplorer() error {
	logToFile("正在重启Windows资源管理器...")
	
	// 1. 结束explorer.exe进程（静默执行）
	if err := executeCommandNoWindow("taskkill /f /im explorer.exe"); err != nil {
		logToFile(fmt.Sprintf("结束explorer.exe失败（可能已经结束）: %v", err))
	}
	
	// 等待一会儿
	time.Sleep(1000 * time.Millisecond)
	
	// 2. 启动新的explorer.exe进程
	cmd := exec.Command("explorer.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow: true,
	}
	if err := cmd.Start(); err != nil {
		logToFile(fmt.Sprintf("启动explorer.exe失败: %v", err))
		return fmt.Errorf("重启资源管理器失败: %w", err)
	}
	
	// 等待资源管理器完全启动
	time.Sleep(2000 * time.Millisecond)
	logToFile("Windows资源管理器重启完成")
	return nil
}

// 发送主题变更通知
func notifyThemeChange() {
	logToFile("发送主题变更通知...")
	
	// 发送WM_SETTINGCHANGE消息
	lParam, _ := syscall.UTF16PtrFromString("ImmersiveColorSet")
	procSendMessageW.Call(
		uintptr(HWND_BROADCAST),
		uintptr(WM_SETTINGCHANGE),
		0,
		uintptr(unsafe.Pointer(lParam)),
	)
	
	// 发送SendNotifyMessage（异步）
	lParam2, _ := syscall.UTF16PtrFromString("Policy")
	procSendNotifyMessageW.Call(
		uintptr(HWND_BROADCAST),
		uintptr(WM_SETTINGCHANGE),
		0,
		uintptr(unsafe.Pointer(lParam2)),
	)
	
	time.Sleep(500 * time.Millisecond)
}

// 切换到浅色模式
func switchToLightMode() error {
	registryPath := `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`

	logToFile("正在切换到浅色模式...")
	
	// 设置注册表值
	if err := setRegistryValue(registryPath, "AppsUseLightTheme", 1); err != nil {
		return fmt.Errorf("设置AppsUseLightTheme失败: %w", err)
	}
	if err := setRegistryValue(registryPath, "SystemUsesLightTheme", 1); err != nil {
		return fmt.Errorf("设置SystemUsesLightTheme失败: %w", err)
	}
	// 重要：只设置ColorPrevalence为1（让任务栏字体变白色），但不开启"在开始和任务栏上显示重点颜色"
	if err := setRegistryValue(registryPath, "ColorPrevalence", 1); err != nil {
		return fmt.Errorf("设置ColorPrevalence失败: %w", err)
	}

	logToFile("注册表设置完成，开始刷新...")
	
	// 发送通知
	notifyThemeChange()
	
	// 强制刷新命令（静默执行）
	if err := executeCommandNoWindow("rundll32.exe user32.dll,UpdatePerUserSystemParameters 1, True"); err != nil {
		logToFile(fmt.Sprintf("强制刷新命令失败: %v", err))
	}
	
	// 可选：重启资源管理器以确保完全生效（注释掉以避免弹窗）
	// if err := restartExplorer(); err != nil {
	//     logToFile(fmt.Sprintf("重启资源管理器失败，但主题设置已应用: %v", err))
	// }
	
	logToFile("浅色模式切换完成")
	return nil
}

// 切换到深色模式
func switchToDarkMode() error {
	registryPath := `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`

	logToFile("正在切换到深色模式...")
	
	// 设置注册表值
	if err := setRegistryValue(registryPath, "AppsUseLightTheme", 0); err != nil {
		return fmt.Errorf("设置AppsUseLightTheme失败: %w", err)
	}
	if err := setRegistryValue(registryPath, "SystemUsesLightTheme", 0); err != nil {
		return fmt.Errorf("设置SystemUsesLightTheme失败: %w", err)
	}
	// 深色模式下设置ColorPrevalence为0或不设置，让系统使用默认
	// 注意：我们完全移除ColorPrevalence的设置，只在浅色模式设置
	// if err := setRegistryValue(registryPath, "ColorPrevalence", 0); err != nil {
	//     return fmt.Errorf("设置ColorPrevalence失败: %w", err)
	// }

	logToFile("注册表设置完成，开始刷新...")
	
	// 发送通知
	notifyThemeChange()
	
	// 强制刷新命令（静默执行）
	if err := executeCommandNoWindow("rundll32.exe user32.dll,UpdatePerUserSystemParameters 1, True"); err != nil {
		logToFile(fmt.Sprintf("强制刷新命令失败: %v", err))
	}
	
	logToFile("深色模式切换完成")
	return nil
}

// 计算下次切换时间（简化版）
func getNextSwitchTime() time.Duration {
	now := time.Now()
	currentHour := now.Hour()

	var nextHour int
	if currentHour < 6 {
		nextHour = 6
	} else if currentHour < 18 {
		nextHour = 18
	} else {
		nextHour = 30 // 明天6点
	}

	// 计算目标时间
	targetTime := time.Date(now.Year(), now.Month(), now.Day(), nextHour%24, 0, 0, 0, now.Location())
	if nextHour >= 24 {
		targetTime = targetTime.Add(24 * time.Hour)
	}

	duration := targetTime.Sub(now)
	if duration <= 0 {
		return 5 * time.Minute // 安全的最小间隔
	}
	return duration
}

// 创建日志文件
func logToFile(message string) {
	exePath, err := os.Executable()
	if err != nil {
		return // 如果获取路径失败，静默退出
	}
	exeDir := filepath.Dir(exePath)
	logPath := filepath.Join(exeDir, "theme_switcher.log")

	file, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer file.Close()

	logTime := time.Now().Format("2006-01-02 15:04:05")
	logEntry := fmt.Sprintf("[%s] %s\n", logTime, message)
	file.WriteString(logEntry)
}

// 检查管理员权限
func isAdmin() bool {
	_, err := os.Open("\\\\.\\PHYSICALDRIVE0")
	return err == nil
}

func main() {
	// 隐藏控制台窗口
	hideConsoleWindow()

	// 检查管理员权限
	if !isAdmin() {
		logToFile("错误：需要以管理员身份运行此程序")
		return
	}

	logToFile("=== Windows主题自动切换程序启动 ===")

	// 首次切换
	now := time.Now()
	var initErr error
	var initMode string

	if now.Hour() >= 6 && now.Hour() < 18 {
		initErr = switchToLightMode()
		initMode = "浅色模式"
	} else {
		initErr = switchToDarkMode()
		initMode = "深色模式"
	}

	logToFile(fmt.Sprintf("程序启动，当前时间: %s", now.Format("15:04:05")))
	logToFile(fmt.Sprintf("首次尝试切换到%s", initMode))

	if initErr != nil {
		logToFile(fmt.Sprintf("首次切换失败: %v", initErr))
	} else {
		logToFile("首次切换成功")
	}

	// 主循环
	for {
		waitTime := getNextSwitchTime()
		logToFile(fmt.Sprintf("等待 %.0f分钟直到下次切换", waitTime.Minutes()))

		select {
		case <-time.After(waitTime):
			now = time.Now()
			var err error
			var mode string

			if now.Hour() >= 6 && now.Hour() < 18 {
				err = switchToLightMode()
				mode = "浅色模式"
			} else {
				err = switchToDarkMode()
				mode = "深色模式"
			}

			if err != nil {
				logToFile(fmt.Sprintf("切换到%s失败: %v", mode, err))
				// 失败后等待5分钟再试
				time.Sleep(5 * time.Minute)
			} else {
				logToFile(fmt.Sprintf("成功切换到%s (时间: %s)", mode, now.Format("15:04:05")))
			}
		}
	}
}