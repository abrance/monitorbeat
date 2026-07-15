export interface Host {
  hostname: string
  os: string
  platform: string
  arch: string
  kernel_version: string
  last_seen: number
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
