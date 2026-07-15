// Copyright 2024 monitorbeat contributors
// Licensed under the MIT License.

// Package extract 把 regexp 命中后的 named / unnamed capture 组展开为 map[string]string。
//
// 命名 group：key = group 名。
// 匿名 group：key = "1" / "2" / … （与 Go regexp 标准 SubexpIndex 一致）。
//
// 全为匿名 group 时，map 用字符串数字 key；零 capture 命中时返回空 map（长度 0）。
package extract

import (
	"regexp"
	"strconv"
)

// Extract 在 line 上跑 re；命中返回 (captures, true)，未命中返回 (nil, false)。
func Extract(line string, re *regexp.Regexp) (map[string]string, bool) {
	m := re.FindStringSubmatch(line)
	if m == nil {
		return nil, false
	}
	names := re.SubexpNames()
	allUnnamed := true
	for _, name := range names {
		if name != "" {
			allUnnamed = false
			break
		}
	}
	out := make(map[string]string, len(m))
	for i, name := range names {
		if i == 0 {
			continue
		}
		if !allUnnamed && name != "" {
			out[name] = m[i]
			continue
		}
		// 全部匿名 或 混合模式下的匿名组：用 "1"/"2"/… 作 key。
		if allUnnamed || name == "" {
			out[strconv.Itoa(i)] = m[i]
		}
	}
	return out, true
}
