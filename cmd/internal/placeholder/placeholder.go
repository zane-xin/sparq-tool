package placeholder

import (
	"fmt"
	"io"
)

// Print 输出预留入口提示，具体业务后续在对应模块补齐。
func Print(writer io.Writer, name string) error {
	_, err := fmt.Fprintf(writer, "%s 命令入口已保留，具体业务待实现\n", name)
	return err
}
