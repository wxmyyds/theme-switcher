# Windows 主题自动切换器

一个在 Windows 上根据设定时间自动切换浅/深色主题的轻量工具。

**要点**
- 使用注册表（`HKCU\Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`）进行系统主题切换；目前没有公开的 WinRT API 可改变系统全局主题。
- 只写入 `AppsUseLightTheme` 和 `SystemUsesLightTheme` 两个值，**不会**修改 `ColorPrevalence`，因此不会影响你在"于标题栏和任务栏显示强调色"上的个人偏好。
- 会（在具备管理员权限时）自动为当前用户创建计划任务，默认触发时间为每天 `06:00` 和 `18:00`，并在登录时触发一次。
- 每次运行都会核对计划任务的描述是否与当前配置一致，一致则跳过注册，不会重复创建。

## 系统要求

- Windows 10 / Windows 11
- 管理员权限（首次创建计划任务需要提升）

## 快速开始

1. 下载或在源码目录编译：

```bash
go build -ldflags="-H windowsgui -s -w -X main.version=3.0.5" -trimpath -o ThemeSwitcher.exe
```

2. 以管理员身份运行生成的 `ThemeSwitcher.exe`（首次运行会提示提升）：

```powershell
.\\ThemeSwitcher.exe
```

3. 常用参数：

- `--version`：打印版本并退出
- `--scheduled`：由计划任务调用以执行一次切换（内部使用）
- `--uninstall`：删除计划任务后退出（删除程序前请先执行）

首次运行行为：

- 在**可执行文件所在目录**创建 `config.json`（若不存在）
- 创建计划任务（如果有管理员权限）并立即执行一次主题切换

日志：默认写入**可执行文件所在目录**的 `theme_switcher.log`（可在 `config.json` 中关闭）

> 注意：若安装到 `C:\Program Files\` 等受保护目录，创建配置文件与写入日志都需要管理员权限。
> 建议把程序放在普通用户目录（如 `%LOCALAPPDATA%\ThemeSwitcher`）下；若必须放在 Program Files，
> 请以管理员身份运行，或确认当前用户对程序目录有写入权限。

## 配置

配置文件 `config.json`（与可执行文件相同目录）示例，内容与内置默认值一致：

```json
{
  "light_mode_white_text": false,
  "dark_mode_white_text": true,
  "light_time_start": 6,
  "dark_time_start": 18,
  "enable_logging": true
}
```

> 首次运行时若 `config.json` 不存在会自动创建；该文件不会被提交到版本库，
> 默认值以代码中的 `config.DefaultConfig()` 为准。

字段说明：

- `light_mode_white_text` (bool)：浅色模式下是否使用白色任务栏文字
- `dark_mode_white_text` (bool)：深色模式下是否使用白色任务栏文字
- `light_time_start` (int)：切换到浅色模式的起始小时（0-23）
- `dark_time_start` (int)：切换到深色模式的起始小时（0-23）
- `enable_logging` (bool)：是否记录运行日志

时间字段接受 `6`、`"6"`、`6.0` 三种写法；缺失的字段会使用默认值。

配置校验规则（被自动纠正的项会记入日志）：

- 小时超出 0-23：回退为默认值（浅色 6、深色 18）
- 两个起始时间相同：深色起始时间自动改为 `(浅色起始 + 12) % 24`，
  否则深色模式永远不会生效

> **注意**：如果 `config.json` 存在格式错误（如语法错误、编辑中断导致截断），程序会使用默认配置运行，
> 且**不会覆盖原文件、也不会重建计划任务**——以免用默认值覆盖你的自定义时间。
> 请根据日志中"配置错误"的提示修复文件后重新运行。

## 故障排查

- 如果计划任务创建失败，请检查 `theme_switcher.log` 中关于"计划任务设置失败"的条目。
- 若看到"界面刷新广播失败"警告：主题已成功写入注册表，只是部分窗口没有即时重绘，资源管理器重启后即会生效，不影响功能。
- 如果不想自动创建计划任务，可手动运行可执行文件并在任务计划程序中创建等效任务，参数为 `--scheduled`。

## 卸载

删除程序前请先移除计划任务，否则系统会持续尝试执行不存在的程序：

```powershell
.\ThemeSwitcher.exe --uninstall
```

或手动操作：

1. 打开任务计划程序（`taskschd.msc`）
2. 在任务计划程序库中找到 `WindowsThemeAutoSwitcher`
3. 右键 → 删除

或使用命令行（管理员）：

```powershell
Unregister-ScheduledTask -TaskName WindowsThemeAutoSwitcher -Confirm:$false
```

## 开发

需要 Go 1.25+ 与 Windows 环境（依赖 `golang.org/x/sys/windows`）。

```bash
go vet ./...
go build -ldflags="-H windowsgui -s -w -X main.version=3.0.5" -trimpath -o ThemeSwitcher.exe
```
