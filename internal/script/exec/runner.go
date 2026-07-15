// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

// Package exec 提供脚本命令执行封装。
package exec

import (
	"context"
	"fmt"
	"os/exec"
)

// Run 通过 sh -c 执行 command，把 userEnvs 追加到进程环境变量中。
// 返回合并的 stdout+stderr 或错误。
func Run(ctx context.Context, command string, userEnvs map[string]string) (string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	if userEnvs != nil {
		cmd.Env = append(cmd.Environ(), envSlice(userEnvs)...)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("exec %q: %w\n%s", command, err, out)
	}
	return string(out), nil
}

func envSlice(m map[string]string) []string {
	s := make([]string, 0, len(m))
	for k, v := range m {
		s = append(s, k+"="+v)
	}
	return s
}
