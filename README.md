# Windows 主题自动切换器

一个在 Windows 上根据设定时间自动切换浅/深色主题的轻量工具。

**要点**
- 使用注册表（`HKCU\Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`）进行系统主题切换；目前没有公开的 WinRT API 可改变系统全局主题。
- 会（在具备管理员权限时）自动为当前用户创建计划任务，默认触发时间为每天 `06:00` 和 `18:00`，并在登录时触发一次。
- 可通过 `config.json` 配置浅/深色时间与任务栏字体颜色偏好。

## 系统要求

- Windows 10 / Windows 11
- 管理员权限（首次创建计划任务需要提升）

## 快速开始

1. 下载或在源码目录编译：

```bash
go build -ldflags="-H windowsgui -s -w" -trimpath -o ThemeSwitcher.exe
```

2. 以管理员身份运行生成的 `ThemeSwitcher.exe`（首次运行会提示提升）：

```powershell
.\\ThemeSwitcher.exe
```

3. 常用参数：

- `--version`：打印版本并退出
- `--scheduled`：由计划任务调用以执行一次切换（内部使用）

首次运行行为：

- 在可写目录创建 `config.json`（若不存在）
- 创建计划任务（如果有管理员权限）并立即执行一次主题切换

日志：默认写入同目录的 `theme_switcher.log`（可在 `config.json` 中关闭）

## 配置

配置文件 `config.json`（与可执行文件相同目录）示例：

```json
{
  "light_mode_white_text": true,
  "dark_mode_white_text": true,
  "light_time_start": 6,
  "dark_time_start": 18,
  "enable_logging": true
}
```

字段说明：

- `light_mode_white_text` (bool)：浅色模式下是否使用白色任务栏文字
- `dark_mode_white_text` (bool)：深色模式下是否使用白色任务栏文字
- `light_time_start` (int)：切换到浅色模式的起始小时（0-23）
- `dark_time_start` (int)：切换到深色模式的起始小时（0-23）
- `enable_logging` (bool)：是否记录运行日志

> **注意**：如果 `config.json` 存在格式错误（如语法错误、编辑中断导致截断），程序会使用默认配置运行，但**不会覆盖原文件**。请根据日志中"配置文件解析失败"的提示修复文件后重新运行。

## 故障排查

- 如果计划任务创建失败，请检查 `theme_switcher.log` 中关于“计划任务设置失败”的条目并贴出 PowerShell 输出以便排查。
- 如果不想自动创建计划任务，可手动运行可执行文件并在任务计划程序中创建等效任务，参数为 `--scheduled`。

### 卸载

删除程序后，需手动移除计划任务，否则系统会持续尝试执行不存在的程序：

1. 打开任务计划程序（`taskschd.msc`）
2. 在任务计划程序库中找到 `WindowsThemeAutoSwitcher`
3. 右键 → 删除

或使用命令行（管理员）：

```powershell
Unregister-ScheduledTask -TaskName WindowsThemeAutoSwitcher -Confirm:$false
```
