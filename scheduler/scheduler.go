package scheduler

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// TaskName 是注册到任务计划程序中的任务名，卸载时使用同一个名字。
const TaskName = "WindowsThemeAutoSwitcher"

// signature 记录了本次注册的完整意图（时间、程序路径，外加运行时追加的当前用户）。
// 把它写进任务的 Description，下次运行时即可据此判断是否需要重新注册，
// 避免每次定时触发都执行一次重量级的 Register-ScheduledTask。
const signatureFmt = "themeswitcher|v1|light=%02d|dark=%02d|exe=%s"

// 占位符按出现顺序排列：任务名、签名、程序路径、工作目录、浅色小时、深色小时。
// 注意 Go 的 fmt 要求显式索引紧贴动词（%[n]verb），不支持 %[n]02d，
// 因此这里统一使用顺序占位符。
const setupScript = `$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
$taskName = '%s'
$userId = "$env:USERDOMAIN\$env:USERNAME"
# 路径部分用单引号原样嵌入以避免被展开，用户名单独拼接以取到真实值。
$signature = '%s' + "|user=$userId"

try {
    $existing = Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
} catch {
    $existing = $null
}
if ($existing -and $existing.Description -eq $signature) {
    Write-Output '计划任务已是最新，跳过注册'
    exit 0
}

try {
    $action = New-ScheduledTaskAction -Execute '%s' -Argument '--scheduled' -WorkingDirectory '%s'
    $triggers = @()
    $triggers += New-ScheduledTaskTrigger -Daily -At %02d:00
    $triggers += New-ScheduledTaskTrigger -Daily -At %02d:00
    $triggers += New-ScheduledTaskTrigger -AtLogon -User $userId
    $principal = New-ScheduledTaskPrincipal -UserId $userId -LogonType Interactive -RunLevel Highest
    $settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -StartWhenAvailable -MultipleInstances IgnoreNew
    Register-ScheduledTask -TaskName $taskName -Action $action -Trigger $triggers -Principal $principal -Settings $settings -Description $signature -Force
    Write-Output '计划任务注册成功'
} catch {
    Write-Error "计划任务注册失败: $_"
    exit 1
}`

const uninstallScript = `$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
$taskName = '%s'

try {
    $existing = Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
} catch {
    $existing = $null
}
if (-not $existing) {
    Write-Output '计划任务不存在，无需卸载'
    exit 0
}

try {
    Unregister-ScheduledTask -TaskName $taskName -Confirm:$false
    Write-Output '计划任务已删除'
} catch {
    Write-Error "删除计划任务失败: $_"
    exit 1
}`

// Setup 创建或更新计划任务。它总是幂等的：若现有任务的 Description 与
// 期望配置完全一致，则直接返回，不会重复注册。
func Setup(exePath string, lightTime, darkTime int) error {
	if lightTime < 0 || lightTime > 23 || darkTime < 0 || darkTime > 23 {
		return fmt.Errorf("触发时间非法: light=%d, dark=%d（应为 0-23）", lightTime, darkTime)
	}

	if err := runPowerShell(buildSetupScript(exePath, lightTime, darkTime)); err != nil {
		return fmt.Errorf("计划任务设置失败: %w", err)
	}
	return nil
}

// buildSetupScript 生成注册用的 PowerShell 脚本。
// 与执行动作分开，便于单独检视脚本内容而无需真的去注册任务。
func buildSetupScript(exePath string, lightTime, darkTime int) string {
	exeDir := filepath.Dir(exePath)
	signature := fmt.Sprintf(signatureFmt, lightTime, darkTime, exePath)

	return fmt.Sprintf(setupScript,
		escapePS(TaskName),
		escapePS(signature),
		escapePS(exePath),
		escapePS(exeDir),
		lightTime,
		darkTime,
	)
}

// Uninstall 删除已注册的计划任务；任务不存在时视为成功。
func Uninstall() error {
	script := fmt.Sprintf(uninstallScript, escapePS(TaskName))
	if err := runPowerShell(script); err != nil {
		return fmt.Errorf("删除计划任务失败: %w", err)
	}
	return nil
}

func runPowerShell(script string) error {
	cmd := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w, 输出: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// escapePS 转义 PowerShell 单引号字符串：内部用两个连续单引号表示一个字面单引号。
// 路径虽来自 os.Executable()，但目录名可能包含单引号，仍需转义。
func escapePS(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
