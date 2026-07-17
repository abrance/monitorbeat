// Package registry 提供 agent 注册心跳上报能力。
//
// 与 web/registry 对应：本包负责发送心跳，web/registry 负责接收和存储。
// 两边通过 JSON 结构对齐，无直接 import 依赖。
package registry

// AgentInfo 是上报到 monitorweb registry 的心跳元数据。
// 字段名与 web/registry/models.go 的 AgentInfo JSON tag 对齐。
type AgentInfo struct {
	Hostname  string   `json:"hostname"`
	Version   string   `json:"version"`
	Tasks     []string `json:"tasks"`
	IP        string   `json:"ip"`
	K8sNode   string   `json:"k8s_node,omitempty"`
	StartTime int64    `json:"start_time"`
}
