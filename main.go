package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
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
	TOKEN_QUERY             = 0x0008
	TokenElevation          = 20
)

// 全局初始化Windows API Proc
var (
	modadvapi32          = syscall.NewLazyDLL("advapi32.dll")
	moduser32            = syscall.NewLazyDLL("user32.dll")
	modkernel32          = syscall.NewLazyDLL("kernel32.dll")
	modshell32           = syscall.NewLazyDLL("shell32.dll")

	procRegOpenKeyExW    = modadvapi32.NewProc("RegOpenKeyExW")
	procRegSetValueExW   = modadvapi32.NewProc("RegSetValueExW")
	procRegCloseKey      = modadvapi32.NewProc("RegCloseKey")
	procSendMessageW     = moduser32.NewProc("SendMessageW")
	procSendNotifyMessageW = moduser32.NewProc("SendNotifyMessageW")
	procShowWindow       = moduser32.NewProc("ShowWindow")
	procGetConsoleWindow = modkernel32.NewProc("GetConsoleWindow")
	procShellExecuteW    = modshell32.NewProc("ShellExecuteW")
	procOpenProcessToken = modadvapi32.NewProc("OpenProcessToken")
	procGetTokenInformation = modadvapi32.NewProc("GetTokenInformation")
)

// 全局变量
var (
	lastSwitchTime time.Time
	switchMutex    sync.Mutex
	logMutex       sync.Mutex // 日志写入锁
	config         Config     // 配置文件
)

// 配置文件结构
type Config struct {
	LightModeTaskbarColor string `json:"light_mode_taskbar_color"` // "white" 或 "default"
	DarkModeTaskbarColor  string `json:"dark_mode_taskbar_color"`  // "black" 或 "default"
	LightTimeStart        int    `json:"light_time_start"`         // 浅色模式开始时间（小时）
	DarkTimeStart         int    `json:"dark_time_start"`          // 深色模式开始时间（小时）
	EnableLogging         bool   `json:"enable_logging"`           // 是否启用日志
}

// 默认配置
var defaultConfig = Config{
	LightModeTaskbarColor: "white",  // 默认浅色模式任务栏字体为白色
	DarkModeTaskbarColor:  "default", // 默认深色模式任务栏字体不变
	LightTimeStart:        6,         // 早上6点开始浅色模式
	DarkTimeStart:         18,        // 晚上6点开始深色模式
	EnableLogging:         true,      // 默认启用日志
}

// 隐藏控制台窗口
func hideConsoleWindow() {
	consoleWindow, _, _ := procGetConsoleWindow.Call()
	if consoleWindow != 0 {
		procShowWindow.Call(consoleWindow, SW_HIDE)
	}
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
	if ret != ERROR_SUCCESS {
		return fmt.Errorf("打开注册表键失败: 错误码 %d (路径: %s), 详细错误: %w", ret, keyPath, err)
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
	if ret != ERROR_SUCCESS {
		return fmt.Errorf("设置注册表值失败: 错误码 %d (值名称: %s), 详细错误: %w", ret, valueName, err)
	}

	return nil
}

// 执行系统命令（无窗口版本，带超时，兼容所有Go版本）
func executeCommandNoWindow(command string) error {
	cmd := exec.Command("cmd", "/c", command)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow: true,
	}

	// 超时处理（无需context包）
	done := make(chan error, 1)
	go func() {
		done <- cmd.Run()
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second): // 5秒超时
		// 超时后杀死进程
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		return fmt.Errorf("命令执行超时: %s", command)
	}
}

// 重启Windows资源管理器（还原最初版本逻辑）
func restartExplorer() error {
	logToFile("正在重启Windows资源管理器...")
	
	// 1. 结束explorer.exe进程
	if err := executeCommandNoWindow("taskkill /f /im explorer.exe"); err != nil {
		logToFile(fmt.Sprintf("结束explorer.exe失败（可能已经结束）: %v", err))
	}
	
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
	
	time.Sleep(2000 * time.Millisecond)
	logToFile("Windows资源管理器重启完成")
	return nil
}

// 发送主题变更通知
func notifyThemeChange() {
	logToFile("发送主题变更通知...")
	
	lParam, err := syscall.UTF16PtrFromString("ImmersiveColorSet")
	if err != nil {
		logToFile(fmt.Sprintf("转换通知参数失败: %v", err))
		return
	}
	procSendMessageW.Call(
		uintptr(HWND_BROADCAST),
		uintptr(WM_SETTINGCHANGE),
		0,
		uintptr(unsafe.Pointer(lParam)),
	)
	
	lParam2, err := syscall.UTF16PtrFromString("Policy")
	if err != nil {
		logToFile(fmt.Sprintf("转换通知参数失败: %v", err))
		return
	}
	procSendNotifyMessageW.Call(
		uintptr(HWND_BROADCAST),
		uintptr(WM_SETTINGCHANGE),
		0,
		uintptr(unsafe.Pointer(lParam2)),
	)
	
	time.Sleep(500 * time.Millisecond)
}

// 切换到浅色模式（根据配置决定任务栏字体颜色）
func switchToLightMode() error {
	registryPath := `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`

	logToFile("正在切换到浅色模式...")
	
	if err := setRegistryValue(registryPath, "AppsUseLightTheme", 1); err != nil {
		return fmt.Errorf("设置AppsUseLightTheme失败: %w", err)
	}
	if err := setRegistryValue(registryPath, "SystemUsesLightTheme", 1); err != nil {
		return fmt.Errorf("设置SystemUsesLightTheme失败: %w", err)
	}
	
	// 根据配置设置任务栏字体颜色
	if config.LightModeTaskbarColor == "white" {
		// ColorPrevalence为2时，任务栏字体为白色，但不开启重点颜色
		if err := setRegistryValue(registryPath, "ColorPrevalence", 2); err != nil {
			logToFile(fmt.Sprintf("注意：设置ColorPrevalence为2失败（但主题仍会切换）: %v", err))
		}
		logToFile("浅色模式：任务栏字体设置为白色")
	} else {
		// ColorPrevalence为0时，任务栏字体为黑色
		if err := setRegistryValue(registryPath, "ColorPrevalence", 0); err != nil {
			logToFile(fmt.Sprintf("注意：设置ColorPrevalence为0失败（但主题仍会切换）: %v", err))
		}
		logToFile("浅色模式：任务栏字体设置为黑色（默认）")
	}

	logToFile("注册表设置完成，开始刷新...")
	
	notifyThemeChange()
	
	if err := executeCommandNoWindow("rundll32.exe user32.dll,UpdatePerUserSystemParameters 1, True"); err != nil {
		logToFile(fmt.Sprintf("强制刷新命令失败: %v", err))
	}
	
	if err := executeCommandNoWindow("powershell -Command \"& {Get-Process explorer | Stop-Process -Force; Start-Sleep -Seconds 2; explorer}\""); err != nil {
		logToFile(fmt.Sprintf("PowerShell刷新失败（非关键错误）: %v", err))
	}
	
	time.Sleep(2000 * time.Millisecond)
	logToFile("浅色模式切换完成")
	return nil
}

// 切换到深色模式（根据配置决定任务栏字体颜色）
func switchToDarkMode() error {
	registryPath := `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`

	logToFile("正在切换到深色模式...")
	
	if err := setRegistryValue(registryPath, "AppsUseLightTheme", 0); err != nil {
		return fmt.Errorf("设置AppsUseLightTheme失败: %w", err)
	}
	if err := setRegistryValue(registryPath, "SystemUsesLightTheme", 0); err != nil {
		return fmt.Errorf("设置SystemUsesLightTheme失败: %w", err)
	}
	
	// 根据配置设置任务栏字体颜色
	if config.DarkModeTaskbarColor == "black" {
		// 深色模式下设置任务栏字体为黑色
		if err := setRegistryValue(registryPath, "ColorPrevalence", 0); err != nil {
			logToFile(fmt.Sprintf("设置ColorPrevalence为0失败（非关键错误）: %v", err))
		}
		logToFile("深色模式：任务栏字体设置为黑色")
	} else {
		// 深色模式下任务栏字体为白色
		if err := setRegistryValue(registryPath, "ColorPrevalence", 2); err != nil {
			logToFile(fmt.Sprintf("注意：设置ColorPrevalence为2失败（但主题仍会切换）: %v", err))
		}
		logToFile("深色模式：任务栏字体设置为白色（默认）")
	}

	logToFile("注册表设置完成，开始刷新...")
	
	notifyThemeChange()
	
	if err := executeCommandNoWindow("rundll32.exe user32.dll,UpdatePerUserSystemParameters 1, True"); err != nil {
		logToFile(fmt.Sprintf("强制刷新命令失败: %v", err))
	}
	
	time.Sleep(1000 * time.Millisecond)
	logToFile("深色模式切换完成")
	return nil
}

// 日志写入（加锁）
func logToFile(message string) {
	if !config.EnableLogging {
		return
	}
	
	logMutex.Lock()
	defer logMutex.Unlock()
	
	exePath, err := os.Executable()
	if err != nil {
		return
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
	_, _ = file.WriteString(logEntry)
}

// 正确判断管理员权限（兼容Go版本）
func isAdmin() bool {
	// 兼容syscall.GetCurrentProcess的返回值
	var currentProcess syscall.Handle
	var err error
	// 新版Go返回两个值，旧版返回一个
	if p, e := syscall.GetCurrentProcess(); e != nil {
		err = e
	} else {
		currentProcess = p
	}
	if err != nil {
		logToFile(fmt.Sprintf("获取当前进程失败: %v", err))
		return false
	}

	var token syscall.Token
	err = syscall.OpenProcessToken(currentProcess, TOKEN_QUERY, &token)
	if err != nil {
		logToFile(fmt.Sprintf("OpenProcessToken失败: %v", err))
		return false
	}
	defer token.Close()

	type tokenElevation struct {
		TokenIsElevated uint32
	}

	var elevation tokenElevation
	buf := (*byte)(unsafe.Pointer(&elevation))
	bufLen := uint32(unsafe.Sizeof(elevation))
	
	// 修正GetTokenInformation参数类型
	err = syscall.GetTokenInformation(token, TokenElevation, buf, bufLen, &bufLen)
	if err != nil {
		logToFile(fmt.Sprintf("GetTokenInformation失败: %v", err))
		return false
	}

	return elevation.TokenIsElevated != 0
}

// 设置Windows计划任务（修复账户和语法问题）
func setupScheduledTask() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取可执行文件路径失败: %w", err)
	}
	
	exePath, err = filepath.Abs(exePath)
	if err != nil {
		return fmt.Errorf("获取绝对路径失败: %w", err)
	}
	
	if _, err := os.Stat(exePath); err != nil {
		return fmt.Errorf("可执行文件不存在: %w", err)
	}
	
	taskName := "WindowsThemeAutoSwitcher"
	exeDir := filepath.Dir(exePath)
	
	logToFile(fmt.Sprintf("创建计划任务 - 程序路径: %s", exePath))
	
	// 修复PowerShell脚本：使用当前用户账户，修正语法
	psScript := fmt.Sprintf(`
$taskName = "%s"
$exePath = "%s"
$exeDir = "%s"

# 获取当前登录用户
$currentUser = [System.Security.Principal.WindowsIdentity]::GetCurrent().Name

# 检查并删除现有任务
$existingTask = Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
if ($existingTask) {
    Unregister-ScheduledTask -TaskName $taskName -Confirm:$false
    Write-Host "已删除现有任务: $taskName"
}

# 创建任务主体（当前用户，最高权限）
$principal = New-ScheduledTaskPrincipal -UserId $currentUser -LogonType Interactive -RunLevel Highest

# 创建任务操作
$action = New-ScheduledTaskAction -Execute $exePath -Argument "--scheduled" -WorkingDirectory $exeDir

# 创建触发器：每天 %d:00 和 %d:00
$trigger1 = New-ScheduledTaskTrigger -Daily -At "%02d:00AM"
$trigger2 = New-ScheduledTaskTrigger -Daily -At "%02d:00PM"

# 创建任务设置
$settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries ` +
`-DontStopIfGoingOnBatteries -StartWhenAvailable -WakeToRun ` +
`-MultipleInstances IgnoreNew -RestartInterval (New-TimeSpan -Minutes 1) ` +
`-RestartCount 3 -RunOnlyIfNetworkAvailable:$false ` +
`-ExecutionTimeLimit (New-TimeSpan -Hours 1)

# 注册任务
$task = New-ScheduledTask -Action $action -Trigger $trigger1, $trigger2 -Principal $principal -Settings $settings ` +
`-Description "自动切换Windows浅色/深色主题"

try {
    Register-ScheduledTask -TaskName $taskName -InputObject $task -Force -ErrorAction Stop
    Write-Host "计划任务创建成功: $taskName"
    
    # 启用任务
    Enable-ScheduledTask -TaskName $taskName
    Write-Host "任务已启用"
    
    return $true
} catch {
    Write-Host "创建计划任务失败: $_"
    return $false
}
`, taskName, exePath, exeDir, config.LightTimeStart, config.DarkTimeStart-12, config.LightTimeStart, config.DarkTimeStart-12)
	
	// 创建临时PS脚本
	tempDir := os.TempDir()
	psFile := filepath.Join(tempDir, "setup_theme_task.ps1")
	err = os.WriteFile(psFile, []byte(psScript), 0600)
	if err != nil {
		return fmt.Errorf("创建临时脚本失败: %w", err)
	}
	defer os.Remove(psFile)
	
	logToFile("执行PowerShell脚本创建计划任务...")
	
	// 执行PowerShell
	cmd := exec.Command("powershell", "-ExecutionPolicy", "Bypass", "-File", psFile)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow: false,
	}
	output, err := cmd.CombinedOutput()
	logToFile(fmt.Sprintf("PowerShell输出:\n%s", string(output)))
	
	if err != nil {
		logToFile(fmt.Sprintf("PowerShell创建任务失败: %v", err))
		// 降级使用schtasks
		if err2 := createTaskWithSchTasks(exePath, taskName); err2 != nil {
			return fmt.Errorf("创建任务失败: %w, schtasks错误: %v", err, err2)
		}
	}
	
	logToFile("计划任务创建成功")
	return nil
}

// 使用schtasks创建任务（备用）
func createTaskWithSchTasks(exePath, taskName string) error {
	logToFile("使用schtasks创建计划任务...")
	
	// 获取当前用户
	currentUser, err := exec.Command("whoami").Output()
	if err != nil {
		return fmt.Errorf("获取当前用户失败: %w", err)
	}
	user := string(currentUser[:len(currentUser)-1]) // 去掉换行
	
	xmlContent := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.4" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>自动切换Windows主题</Description>
    <Author>%s</Author>
  </RegistrationInfo>
  <Triggers>
    <CalendarTrigger>
      <StartBoundary>2030-01-01T%02d:00:00</StartBoundary>
      <Enabled>true</Enabled>
      <ScheduleByDay><DaysInterval>1</DaysInterval></ScheduleByDay>
    </CalendarTrigger>
    <CalendarTrigger>
      <StartBoundary>2030-01-01T%02d:00:00</StartBoundary>
      <Enabled>true</Enabled>
      <ScheduleByDay><DaysInterval>1</DaysInterval></ScheduleByDay>
    </CalendarTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <UserId>%s</UserId>
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>HighestAvailable</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <StartWhenAvailable>true</StartWhenAvailable>
    <WakeToRun>true</WakeToRun>
    <ExecutionTimeLimit>PT1H</ExecutionTimeLimit>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>"%s"</Command>
      <Arguments>--scheduled</Arguments>
    </Exec>
  </Actions>
</Task>`, user, config.LightTimeStart, config.DarkTimeStart, user, exePath)
	
	tempDir := os.TempDir()
	xmlFile := filepath.Join(tempDir, "theme_task.xml")
	err = os.WriteFile(xmlFile, []byte(xmlContent), 0600)
	if err != nil {
		return fmt.Errorf("创建XML文件失败: %w", err)
	}
	defer os.Remove(xmlFile)
	
	// 删除旧任务
	exec.Command("schtasks", "/Delete", "/TN", taskName, "/F").Run()
	
	// 创建新任务
	cmd := exec.Command("schtasks", "/Create", "/TN", taskName, "/XML", xmlFile, "/F")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := cmd.CombinedOutput()
	
	if err != nil {
		logToFile(fmt.Sprintf("schtasks失败: %v, 输出: %s", err, string(output)))
		return fmt.Errorf("schtasks创建失败: %w", err)
	}
	
	// 启用任务
	exec.Command("schtasks", "/Change", "/TN", taskName, "/Enable").Run()
	logToFile("schtasks创建任务成功")
	return nil
}

// 执行单次切换
func performSingleSwitch() error {
	switchMutex.Lock()
	defer switchMutex.Unlock()
	
	if time.Since(lastSwitchTime) < 5*time.Minute {
		logToFile("跳过：距离上次切换不足5分钟")
		return nil
	}
	
	now := time.Now()
	currentHour := now.Hour()
	logToFile(fmt.Sprintf("=== 执行切换 (时间: %s) ===", now.Format("15:04:05")))
	
	var err error
	var mode string
	
	if currentHour >= config.LightTimeStart && currentHour < config.DarkTimeStart {
		err = switchToLightMode()
		mode = "浅色模式"
		logToFile(fmt.Sprintf("当前时间 %02d，应该是白天，切换到浅色模式", currentHour))
	} else {
		err = switchToDarkMode()
		mode = "深色模式"
		logToFile(fmt.Sprintf("当前时间 %02d，应该是晚上，切换到深色模式", currentHour))
	}
	
	if err != nil {
		logToFile(fmt.Sprintf("切换到%s失败: %v", mode, err))
		return err
	}
	
	lastSwitchTime = now
	logToFile(fmt.Sprintf("成功切换到%s", mode))
	logCurrentThemeSettings()
	return nil
}

// 记录当前主题设置
func logCurrentThemeSettings() {
	if !config.EnableLogging {
		return
	}
	
	registryPath := `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`
	
	cmd := exec.Command("reg", "query", fmt.Sprintf("HKCU\\%s", registryPath), "/v", "AppsUseLightTheme")
	output, err := cmd.Output()
	if err == nil {
		logToFile(fmt.Sprintf("当前AppsUseLightTheme: %s", string(output)))
	}
	
	cmd = exec.Command("reg", "query", fmt.Sprintf("HKCU\\%s", registryPath), "/v", "ColorPrevalence")
	output, err = cmd.Output()
	if err == nil {
		logToFile(fmt.Sprintf("当前ColorPrevalence: %s", string(output)))
	}
}

// 以管理员身份重新运行
func runAsAdmin() bool {
	exePath, err := os.Executable()
	if err != nil {
		logToFile(fmt.Sprintf("获取可执行路径失败: %v", err))
		return false
	}
	
	verb, _ := syscall.UTF16PtrFromString("runas")
	exePathUTF16, _ := syscall.UTF16PtrFromString(exePath)
	argsUTF16, _ := syscall.UTF16PtrFromString("--scheduled")
	
	ret, _, _ := procShellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(exePathUTF16)),
		uintptr(unsafe.Pointer(argsUTF16)),
		0,
		SW_HIDE,
	)
	
	if ret > 32 {
		logToFile("已请求管理员权限重新运行")
		return true
	}
	
	logToFile(fmt.Sprintf("ShellExecute失败，返回码: %d", ret))
	return false
}

// 加载配置文件
func loadConfig() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取可执行文件路径失败: %w", err)
	}
	exeDir := filepath.Dir(exePath)
	configPath := filepath.Join(exeDir, "config.json")
	
	// 先设置为默认配置
	config = defaultConfig
	
	// 尝试读取配置文件
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// 配置文件不存在，创建默认配置文件
			return saveConfig()
		}
		return fmt.Errorf("读取配置文件失败: %w", err)
	}
	
	// 解析JSON
	err = json.Unmarshal(data, &config)
	if err != nil {
		return fmt.Errorf("解析配置文件失败: %w", err)
	}
	
	// 验证配置值
	if config.LightModeTaskbarColor != "white" && config.LightModeTaskbarColor != "default" {
		config.LightModeTaskbarColor = defaultConfig.LightModeTaskbarColor
	}
	if config.DarkModeTaskbarColor != "black" && config.DarkModeTaskbarColor != "default" {
		config.DarkModeTaskbarColor = defaultConfig.DarkModeTaskbarColor
	}
	if config.LightTimeStart < 0 || config.LightTimeStart > 23 {
		config.LightTimeStart = defaultConfig.LightTimeStart
	}
	if config.DarkTimeStart < 0 || config.DarkTimeStart > 23 {
		config.DarkTimeStart = defaultConfig.DarkTimeStart
	}
	
	logToFile(fmt.Sprintf("配置文件加载成功：浅色模式字体=%s，深色模式字体=%s，切换时间=%d:00和%d:00", 
		config.LightModeTaskbarColor, config.DarkModeTaskbarColor, 
		config.LightTimeStart, config.DarkTimeStart))
	
	return nil
}

// 保存配置文件
func saveConfig() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取可执行文件路径失败: %w", err)
	}
	exeDir := filepath.Dir(exePath)
	configPath := filepath.Join(exeDir, "config.json")
	
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("生成配置文件失败: %w", err)
	}
	
	err = os.WriteFile(configPath, data, 0644)
	if err != nil {
		return fmt.Errorf("保存配置文件失败: %w", err)
	}
	
	logToFile("默认配置文件已创建")
	return nil
}

func main() {
	hideConsoleWindow()
	
	// 加载配置
	if err := loadConfig(); err != nil {
		// 如果加载配置失败，使用默认配置
		fmt.Fprintf(os.Stderr, "加载配置失败，使用默认配置: %v\n", err)
		config = defaultConfig
	}
	
	logToFile("================================================")
	logToFile("Windows主题自动切换程序启动")
	logToFile(fmt.Sprintf("启动时间: %s", time.Now().Format("2006-01-02 15:04:05")))
	logToFile(fmt.Sprintf("命令行参数: %v", os.Args))
	
	// 计划任务模式
	if len(os.Args) > 1 && os.Args[1] == "--scheduled" {
		logToFile("模式: 计划任务执行模式")
		if !isAdmin() {
			logToFile("警告：无管理员权限，尝试提升...")
			if !runAsAdmin() {
				logToFile("提升权限失败，退出")
				return
			}
			return
		}
		
		if err := performSingleSwitch(); err != nil {
			logToFile(fmt.Sprintf("计划任务执行失败: %v", err))
		} else {
			logToFile("计划任务执行成功")
		}
		time.Sleep(2 * time.Second)
		return
	}
	
	// 交互模式
	logToFile("模式: 交互模式")
	logToFile(fmt.Sprintf("配置信息：浅色模式字体=%s，深色模式字体=%s，切换时间=%d:00和%d:00", 
		config.LightModeTaskbarColor, config.DarkModeTaskbarColor,
		config.LightTimeStart, config.DarkTimeStart))
	
	if !isAdmin() {
		logToFile("错误：需要以管理员身份运行此程序")
		logToFile("正在尝试以管理员身份重新启动...")
		if runAsAdmin() {
			logToFile("已请求提升权限，当前进程退出")
			time.Sleep(2 * time.Second)
			return
		}
		logToFile("无法获取管理员权限，程序退出")
		time.Sleep(3 * time.Second)
		return
	}
	
	logToFile("管理员权限验证成功")
	
	logToFile("正在设置Windows计划任务...")
	if err := setupScheduledTask(); err != nil {
		logToFile(fmt.Sprintf("设置计划任务失败: %v", err))
		logToFile("请手动创建计划任务：")
		logToFile("1. 打开'任务计划程序'")
		logToFile("2. 创建基本任务")
		logToFile(fmt.Sprintf("3. 设置触发器为每天 %d:00 和 %d:00", config.LightTimeStart, config.DarkTimeStart))
		logToFile("4. 操作为启动程序，选择此exe文件，参数添加--scheduled")
		logToFile("5. 勾选'使用最高权限运行'")
	} else {
		logToFile("计划任务设置成功")
		logToFile(fmt.Sprintf("提示：主题切换将由Windows计划任务在 %d:00 和 %d:00 自动执行", 
			config.LightTimeStart, config.DarkTimeStart))
		logToFile("提示：您可以在'任务计划程序'中查看和管理任务")
		
		// 立即执行一次切换
		logToFile("立即执行首次切换...")
		if err := performSingleSwitch(); err != nil {
			logToFile(fmt.Sprintf("首次切换失败: %v", err))
		}
		
		// 显示成功信息并退出
		logToFile("程序设置完成，将在10秒后退出")
		logToFile("您可以查看日志文件了解详情：theme_switcher.log")
		logToFile("如需修改配置，请编辑同目录下的 config.json 文件")
		time.Sleep(10 * time.Second)
	}
}