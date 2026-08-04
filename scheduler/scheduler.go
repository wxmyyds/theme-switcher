package scheduler

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func Setup(exePath string, lightTime, darkTime int) error {
	taskName := "WindowsThemeAutoSwitcher"
	exeDir := filepath.Dir(exePath)

	psCommand := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
try {
    $userId = "$env:USERDOMAIN\$env:USERNAME"
    $action = New-ScheduledTaskAction -Execute '%s' -Argument '--scheduled' -WorkingDirectory '%s'
    $triggers = @()
    $triggers += New-ScheduledTaskTrigger -Daily -At %02d:00
    $triggers += New-ScheduledTaskTrigger -Daily -At %02d:00
    $triggers += New-ScheduledTaskTrigger -AtLogon
    $principal = New-ScheduledTaskPrincipal -UserId $userId -LogonType Interactive -RunLevel Highest
    $settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -StartWhenAvailable -MultipleInstances IgnoreNew
    Register-ScheduledTask -TaskName '%s' -Action $action -Trigger $triggers -Principal $principal -Settings $settings -Force
    Write-Output '计划任务注册成功'
} catch {
    Write-Error "计划任务注册失败: $_"
    exit 1
}`,
		strings.ReplaceAll(exePath, "'", "''"),
		strings.ReplaceAll(exeDir, "'", "''"),
		lightTime,
		darkTime,
		taskName,
	)

	cmd := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", psCommand)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("计划任务设置失败: %w, 输出: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}