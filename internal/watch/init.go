package watch

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const sampleConfig = `cwd: .

build:
  command: go build ./...

run:
  command: go run ./cmd/app
  env:
    APP_ENV: dev

watch:
  paths: [.]
  include: ["**/*.go", go.mod, go.sum]
  exclude: [.git, tmp, vendor, storage]
  exclude_globs: []
  mode: notify
  poll_interval: 500ms
  debounce: 300ms

stop:
  grace_timeout: 3s
  kill_timeout: 2s

restart:
  policy: never
  initial_delay: 500ms
  max_delay: 10s
  reset_after: 30s

# health:
#   url: http://127.0.0.1:8080
#   path: /healthz
#   timeout: 30s
`

// InitConfig 排他创建标准配置，避免覆盖用户已有的开发命令和路径规则。
func InitConfig(path string, writer io.Writer) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("watch 配置已存在，不会覆盖: %s", path)
		}
		return fmt.Errorf("创建 watch 配置 %s 失败: %w", path, err)
	}
	if _, err := io.WriteString(file, sampleConfig); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return fmt.Errorf("写入 watch 配置 %s 失败: %w", path, err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("关闭 watch 配置 %s 失败: %w", path, err)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		absolute = path
	}
	fmt.Fprintf(writer, "已生成 watch 配置: %s\n", absolute)
	return nil
}
