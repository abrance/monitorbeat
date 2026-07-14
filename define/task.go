// Copyright 2024 monitorbeat contributors
//
// Licensed under the MIT License.

package define

import "context"

// Task 模块名常量，枚举所有内置采集任务类型。
//
// 与 bkmonitorbeat 保持命名兼容，方便后续从原仓平滑搬运 collector 实现。
const (
	ModuleGlobalHeartBeat = "global_heartbeat"
	ModuleChildHeartBeat  = "child_heartbeat"
	ModuleStatus           = "status"
	ModuleStatic           = "static"
	ModuleHTTP             = "http"
	ModuleMetricbeat       = "metricbeat"
	ModulePing             = "ping"
	ModuleScript           = "script"
	ModuleTCP              = "tcp"
	ModuleUDP              = "udp"
	ModuleKeyword          = "keyword"
	ModuleTrap             = "snmptrap"
	ModuleBasereport       = "basereport"
	ModuleExceptionbeat    = "exceptionbeat"
	ModuleKubeevent        = "kubeevent"
	ModuleProcessbeat      = "processbeat"
	ModuleProcConf         = "procconf"
	ModuleProcCustom       = "proccustom"
	ModuleProcSync         = "procsync"
	ModuleProcStatus       = "procstatus"
	ModuleProcBin          = "procbin"
	ModuleLoginLog         = "loginlog"
	ModuleProcSnapshot     = "procsnapshot"
	ModuleSocketSnapshot   = "socketsnapshot"
	ModuleShellHistory     = "shellhistory"
	ModuleRpmPackage       = "rpmpackage"
	ModuleTimeSync         = "timesync"
	ModuleDmesg            = "dmesg"
	ModuleGatherUpBeat     = "gather_up_beat"
	ModuleSelfStats        = "selfstats"
)

// Status 表示 task 或 scheduler 生命周期状态。
type Status int

const (
	StatusReady    Status = iota // 待执行
	StatusRunning                // 运行中
	StatusError                  // 出错退出
	StatusFinished               // 正常结束
)

// Task 是调度器调度的最小单元接口契约。
//
// 对照 bkmonitorbeat/define/task.go，去掉 GetBizID/GetDataID 等
// 蓝鲸多租户/数据账号概念；Ident 保留为调度器按任务指纹 dedup 与 reload 使用。
type Task interface {
	GetTaskID() int32
	GetStatus() Status
	SetConfig(TaskConfig)
	GetConfig() TaskConfig
	SetGlobalConfig(Config)
	GetGlobalConfig() Config
	Reload()
	Wait()
	Stop()
	Run(ctx context.Context, e chan<- Event)
}