package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	HKEY_CURRENT_USER              = 0x80000001
	KEY_SET_VALUE                  = 0x0002
	KEY_WOW64_64KEY                = 0x0100
	REG_DWORD                      = 0x0004
	WM_SETTINGCHANGE               = 0x001A
	WM_DWMCOLORIZATIONCOLORCHANGED = 0x0320
	HWND_BROADCAST                 = 0xFFFF
	SW_HIDE                        = 0
	SMTO_ABORTIFHUNG               = 0x0002
)

var (
	advapi32 = windows.NewLazySystemDLL("advapi32.dll")
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")

	procRegOpenKeyEx       = advapi32.NewProc("RegOpenKeyExW")
	procRegSetValueEx      = advapi32.NewProc("RegSetValueExW")
	procRegCloseKey        = advapi32.NewProc("RegCloseKey")
	procSendMessageTimeout = user32.NewProc("SendMessageTimeoutW")
	procShowWindow         = user32.NewProc("ShowWindow")
	procGetConsoleWindow   = kernel32.NewProc("GetConsoleWindow")
	procShellExecute       = shell32.NewProc("ShellExecuteW")

	switchMutex sync.Mutex
	logMutex    sync.Mutex
	config      Config
	configPath  string
	logPath     string
	exePath     string
)

type Config struct {
	LightModeWhiteText bool `json:"light_mode_white_text"`
	DarkModeWhiteText  bool `json:"dark_mode_white_text"`
	LightTimeStart     int  `json:"light_time_start"`
	DarkTimeStart      int  `json:"dark_time_start"`
	EnableLogging      bool `json:"enable_logging"`
}

var (
	version       = "dev"
	defaultConfig = Config{
		LightModeWhiteText: false,
		DarkModeWhiteText:  true,
		LightTimeStart:     6,
		DarkTimeStart:      18,
		EnableLogging:      true,
	}
)

func init() {
	var err error
	exePath, err = os.Executable()
	if err != nil {
		exePath, _ = filepath.Abs(os.Args[0])
	}
	exeDir := filepath.Dir(exePath)
	configPath = filepath.Join(exeDir, "config.json")
	logPath = filepath.Join(exeDir, "theme_switcher.log")
}

// --- 注册表操作 ---

func setRegistryValue(keyPath, valueName string, value uint32) error {
	keyPathPtr, err := windows.UTF16PtrFromString(keyPath)
	if err != nil {
		return fmt.Errorf("转换路径失败: %w", err)
	}
	valueNamePtr, err := windows.UTF16PtrFromString(valueName)
	if err != nil {
		return fmt.Errorf("转换值名失败: %w", err)
	}

	var hKey windows.Handle
	ret, _, err := procRegOpenKeyEx.Call(
		uintptr(HKEY_CURRENT_USER),
		uintptr(unsafe.Pointer(keyPathPtr)),
		0,
		uintptr(KEY_SET_VALUE|KEY_WOW64_64KEY),
		uintptr(unsafe.Pointer(&hKey)),
	)
	if ret != 0 {
		return fmt.Errorf("打开注册表键失败: 错误代码 %d", ret)
	}
	defer procRegCloseKey.Call(uintptr(hKey))

	ret, _, err = procRegSetValueEx.Call(
		uintptr(hKey),
		uintptr(unsafe.Pointer(valueNamePtr)),
		0,
		uintptr(REG_DWORD),
		uintptr(unsafe.Pointer(&value)),
		4,
	)
	if ret != 0 {
		return fmt.Errorf("设置注册表值失败: 错误代码 %d", ret)
	}
	return nil
}

// --- 主题刷新 ---

func refreshTheme() error {
	// 发出 WM_SETTINGCHANGE，传入不同 lParam 字符串
	for _, change := range []string{"ImmersiveColorSet", "Policy"} {
		lParamPtr, err := windows.UTF16PtrFromString(change)
		if err != nil {
			return err
		}
		var result uintptr
		ret, _, err := procSendMessageTimeout.Call(
			uintptr(HWND_BROADCAST),
			uintptr(WM_SETTINGCHANGE),
			0,
			uintptr(unsafe.Pointer(lParamPtr)),
			uintptr(SMTO_ABORTIFHUNG),
			3000,
			uintptr(unsafe.Pointer(&result)),
		)
		if ret == 0 {
			return fmt.Errorf("广播消息 '%s' 失败: %v", change, err)
		}
	}
	// DWM 颜色变更通知
	lParamPtr, _ := windows.UTF16PtrFromString("")
	var result uintptr
	procSendMessageTimeout.Call(
		uintptr(HWND_BROADCAST),
		uintptr(WM_DWMCOLORIZATIONCOLORCHANGED),
		0,
		uintptr(unsafe.Pointer(lParamPtr)),
		uintptr(SMTO_ABORTIFHUNG),
		3000,
		uintptr(unsafe.Pointer(&result)),
	)
	return nil
}

// --- 核心主题逻辑 ---

func applyThemeLogic(isLightMode bool) error {
	// WinRT API 目前不公开系统主题切换接口，使用注册表方式
	return applyThemeLogicViaRegistry(isLightMode)
}

func applyThemeLogicViaRegistry(isLightMode bool) error {
	regPath := `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`

	appMode := uint32(0)
	if isLightMode {
		appMode = 1
	}
	if err := setRegistryValue(regPath, "AppsUseLightTheme", appMode); err != nil {
		return fmt.Errorf("设置应用主题失败: %w", err)
	}

	useWhiteText := config.DarkModeWhiteText
	if isLightMode {
		useWhiteText = config.LightModeWhiteText
	}

	sysMode := appMode
	colorPrevalence := uint32(0)
	if useWhiteText {
		colorPrevalence = 1
	} else {
		sysMode = 1
	}

	if err := setRegistryValue(regPath, "SystemUsesLightTheme", sysMode); err != nil {
		return fmt.Errorf("设置系统主题失败: %w", err)
	}
	if err := setRegistryValue(regPath, "ColorPrevalence", colorPrevalence); err != nil {
		return fmt.Errorf("设置颜色应用失败: %w", err)
	}

	return refreshTheme()
}

// --- 日志功能 ---

func logToFile(format string, args ...interface{}) {
	if !config.EnableLogging {
		return
	}
	logMutex.Lock()
	defer logMutex.Unlock()

	file, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		backupLogPath := filepath.Join(os.TempDir(), "theme_switcher.log")
		file, err = os.OpenFile(backupLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return
		}
	}
	defer file.Close()

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	fmt.Fprintf(file, "[%s] %s\n", timestamp, fmt.Sprintf(format, args...))
}

// --- 配置管理 ---

func loadConfig() error {
	config = defaultConfig
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return saveConfig()
		}
		return fmt.Errorf("读取配置文件失败: %w", err)
	}
	if err := json.Unmarshal(data, &config); err != nil {
		logToFile("解析配置文件失败，使用默认配置: %v", err)
		config = defaultConfig
		return saveConfig()
	}
	validateConfig()
	return nil
}

func validateConfig() {
	if config.LightTimeStart < 0 || config.LightTimeStart > 23 {
		config.LightTimeStart = defaultConfig.LightTimeStart
	}
	if config.DarkTimeStart < 0 || config.DarkTimeStart > 23 {
		config.DarkTimeStart = defaultConfig.DarkTimeStart
	}
}

func saveConfig() error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}
	return os.WriteFile(configPath, data, 0644)
}

// --- 权限和任务管理 ---

func isAdmin() bool {
	token := windows.GetCurrentProcessToken()
	defer token.Close()
	return token.IsElevated()
}

func runAsAdmin() error {
	verbPtr, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return fmt.Errorf("创建runas字符串失败: %w", err)
	}
	exePtr, err := windows.UTF16PtrFromString(exePath)
	if err != nil {
		return fmt.Errorf("创建路径字符串失败: %w", err)
	}
	argPtr, err := windows.UTF16PtrFromString("")
	if err != nil {
		return fmt.Errorf("创建参数字符串失败: %w", err)
	}

	ret, _, err := procShellExecute.Call(
		0,
		uintptr(unsafe.Pointer(verbPtr)),
		uintptr(unsafe.Pointer(exePtr)),
		uintptr(unsafe.Pointer(argPtr)),
		0,
		uintptr(SW_HIDE),
	)
	if ret <= 32 {
		return fmt.Errorf("ShellExecute失败，返回值: %d", ret)
	}
	return nil
}

func setupScheduledTask() error {
	taskName := "WindowsThemeAutoSwitcher"
	exeDir := filepath.Dir(exePath)

	psCommand := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
try {
    $userId = "$env:USERDOMAIN\$env:USERNAME"
    $action = New-ScheduledTaskAction -Execute '%s' -Argument '--scheduled' -WorkingDirectory '%s'
    
    $triggers = @()
    $triggers += New-ScheduledTaskTrigger -Daily -At %02d:00
    $triggers += New-ScheduledTaskTrigger -Daily -At %02d:00
    $triggers += New-ScheduledTaskTrigger -AtLogon
    
	$principal = New-ScheduledTaskPrincipal -UserId $userId -LogonType Interactive -RunLevel Highest
    $settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -StartWhenAvailable -MultipleInstances IgnoreNew
    
    Register-ScheduledTask -TaskName '%s' -Action $action -Trigger $triggers -Principal $principal -Settings $settings -Force
    Write-Output '计划任务注册成功'
} catch {
    Write-Error "计划任务注册失败: $_"
    exit 1
}`,
		strings.ReplaceAll(exePath, "'", "''"),
		strings.ReplaceAll(exeDir, "'", "''"),
		config.LightTimeStart,
		config.DarkTimeStart,
		taskName,
	)

	cmd := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", psCommand)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := cmd.CombinedOutput()
	if err != nil {
		logToFile("计划任务设置失败: %v, 输出: %s", err, string(output))
		return fmt.Errorf("计划任务设置失败: %w", err)
	}
	logToFile("计划任务设置成功: %s", strings.TrimSpace(string(output)))
	return nil
}

// --- 主题切换逻辑 ---

func shouldUseLightMode(hour int) bool {
	if config.LightTimeStart < config.DarkTimeStart {
		return hour >= config.LightTimeStart && hour < config.DarkTimeStart
	}
	return hour >= config.LightTimeStart || hour < config.DarkTimeStart
}

func performSingleSwitch() error {
	switchMutex.Lock()
	defer switchMutex.Unlock()

	now := time.Now()
	isLight := shouldUseLightMode(now.Hour())

	logToFile("执行主题切换: 时间=%s, 小时=%d, 浅色模式=%v",
		now.Format("15:04:05"), now.Hour(), isLight)

	if err := applyThemeLogic(isLight); err != nil {
		logToFile("主题切换失败: %v", err)
		return fmt.Errorf("主题切换失败: %w", err)
	}

	logToFile("主题切换成功")
	return nil
}

// --- 隐藏控制台窗口 ---

func hideConsoleWindow() {
	consoleWindow, _, _ := procGetConsoleWindow.Call()
	if consoleWindow != 0 {
		procShowWindow.Call(consoleWindow, SW_HIDE)
	}
}

// --- 主函数 ---

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Printf("Theme Switcher %s\n", version)
		return
	}

	hideConsoleWindow()

	if err := loadConfig(); err != nil {
		logToFile("配置加载警告: %v", err)
	}

	// 处理计划任务调用
	if len(os.Args) > 1 && os.Args[1] == "--scheduled" {
		if err := performSingleSwitch(); err != nil {
			logToFile("计划任务执行失败: %v", err)
			os.Exit(1)
		}
		return
	}

	// 检查管理员权限
	if !isAdmin() {
		if err := runAsAdmin(); err != nil {
			logToFile("提升权限失败: %v", err)
			fmt.Fprintf(os.Stderr, "需要管理员权限运行，请手动以管理员身份运行此程序")
			os.Exit(1)
		}
		return
	}

	// 设置计划任务
	if err := setupScheduledTask(); err != nil {
		logToFile("计划任务设置失败: %v", err)
		fmt.Fprintf(os.Stderr, "计划任务设置失败: %v\n", err)
	}

	// 执行初始主题切换
	if err := performSingleSwitch(); err != nil {
		logToFile("初始主题切换失败: %v", err)
		fmt.Fprintf(os.Stderr, "主题切换失败: %v\n", err)
		os.Exit(1)
	}

	time.Sleep(1 * time.Second)
}
