//go:build !windows

package nocontext

import (
	"context"
	"os/exec"
	"syscall"
)

func nocontextSilentProcessGroupKill(ctx context.Context) error {
	cmd := exec.Command("sleep", "10")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-ctx.Done():
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
		return ctx.Err()
	case err := <-done:
		return err
	}
}
