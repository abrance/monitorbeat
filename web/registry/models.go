// Package registry 提供 agent 心跳注册与发现能力。
//
// 嵌入 monitorweb 单二进制，与 alert 共享同一 SQLite 文件。
// agent 通过 POST heartbeat 上报元数据，前端通过 GET agents 获取主机列表。
package registry

// AgentInfo 是 agent 上报的心跳元数据。
type AgentInfo struct {
	Hostname  string   `json:"hostname"`
	Version   string   `json:"version"`
	Tasks     []string `json:"tasks"`
	IP        string   `json:"ip"`
	K8sNode   string   `json:"k8s_node,omitempty"`
	StartTime int64    `json:"start_time"`
	LastSeen  int64    `json:"last_seen,omitempty"`
	Online    bool     `json:"online,omitempty"`
}
