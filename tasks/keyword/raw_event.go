// Copyright 2024 monitorbeat contributors
// Licensed under the MIT License.

package keyword

import (
	"strconv"

	"github.com/abrance/monitorbeat/define"
	"github.com/abrance/monitorbeat/tasks"
)

// RawLogEventType 是 raw_log 事件在 define.Event.GetType() 上的固定字符串。
//
// 对外（output / 下游消费方）按此常量识别，不要在其它位置硬编码字符串。
const RawLogEventType = "raw_log"

// BuildRawLogEvent 构造一个 raw_log 事件。
//
// 负载 schema：
//   - dimensions.file:        源文件绝对路径
//   - dimensions.regex:       命中用的正则原文
//   - dimensions.hostname:    上报主机名
//   - metrics.matches_count:  始终 1
//   - fields:                 capture map + line_number（命名 group 用名字，匿名 group 用 "1"/"2"/…）
//   - raw:                    原始行（不含行尾 \n）
func BuildRawLogEvent(file, pattern string, lineNo int, captures map[string]string, raw string) define.Event {
	fields := make(map[string]string, len(captures)+1)
	for k, v := range captures {
		fields[k] = v
	}
	fields["line_number"] = strconv.Itoa(lineNo)

	return define.NewEvent(RawLogEventType, map[string]any{
		"dimensions": map[string]string{
			"file":     file,
			"regex":    pattern,
			"hostname": tasks.Hostname(),
		},
		"metrics": map[string]float64{
			"matches_count": 1,
		},
		"fields": fields,
		"raw":    raw,
	})
}
