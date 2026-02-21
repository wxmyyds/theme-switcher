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
)

// Windows API 常量
const (
	HKEY_CURRENT_USER              = 0x80000001
	KEY_SET_VALUE                  = 0x0002
	KEY_QUERY_VALUE                = 0x0001
	KEY_WOW64_64KEY                = 0x0100
	REG_DWORD                      = 0x0004
	WM_SETTINGCHANGE               = 0x001A
	WM_DWMCOLORIZATIONCOLORCHANGED = 0x0320
	HWND_BROADCAST                 = 0xFFFF
	ERROR_SUCCESS                  = 0
	SW_HIDE                        = 0
	TOKEN_QUERY                    = 0x0008
	TokenElevation                 = 20
	SMTO_ABORTIFHUNG               = 0x0002
)

var (
	modadvapi32 = syscall.NewLazyDLL("advapi32.dll")
	moduser32   = syscall.NewLazyDLL("user32.dll")
	modkernel32 = syscall.NewLazyDLL("kernel32.dll")

	procRegOpenKeyExW       = modadvapi32.NewProc("RegOpenKeyExW")
	procRegSetValueExW      = modadvapi32.NewProc("RegSetValueExW")
	procRegQueryValueExW    = modadvapi32.NewProc("RegQueryValueExW")
	procRegCloseKey         = modadvapi32.NewProc("RegCloseKey")
	procSendMessageTimeoutW = moduser32.NewProc("SendMessageTimeoutW")
	procShowWindow          = moduser32.NewProc("ShowWindow")
	procGetConsoleWindow    = modkernel32.NewProc("GetConsoleWindow")
	procOpenProcessToken    = modadvapi32.NewProc("OpenProcessToken")
	procGetTokenInformation = modadvapi32.NewProc("GetTokenInformation")
	procGetCurrentProcess   = modkernel32.NewProc("GetCurrentProcess")
)

var (
	lastSwitchTime time.Time
	switchMutex    sync.Mutex
	logMutex       sync.Mutex
	config         Config
)

type Config struct {
	LightModeWhiteText int `json:"light_mode_white_text"`
	DarkModeWhiteText  int `json:"dark_mode_white_text"`
	LightTimeStart     int `json:"light_time_start"`
	DarkTimeStart      int `json:"dark_time_start"`
	EnableLogging      int `json:"enable_logging"`
}

var defaultConfig = Config{
	LightModeWhiteText: 0,
	DarkModeWhiteText:  1,
	LightTimeStart:     6,
	DarkTimeStart:      18,
	EnableLogging:      1,
}

// --- 基础注册表工具 ---

func setRegistryValue(keyPath, valueName string, value uint32) error {
	keyPathPtr, _ := syscall.UTF16PtrFromString(keyPath)
	valueNamePtr, _ := syscall.UTF16PtrFromString(valueName)
	var hKey syscall.Handle
	ret, _, _ := procRegOpenKeyExW.Call(uintptr(HKEY_CURRENT_USER), uintptr(unsafe.Pointer(keyPathPtr)), 0, uintptr(KEY_SET_VALUE|KEY_WOW64_64KEY), uintptr(unsafe.Pointer(&hKey)))
	if ret != ERROR_SUCCESS {
		return fmt.Errorf("fail open: %d", ret)
	}
	defer procRegCloseKey.Call(uintptr(hKey))
	data := uint32(value)
	ret, _, _ = procRegSetValueExW.Call(uintptr(hKey), uintptr(unsafe.Pointer(valueNamePtr)), 0, uintptr(REG_DWORD), uintptr(unsafe.Pointer(&data)), uintptr(4))
	if ret != ERROR_SUCCESS {
		return fmt.Errorf("fail set: %d", ret)
	}
	return nil
}

// --- 核心修复逻辑 ---

func refreshTheme() {
	var result uintptr
	lParam1, _ := syscall.UTF16PtrFromString("ImmersiveColorSet")
	procSendMessageTimeoutW.Call(uintptr(HWND_BROADCAST), uintptr(WM_SETTINGCHANGE), 0, uintptr(unsafe.Pointer(lParam1)), uintptr(SMTO_ABORTIFHUNG), 3000, uintptr(unsafe.Pointer(&result)))

	lParam2, _ := syscall.UTF16PtrFromString("Policy")
	procSendMessageTimeoutW.Call(uintptr(HWND_BROADCAST), uintptr(WM_SETTINGCHANGE), 0, uintptr(unsafe.Pointer(lParam2)), uintptr(SMTO_ABORTIFHUNG), 3000, uintptr(unsafe.Pointer(&result)))

	procSendMessageTimeoutW.Call(uintptr(HWND_BROADCAST), uintptr(WM_DWMCOLORIZATIONCOLORCHANGED), 0, 0, uintptr(SMTO_ABORTIFHUNG), 3000, uintptr(unsafe.Pointer(&result)))
}

func applyThemeLogic(isLightMode bool) error {
	regPath := `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`

	// 1. 设置应用模式 (AppsUseLightTheme)
	appMode := uint32(0)
	if isLightMode {
		appMode = 1
	}
	setRegistryValue(regPath, "AppsUseLightTheme", appMode)

	// 2. 修复逻辑：如果要黑色字体，SystemUsesLightTheme 必须为 1
	targetWhite := 0
	if isLightMode {
		targetWhite = config.LightModeWhiteText
	} else {
		targetWhite = config.DarkModeWhiteText
	}

	if targetWhite == 0 {
		setRegistryValue(regPath, "SystemUsesLightTheme", 1)
		setRegistryValue(regPath, "ColorPrevalence", 0)
		logToFile("设置：任务栏黑色字体 (SystemMode=1, ColorPrevalence=0)")
	} else {
		sysMode := uint32(0)
		if isLightMode {
			sysMode = 1
		}
		setRegistryValue(regPath, "SystemUsesLightTheme", sysMode)
		setRegistryValue(regPath, "ColorPrevalence", 1)
		logToFile("设置：任务栏白色字体")
	}

	refreshTheme()
	return nil
}

// --- 辅助功能函数 ---

func logToFile(message string) {
	if config.EnableLogging != 1 {
		return
	}
	logMutex.Lock()
	defer logMutex.Unlock()

	exePath, _ := os.Executable()
	logPath := filepath.Join(filepath.Dir(exePath), "theme_switcher.log")
	file, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		file, _ = os.OpenFile(filepath.Join(os.TempDir(), "theme_switcher.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	}
	if file != nil {
		defer file.Close()
		logTime := time.Now().Format("2006-01-02 15:04:05")
		file.WriteString(fmt.Sprintf("[%s] %s\n", logTime, message))
	}
}

func isAdmin() bool {
	currentProcess, _, _ := procGetCurrentProcess.Call()
	var token syscall.Handle
	ret, _, _ := procOpenProcessToken.Call(currentProcess, uintptr(TOKEN_QUERY), uintptr(unsafe.Pointer(&token)))
	if ret == 0 {
		return false
	}
	defer syscall.CloseHandle(token)
	var elevation uint32
	var returnLen uint32
	procGetTokenInformation.Call(uintptr(token), uintptr(TokenElevation), uintptr(unsafe.Pointer(&elevation)), uintptr(unsafe.Sizeof(elevation)), uintptr(unsafe.Pointer(&returnLen)))
	return elevation != 0
}

func runAsAdmin() {
	exePath, _ := os.Executable()
	verbPtr, _ := syscall.UTF16PtrFromString("runas")
	exePtr, _ := syscall.UTF16PtrFromString(exePath)
	argPtr, _ := syscall.UTF16PtrFromString("--scheduled")
	modshell32 := syscall.NewLazyDLL("shell32.dll")
	procShellExecuteW := modshell32.NewProc("ShellExecuteW")
	procShellExecuteW.Call(0, uintptr(unsafe.Pointer(verbPtr)), uintptr(unsafe.Pointer(exePtr)), uintptr(unsafe.Pointer(argPtr)), 0, SW_HIDE)
}

func loadConfig() {
	exePath, _ := os.Executable()
	configPath := filepath.Join(filepath.Dir(exePath), "config.json")
	config = defaultConfig
	data, err := os.ReadFile(configPath)
	if err == nil {
		json.Unmarshal(data, &config)
	} else {
		saveConfig()
	}
}

func saveConfig() {
	exePath, _ := os.Executable()
	configPath := filepath.Join(filepath.Dir(exePath), "config.json")
	data, _ := json.MarshalIndent(config, "", "  ")
	os.WriteFile(configPath, data, 0644)
}

func setupScheduledTask() error {
	exePath, _ := filepath.Abs(os.Args[0])
	exeDir := filepath.Dir(exePath)
	taskName := "WindowsThemeAutoSwitcher"

	psScript := fmt.Sprintf(`
$action = New-ScheduledTaskAction -Execute "%s" -Argument "--scheduled" -WorkingDirectory "%s"
$triggers = @()
$triggers += New-ScheduledTaskTrigger -Daily -At "%02d:00"
$triggers += New-ScheduledTaskTrigger -Daily -At "%02d:00"
$triggers += New-ScheduledTaskTrigger -AtLogon
$principal = New-ScheduledTaskPrincipal -UserId (Get-CimInstance –ClassName Win32_ComputerSystem).UserName -RunLevel Highest
Register-ScheduledTask -TaskName "%s" -Action $action -Trigger $triggers -Principal $principal -Force
`, strings.ReplaceAll(exePath, `\`, `\\`), strings.ReplaceAll(exeDir, `\`, `\\`), config.LightTimeStart, config.DarkTimeStart, taskName)

	cmd := exec.Command("powershell", "-Command", psScript)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Run()
}

func performSingleSwitch() error {
	switchMutex.Lock()
	defer switchMutex.Unlock()
	now := time.Now()
	hour := now.Hour()
	isLight := false
	if config.LightTimeStart < config.DarkTimeStart {
		isLight = hour >= config.LightTimeStart && hour < config.DarkTimeStart
	} else {
		isLight = hour >= config.LightTimeStart || hour < config.DarkTimeStart
	}
	return applyThemeLogic(isLight)
}

func main() {
	consoleWindow, _, _ := procGetConsoleWindow.Call()
	if consoleWindow != 0 {
		procShowWindow.Call(consoleWindow, SW_HIDE)
	}

	loadConfig()

	if len(os.Args) > 1 && os.Args[1] == "--scheduled" {
		performSingleSwitch()
		return
	}

	if !isAdmin() {
		runAsAdmin()
		return
	}

	setupScheduledTask()
	performSingleSwitch()
	time.Sleep(2 * time.Second)
}