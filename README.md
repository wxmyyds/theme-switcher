# Windows 主题自动切换器

[![Version](https://img.shields.io/badge/version-1.5.0-blue.svg)](https://github.com/yourusername/theme-switcher)
[![Platform](https://img.shields.io/badge/platform-Windows-brightgreen.svg)](https://github.com/yourusername/theme-switcher)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](https://github.com/yourusername/theme-switcher)

## 📖 简介

Windows主题自动切换器是一个能够根据时间自动切换Windows系统浅色/深色主题的工具。它通过修改注册表实现主题切换，并支持自定义任务栏字体颜色。

## ✨ 功能特点

- **自动切换**：根据设定的时间自动切换浅色/深色主题
- **自定义颜色**：可分别设置浅色/深色模式下的任务栏字体颜色
- **灵活配置**：通过JSON配置文件自定义所有参数
- **计划任务**：自动创建Windows计划任务，无需手动设置
- **日志记录**：可选择是否记录运行日志
- **无窗口运行**：后台静默运行，不干扰用户操作

## 📋 系统要求

- Windows 10 / Windows 11
- 管理员权限（必需）

## 🚀 快速开始

### 下载安装

1. 下载最新版本的程序包
2. 解压到任意目录（建议使用英文路径）
3. 以管理员身份运行 `theme_switcher.exe`

### 首次运行

首次运行程序会自动：
- 创建默认配置文件 `config.json`
- 创建计划任务（每天6:00和18:00自动切换）
- 立即执行一次主题切换
- 生成日志文件 `theme_switcher.log`

参数说明
light_mode_taskbar_color
类型：string

说明：设置在浅色模式下任务栏字体的颜色

可选值：

"white"：任务栏字体显示为白色

"default"：使用系统默认颜色

dark_mode_taskbar_color
类型：string

说明：设置在深色模式下任务栏字体的颜色

可选值：

"black"：任务栏字体显示为黑色

"default"：使用系统默认颜色

light_time_start
类型：int

说明：设置切换到浅色模式的时间（小时）

取值范围：0（午夜）到 23（晚上11点）

dark_time_start
类型：int

说明：设置切换到深色模式的时间（小时）

取值范围：0（午夜）到 23（晚上11点）

enable_logging
类型：bool

说明：是否将程序运行信息写入日志文件

可选值：

true：启用日志记录，生成 theme_switcher.log 文件

false：禁用日志记录

### 配置文件示例

```json
{
  "light_mode_taskbar_color": "white",
  "dark_mode_taskbar_color": "default",
  "light_time_start": 6,
  "dark_time_start": 18,
  "enable_logging": true
}
