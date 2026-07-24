package server

import (
	"fmt"
	"os"
	"path/filepath"
)

func ensureWritableDirectory(path string) error {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return fmt.Errorf("创建下载目录失败: %w", err)
	}
	probe, err := os.CreateTemp(path, ".localbilibili-write-test-*")
	if err != nil {
		return fmt.Errorf("下载目录不可写: %s。请在 fnOS 应用设置中将该保存目录授权为读写；如果这是已有UP主目录，请修复该UP主目录权限或重新选择可写目录。原始错误: %w", path, err)
	}
	name := probe.Name()
	if err := probe.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("下载目录写入检测失败: %s: %w", path, err)
	}
	if err := os.Remove(name); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("下载目录临时文件清理失败: %s: %w", filepath.Base(name), err)
	}
	return nil
}
