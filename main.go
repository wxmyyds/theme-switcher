package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"themeswitcher/config"
	"themeswitcher/logger"
	"themeswitcher/scheduler"
	"themeswitcher/theme"
)

const (
	// ERROR_CANCELLED：用户在 UAC/凭据提示中选择了取消。
	errorCancelled = 1223

	swHide       = 0
	swShowNormal = 1
)

// errElevationCancelled 表示用户主动取消了提权，与"权限不足"区别对待。
var errElevationCancelled = errors.New("用户取消了权限提升")

// shellExecuteInfo 对应 Win32 SHELLEXECUTEINFOW。
// x/sys/windows 未导出该结构，这里自行定义；字段顺序与类型必须严格匹配，
// 布局在 amd64 下为 112 字节（已用 unsafe.Offsetof 校验）。
type shellExecuteInfo struct {
	cbSize         uint32
	fMask          uint32
	hwnd           windows.Handle
	lpVerb         *uint16
	lpFile         *uint16
	lpParameters   *uint16
	lpDirectory    *uint16
	nShow          int32
	hInstApp       windows.Handle
	lpIDList       uintptr
	lpClass        *uint16
	hKeyClass      windows.Handle
	dwHotKey       uint32
	hIconOrMonitor windows.Handle
	hProcess       windows.Handle
}

var version = "3.0.4"

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

// main 只负责把退出码交给操作系统，实际逻辑放在 run 里，
// 保证 defer（如关闭日志文件）总能执行——直接用 os.Exit 会跳过它们。
func main() {
	os.Exit(run())
}

func run() int {
	if hasArg("--version") {
		fmt.Printf("Theme Switcher %s\n", version)
		return 0
	}

	hideConsoleWindow()

	var loadErr error
	cfg, loadErr = config.Load(configPath)

	log = logger.New(logPath, cfg.EnableLogging)
	defer log.Close()

	// 三种配置状态需要区别对待：
	//   - 被自动纠正：配置可用，记警告后继续；
	//   - 解析失败：cfg 里是默认值，绝不能据此重建计划任务，
	//     否则会把用户自定义的时间永久重置为默认值；
	//   - 其它：文件读取或创建失败，不影响使用默认值继续。
	syncTask := true
	switch {
	case loadErr == nil:
	case errors.Is(loadErr, config.ErrAdjusted):
		log.Log("配置警告: %v", loadErr)
	case errors.Is(loadErr, config.ErrParseFailed):
		log.Log("配置错误: %v；本次跳过计划任务同步，配置文件保持原样", loadErr)
		syncTask = false
	default:
		log.Log("配置加载警告: %v", loadErr)
	}

	// 由计划任务调用：先同步任务，保证即使本次切换失败，
	// 配置里的时间改动也能生效。
	// 注意这一分支**不**做提权：计划任务本身已配置为最高权限运行，
	// 后台触发时不该再弹 UAC。
	if hasArg("--scheduled") {
		if syncTask {
			if err := scheduler.Setup(exePath, cfg.LightTimeStart, cfg.DarkTimeStart); err != nil {
				log.Log("同步计划任务失败: %v", err)
			}
		}
		if err := performSwitch(); err != nil {
			log.Log("计划任务执行失败: %v", err)
			return 1
		}
		return 0
	}

	// 其余分支（安装、卸载）都需要管理员权限，统一在此提权。
	// 提权时会带上原始参数，因此 --uninstall 在提升后的进程中依然有效。
	if !isAdmin() {
		if err := runAsAdmin(); err != nil {
			log.Log("提升权限失败: %v", err)
			if errors.Is(err, errElevationCancelled) {
				showError("未获得管理员权限，程序将退出。")
			} else {
				showError("需要管理员权限运行，请手动以管理员身份运行此程序。\n\n原因: %v", err)
			}
			return 1
		}
		return 0
	}

	if hasArg("--uninstall") {
		if err := scheduler.Uninstall(); err != nil {
			log.Log("卸载计划任务失败: %v", err)
			showError("卸载计划任务失败:\n%v", err)
			return 1
		}
		log.Log("计划任务已卸载")
		showInfo("计划任务已删除，可以安全删除本程序。")
		return 0
	}

	if syncTask {
		if err := scheduler.Setup(exePath, cfg.LightTimeStart, cfg.DarkTimeStart); err != nil {
			log.Log("计划任务设置失败: %v", err)
			showError("计划任务设置失败:\n%v", err)
		}
	} else {
		showError("配置文件解析失败，已跳过计划任务设置。\n请修复以下文件后重新运行：\n%s\n\n%v",
			configPath, loadErr)
	}

	if err := performSwitch(); err != nil {
		log.Log("初始主题切换失败: %v", err)
		showError("主题切换失败:\n%v", err)
		return 1
	}
	return 0
}

// performSwitch 只在注册表写入失败时返回错误。
func performSwitch() error {
	now := time.Now()
	isLight := theme.ShouldUseLightMode(now.Hour(), cfg.LightTimeStart, cfg.DarkTimeStart)

	log.Log("执行主题切换: 时间=%s, 小时=%d, 浅色模式=%v",
		now.Format("15:04:05"), now.Hour(), isLight)

	if err := theme.Apply(isLight, cfg.LightModeWhiteText, cfg.DarkModeWhiteText); err != nil {
		log.Log("主题切换失败: %v", err)
		return err
	}

	// 主题已经写入注册表，广播失败只影响部分窗口的即时刷新，不算切换失败。
	if err := theme.Refresh(); err != nil {
		log.Log("警告: 界面刷新广播失败（主题已写入注册表）: %v", err)
	}

	log.Log("主题切换成功")
	return nil
}

// hasArg 判断命令行中是否出现指定参数。
// 只检查 os.Args[1] 会漏掉 `prog.exe --foo --scheduled` 这种写法。
func hasArg(name string) bool {
	for _, arg := range os.Args[1:] {
		if arg == name {
			return true
		}
	}
	return false
}

func isAdmin() bool {
	token := windows.GetCurrentProcessToken()
	defer token.Close()
	return token.IsElevated()
}

func runAsAdmin() error {
	verbPtr, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return fmt.Errorf("创建 runas 字符串失败: %w", err)
	}
	exePtr, err := windows.UTF16PtrFromString(exePath)
	if err != nil {
		return fmt.Errorf("创建路径字符串失败: %w", err)
	}
	var encoded []string
	for _, arg := range os.Args[1:] {
		encoded = append(encoded, syscall.EscapeArg(arg))
	}
	argPtr, err := windows.UTF16PtrFromString(strings.Join(encoded, " "))
	if err != nil {
		return fmt.Errorf("创建参数字符串失败: %w", err)
	}

	shell32 := windows.NewLazySystemDLL("shell32.dll")
	// 用 ShellExecuteEx 而不是 ShellExecute：后者的返回值无法可靠区分
	// "用户取消 UAC"（常见为 ERROR_CANCELLED，值大于 32，会被误判为成功）。
	// ShellExecuteEx 在失败时返回 FALSE 并可通过 GetLastError 拿到确切原因。
	procShellExecuteEx := shell32.NewProc("ShellExecuteExW")

	info := shellExecuteInfo{
		lpVerb:       verbPtr,
		lpFile:       exePtr,
		lpParameters: argPtr,
		nShow:        swShowNormal,
	}
	info.cbSize = uint32(unsafe.Sizeof(info))

	ret, _, errno := procShellExecuteEx.Call(uintptr(unsafe.Pointer(&info)))
	if ret == 0 {
		// proc.Call 返回的 error 在成功时也是非 nil（值为 Errno(0)），
		// 必须显式排除，不能直接判空。
		var lastErr error
		if en, ok := errno.(syscall.Errno); ok && en != 0 {
			lastErr = en
		}
		if lastErr == nil {
			lastErr = errors.New("未知错误")
		}
		if en, ok := lastErr.(syscall.Errno); ok && uint32(en) == errorCancelled {
			return errElevationCancelled
		}
		return lastErr
	}
	return nil
}

func hideConsoleWindow() {
	kernel32 := windows.NewLazySystemDLL("kernel32.dll")
	user32 := windows.NewLazySystemDLL("user32.dll")
	procGetConsoleWindow := kernel32.NewProc("GetConsoleWindow")
	procGetConsoleProcessList := kernel32.NewProc("GetConsoleProcessList")
	procShowWindow := user32.NewProc("ShowWindow")

	consoleWindow, _, _ := procGetConsoleWindow.Call()
	if consoleWindow == 0 {
		return
	}

	// 从 cmd / PowerShell 启动时，本进程与宿主共享同一个控制台，
	// 直接隐藏会把用户自己的终端窗口一起藏掉。
	// 只有当控制台被本进程独占（进程数为 1）时才隐藏。
	var pids [2]uint32
	ret, _, _ := procGetConsoleProcessList.Call(
		uintptr(unsafe.Pointer(&pids[0])),
		uintptr(len(pids)),
	)
	if count := uint32(ret); count == 0 || count > 1 {
		return
	}

	procShowWindow.Call(consoleWindow, uintptr(swHide))
}

// showError / showInfo 用消息框提示用户。
// 程序以 windowsgui 子系统构建，不存在可用控制台，
// 写到 os.Stderr 的内容用户永远看不到。
func showError(format string, args ...interface{}) {
	messageBox(format, args, windows.MB_OK|windows.MB_ICONERROR|windows.MB_TOPMOST)
}

func showInfo(format string, args ...interface{}) {
	messageBox(format, args, windows.MB_OK|windows.MB_ICONINFORMATION|windows.MB_TOPMOST)
}

func messageBox(format string, args []interface{}, flags uint32) {
	msg, err := windows.UTF16PtrFromString(fmt.Sprintf(format, args...))
	if err != nil {
		return
	}
	caption, err := windows.UTF16PtrFromString("主题切换器")
	if err != nil {
		return
	}
	windows.MessageBox(0, msg, caption, flags)
}
