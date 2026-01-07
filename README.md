# Windows 主题自动切换器

一个用于 Windows 11 的自动深浅色主题切换工具，根据时间自动切换系统主题模式。

## 🤖 AI 声明

本项目的主要代码（约 95%）由 **AI 助手** 生成和优化，包括：
- Windows API 调用和注册表操作
- 系统主题切换逻辑
- 日志记录系统
- 错误处理机制
- 程序架构设计

## 功能特点

- 🌓 自动在浅色模式和深色模式之间切换
- ⏰ 每天 6:00 自动切换到浅色模式
- 🌙 每天 18:00 自动切换到深色模式
- 📝 自动记录运行日志
- 🚫 无界面后台运行
- ⚡ 静默刷新，不弹窗不干扰

## 技术原理

通过修改以下注册表键值实现主题切换：
- `AppsUseLightTheme` - 应用程序主题
- `SystemUsesLightTheme` - 系统主题  
- `ColorPrevalence` - 任务栏字体颜色

## 使用方法

### 1. 下载使用
- 从 Releases 页面下载 `theme-switcher.exe`
- **重要**：必须以管理员身份运行！

### 2. 自行构建
```bash
# 克隆项目
git clone https://github.com/wxmyyds/theme-switcher.git
cd theme-switcher

# 构建程序
go build -ldflags="-H windowsgui" -o theme-switcher.exe