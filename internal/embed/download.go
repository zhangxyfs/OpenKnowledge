package embed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// Download 把模型下载到 modelsDir/<id>.gguf：
// .part 断点续传（Range）→ 写完后整文件 sha256 校验 → 原子改名。
// ctx 取消保留 .part 供下次续传；sha256 不符删 .part 报错（防循环续传坏文件）。
func Download(ctx context.Context, hc *http.Client, m BuiltinModel, mirror, modelsDir string, progress func(done, total int64)) error {
	if hc == nil {
		hc = http.DefaultClient
	}
	if err := os.MkdirAll(modelsDir, 0o755); err != nil {
		return err
	}
	dest := m.InstalledPath(modelsDir)
	part := dest + ".part"
	var offset int64
	if st, err := os.Stat(part); err == nil {
		offset = st.Size()
		if offset > m.Size { // 异常残留，重下
			_ = os.Remove(part)
			offset = 0
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.URL(mirror), nil)
	if err != nil {
		return err
	}
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch {
	case offset > 0 && resp.StatusCode == http.StatusRequestedRangeNotSatisfiable:
		// .part 比服务端还长（源变了）：删掉重头来
		_ = os.Remove(part)
		return fmt.Errorf("续传偏移越界（416），已清除 %s，请重试", filepath.Base(part))
	case offset > 0 && resp.StatusCode == http.StatusOK:
		// 服务端不认 Range：截断重下
		offset = 0
	case resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent:
		return fmt.Errorf("下载失败 HTTP %d", resp.StatusCode)
	}
	flag := os.O_CREATE | os.O_WRONLY
	if offset > 0 {
		flag |= os.O_APPEND
	} else {
		flag |= os.O_TRUNC
	}
	f, err := os.OpenFile(part, flag, 0o644)
	if err != nil {
		return err
	}
	written := offset
	buf := make([]byte, 256*1024)
	copyErr := func() error {
		for {
			n, rerr := resp.Body.Read(buf)
			if n > 0 {
				if _, werr := f.Write(buf[:n]); werr != nil {
					return werr
				}
				written += int64(n)
				if progress != nil {
					progress(written, m.Size)
				}
			}
			if rerr != nil {
				if rerr == io.EOF {
					return nil
				}
				return rerr
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
		}
	}()
	_ = f.Close()
	if copyErr != nil {
		return copyErr // .part 保留
	}
	if written != m.Size {
		return fmt.Errorf("下载大小不符：%d，期望 %d（.part 已保留可续传）", written, m.Size)
	}
	sum, err := fileSHA256(part)
	if err != nil {
		return err
	}
	if sum != m.SHA256 {
		_ = os.Remove(part)
		return fmt.Errorf("sha256 校验不符（已删除 %s）", filepath.Base(part))
	}
	return os.Rename(part, dest)
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
