export interface Host {
  hostname: string
  last_seen: number
}

export interface Agent {
  hostname: string
  version: string
  tasks: string[]
  ip: string
  k8s_node: string
  start_time: number
  last_seen: number
  online: boolean
}

export interface Summary {
  hostname: string
  ts: number
  cpu_usage?: number
  mem_used_percent?: number
  disk_root_used_percent?: number
  load1?: number
  net_bytes_recv?: number
  net_bytes_sent?: number
}

export type Point = [number, number]

export interface MetricSeries {
  metric: string
  unit: string
  points: Point[]
}

export interface EventsResult {
  type: string
  points: Point[]
}

export interface ProbeSeries {
  up: Point[]
  duration: Point[]
}

export interface ProbeResult {
  ping: ProbeSeries
  tcp: ProbeSeries
  http: ProbeSeries
}

export interface Healthz {
  status: string
  victoriametrics: string
}

export interface AlertRule {
  id: number
  name: string
  enabled: boolean
  expr: string
  duration: number
  description: string
  created_at: string
  updated_at: string
  states: AlertState[]
}

export interface AlertState {
  fingerprint: string
  labels: Record<string, string>
  hostname: string
  status: 'ok' | 'pending' | 'firing'
  acknowledged: boolean
  silence_until: string | null
  last_value: number
}

export interface AlertHistoryItem {
  id: number
  rule_id: number
  rule_name: string
  fingerprint: string
  labels: Record<string, string>
  hostname: string
  expr: string
  metric_value: number
  state: 'firing' | 'recovered'
  acknowledged: boolean
  triggered_at: string
}

export interface AlertStatus {
  firing_count: number
  acknowledged_count: number
  items: AlertStatusItem[]
}

export interface AlertStatusItem {
  rule_id: number
  rule_name: string
  fingerprint: string
  hostname: string
  labels: Record<string, string>
  status: string
  acknowledged: boolean
  since: string
}

export interface AlertTestInstance {
  fingerprint: string
  labels: Record<string, string>
  value: number
}

export interface AlertTestResponse {
  result: AlertTestInstance[]
}
