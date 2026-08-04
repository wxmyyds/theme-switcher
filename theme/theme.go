package theme

import (
	"fmt"
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
	user32               = windows.NewLazySystemDLL("user32.dll")
	procSendMessageTimeout = user32.NewProc("SendMessageTimeoutW")
)

func ShouldUseLightMode(hour int, lightStart, darkStart int) bool {
	if lightStart < darkStart {
		return hour >= lightStart && hour < darkStart
	}
	return hour >= lightStart || hour < darkStart
}

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

	sysMode := appMode
	colorPrevalence := uint32(0)
	if useWhiteText {
		colorPrevalence = 1
	} else {
		sysMode = 1
	}

	if err := k.SetDWordValue("SystemUsesLightTheme", sysMode); err != nil {
		return fmt.Errorf("设置系统主题失败: %w", err)
	}
	if err := k.SetDWordValue("ColorPrevalence", colorPrevalence); err != nil {
		return fmt.Errorf("设置颜色应用失败: %w", err)
	}

	return refresh()
}

func refresh() error {
	for _, change := range []string{"ImmersiveColorSet", "Policy"} {
		lParamPtr, err := windows.UTF16PtrFromString(change)
		if err != nil {
			return err
		}
		var result uintptr
		ret, _, _ := procSendMessageTimeout.Call(
			uintptr(hwndBroadcast),
			uintptr(wmSettingChange),
			0,
			uintptr(unsafe.Pointer(lParamPtr)),
			uintptr(smtoAbortIfHung),
			uintptr(sendTimeoutMs),
			uintptr(unsafe.Pointer(&result)),
		)
		if ret == 0 {
			return fmt.Errorf("广播消息 '%s' 失败", change)
		}
	}

	lParamPtr, _ := windows.UTF16PtrFromString("")
	var result uintptr
	ret, _, _ := procSendMessageTimeout.Call(
		uintptr(hwndBroadcast),
		uintptr(wmDWMColorChange),
		0,
		uintptr(unsafe.Pointer(lParamPtr)),
		uintptr(smtoAbortIfHung),
		uintptr(sendTimeoutMs),
		uintptr(unsafe.Pointer(&result)),
	)
	if ret == 0 {
		return fmt.Errorf("广播 DWM 颜色变更通知失败")
	}
	return nil
}