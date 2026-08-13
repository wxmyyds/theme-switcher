package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"themeswitcher/config"
	"themeswitcher/logger"
	"themeswitcher/scheduler"
	"themeswitcher/theme"
)

const shellExecuteSuccessThreshold = 32

const version = "3.0.4"

var (
	exePath    string
	configPath string
	logPath    string
	log        *logger.Logger
	cfg        *config.Config
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

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Printf("Theme Switcher %s\n", version)
		return
	}

	hideConsoleWindow()

	var loadErr error
	cfg, loadErr = config.Load(configPath)

	log = logger.New(logPath, cfg.EnableLogging)

	if loadErr != nil {
		log.Log("配置加载警告: %v", loadErr)
	}

	if len(os.Args) > 1 && os.Args[1] == "--scheduled" {
		if err := performSwitch(); err != nil {
			log.Log("计划任务执行失败: %v", err)
			os.Exit(1)
		}
		return
	}

	if !isAdmin() {
		if err := runAsAdmin(); err != nil {
			log.Log("提升权限失败: %v", err)
			fmt.Fprintln(os.Stderr, "需要管理员权限运行，请手动以管理员身份运行此程序")
			os.Exit(1)
		}
		return
	}

	if err := scheduler.Setup(exePath, cfg.LightTimeStart, cfg.DarkTimeStart); err != nil {
		log.Log("计划任务设置失败: %v", err)
		fmt.Fprintf(os.Stderr, "计划任务设置失败: %v\n", err)
	}

	if err := performSwitch(); err != nil {
		log.Log("初始主题切换失败: %v", err)
		fmt.Fprintf(os.Stderr, "主题切换失败: %v\n", err)
		os.Exit(1)
	}

	time.Sleep(1 * time.Second)
}

func performSwitch() error {
	now := time.Now()
	isLight := theme.ShouldUseLightMode(now.Hour(), cfg.LightTimeStart, cfg.DarkTimeStart)

	log.Log("执行主题切换: 时间=%s, 小时=%d, 浅色模式=%v",
		now.Format("15:04:05"), now.Hour(), isLight)

	if err := theme.Apply(isLight, cfg.LightModeWhiteText, cfg.DarkModeWhiteText); err != nil {
		log.Log("主题切换失败: %v", err)
		return err
	}

	log.Log("主题切换成功")
	return nil
}

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
	var encoded []string
	for _, arg := range os.Args[1:] {
		encoded = append(encoded, encodeArg(arg))
	}
	argPtr, err := windows.UTF16PtrFromString(strings.Join(encoded, " "))
	if err != nil {
		return fmt.Errorf("创建参数字符串失败: %w", err)
	}

	shell32 := windows.NewLazySystemDLL("shell32.dll")
	procShellExecute := shell32.NewProc("ShellExecuteW")

	ret, _, _ := procShellExecute.Call(
		0,
		uintptr(unsafe.Pointer(verbPtr)),
		uintptr(unsafe.Pointer(exePtr)),
		uintptr(unsafe.Pointer(argPtr)),
		0,
		0,
	)
	if ret <= shellExecuteSuccessThreshold {
		return fmt.Errorf("ShellExecute失败，返回值: %d", ret)
	}
	return nil
}

func hideConsoleWindow() {
	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	user32 := windows.NewLazySystemDLL("user32.dll")
	procGetConsoleWindow := kernel32.NewProc("GetConsoleWindow")
	procShowWindow := user32.NewProc("ShowWindow")

	consoleWindow, _, _ := procGetConsoleWindow.Call()
	if consoleWindow != 0 {
		procShowWindow.Call(consoleWindow, 0)
	}
}

func encodeArg(arg string) string {
	if arg == "" {
		return `""`
	}
	needsQuoting := false
	for _, c := range arg {
		if c == ' ' || c == '\t' || c == '"' {
			needsQuoting = true
			break
		}
	}
	if !needsQuoting {
		return arg
	}
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(arg); i++ {
		c := arg[i]
		if c == '\\' {
			backslashCount := 0
			for ; i < len(arg) && arg[i] == '\\'; i++ {
				backslashCount++
			}
			i--
			if i+1 < len(arg) && arg[i+1] == '"' {
				b.WriteString(strings.Repeat(`\\`, backslashCount))
			} else {
				b.WriteString(strings.Repeat(`\`, backslashCount))
			}
		} else if c == '"' {
			b.WriteString(`\"`)
		} else {
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}