package theme

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	regPersonalize = `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`

	hwndBroadcast    = 0xFFFF
	wmSettingChange  = 0x001A
	wmDWMColorChange = 0x0320
	smtoAbortIfHung  = 0x0002
	sendTimeoutMs    = 3000
)

var (
	user32                 = windows.NewLazySystemDLL("user32.dll")
	procSendMessageTimeout = user32.NewProc("SendMessageTimeoutW")
)

// ShouldUseLightMode 判断给定小时应使用的主题。
//
// lightStart 与 darkStart 相等是无效配置：若直接进入下面的跨界分支，
// `hour >= lightStart || hour < darkStart` 会恒为 true，深色模式永不生效。
// 这里兜底成"以 lightStart 起算的 12 小时窗口"，与 config.Validate 的
// 纠正规则保持一致，保证任何输入下两种模式都可达。
func ShouldUseLightMode(hour, lightStart, darkStart int) bool {
	if lightStart == darkStart {
		darkStart = (lightStart + 12) % 24
	}
	if lightStart < darkStart {
		return hour >= lightStart && hour < darkStart
	}
	return hour >= lightStart || hour < darkStart
}

// Apply 把主题写入注册表。
//
// 只写入 AppsUseLightTheme 与 SystemUsesLightTheme，不会修改 ColorPrevalence
// ——后者对应"在标题栏和任务栏上显示强调色"，属于用户独立的系统偏好，
// 强行写 0 会静默破坏用户设置且无法恢复。
//
// 写入成功即视为切换成功；界面是否即时刷新由 Refresh 负责，两者的失败
// 语义不同，不应混为一谈。
func Apply(isLight bool, lightWhiteText, darkWhiteText bool) error {
	appMode := uint32(0)
	if isLight {
		appMode = 1
	}

	k, err := registry.OpenKey(registry.CURRENT_USER, regPersonalize, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("打开注册表键失败: %w", err)
	}
	defer k.Close()

	if err := k.SetDWordValue("AppsUseLightTheme", appMode); err != nil {
		return fmt.Errorf("设置应用主题失败: %w", err)
	}

	useWhiteText := darkWhiteText
	if isLight {
		useWhiteText = lightWhiteText
	}

	sysMode := uint32(1)
	if useWhiteText {
		sysMode = 0
	}

	if err := k.SetDWordValue("SystemUsesLightTheme", sysMode); err != nil {
		return fmt.Errorf("设置系统主题失败: %w", err)
	}
	return nil
}

// Refresh 广播主题变更通知，让已运行的窗口重新读取主题设置。
//
// 失败通常只意味着部分应用没有即时重绘（资源管理器一般会在下一次重绘或
// 重启后生效），因此调用方应记录警告，而不要把它判定为切换失败。
func Refresh() error {
	for _, change := range []string{"ImmersiveColorSet", "Policy"} {
		lParamPtr, err := windows.UTF16PtrFromString(change)
		if err != nil {
			return err
		}
		if err := broadcast(wmSettingChange, lParamPtr); err != nil {
			return fmt.Errorf("广播消息 '%s' 失败: %w", change, err)
		}
	}

	lParamPtr, err := windows.UTF16PtrFromString("")
	if err != nil {
		return err
	}
	if err := broadcast(wmDWMColorChange, lParamPtr); err != nil {
		return fmt.Errorf("广播 DWM 颜色变更通知失败: %w", err)
	}
	return nil
}

func broadcast(msg uintptr, lParam *uint16) error {
	// result 是 Windows 的输出参数，调用期间不会被 Go 运行时移动
	// （Go 的 GC 不移动堆对象，且 SyscallN 会固定传入的指针）。
	// x/sys/windows 未导出 SendMessageTimeout 封装，只能自行调用。
	var result uintptr
	ret, _, err := procSendMessageTimeout.Call(
		uintptr(hwndBroadcast),
		msg,
		0,
		uintptr(unsafe.Pointer(lParam)),
		uintptr(smtoAbortIfHung),
		uintptr(sendTimeoutMs),
		uintptr(unsafe.Pointer(&result)),
	)
	if ret != 0 {
		return nil
	}
	// 注意：proc.Call 返回的 error 在成功时也是非 nil（值为 Errno(0)），
	// 必须显式排除，不能直接判空。
	if errno, ok := err.(syscall.Errno); ok && errno != 0 {
		return errno
	}
	return errors.New("广播超时或被中止")
}
