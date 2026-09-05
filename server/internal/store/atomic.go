package store

import (
	"io"
	"os"
)

// 原子写辅助：临时文件 + 写后 rename，避免崩溃导致文件损坏。
// 三个 OS 操作通过包级变量暴露，供测试注入故障以覆盖错误分支（生产代码保持纯 stdlib）。
//
// 为什么用注入而不是 mount/tmpfs：故障注入是单测验证错误路径的事实标准做法，
// 路径固定由本进程命名，无并发抢占风险。
var (
	atomicOpenTmp = func(path string) (*os.File, error) {
		// 清理崩溃残骸；O_EXCL 防抢占与符号链接重定向。
		_ = os.Remove(path)
		return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	}
	atomicWrite  = func(f *os.File, data []byte) (int, error) { return f.Write(data) }
	atomicClose  = func(f *os.File) error { return f.Close() }
	atomicRename = func(tmp, dst string) error { return os.Rename(tmp, dst) }
	atomicRemove = os.Remove
)

// atomicWriteFile 原子写 path：先写 .tmp，成功后 rename 覆盖原文件；任何错误清理 .tmp。
// 调用方语义：路径 tmp = path + ".tmp" 必须可用（即 path 父目录存在且可写）。
func atomicWriteFile(path string, data []byte) error {
	tmp := path + ".tmp"
	f, err := atomicOpenTmp(tmp)
	if err != nil {
		return err
	}
	if n, werr := atomicWrite(f, data); werr != nil {
		_ = atomicClose(f)
		_ = atomicRemove(tmp)
		return werr
	} else if n != len(data) {
		_ = atomicClose(f)
		_ = atomicRemove(tmp)
		return io.ErrShortWrite
	}
	if cerr := atomicClose(f); cerr != nil {
		_ = atomicRemove(tmp)
		return cerr
	}
	if err := atomicRename(tmp, path); err != nil {
		_ = atomicRemove(tmp)
		return err
	}
	// 收紧权限：账号文件含 SecretKey/加密盐，必须仅属主可读写。
	// 失败不回滚：磁盘权限错误属罕见运维事件，文件已成功原子替换；后续 umask 或 OS 默认可能仍非 0600。
	_ = os.Chmod(path, 0o600)
	return nil
}
