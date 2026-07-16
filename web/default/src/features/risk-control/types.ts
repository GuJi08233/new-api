// Risk control types

export interface IpRankItem {
  ip: string
  user_count: number
  request_count: number
}

export interface UserRankItem {
  user_id: number
  username: string
  ip_count: number
  request_count: number
}

export interface UaRankItem {
  ua: string
  user_count: number
  request_count: number
}

export interface RiskUserDetailItem {
  user_id: number
  username: string
  request_count: number
  first_seen: number
  last_seen: number
}

export interface RiskIpDetailItem {
  ip: string
  request_count: number
  first_seen: number
  last_seen: number
}

export interface RiskRankingsMeta {
  ip_log_enabled: boolean
  ua_log_enabled: boolean
  setting_enabled: boolean
}

export type RiskMetric = 'ip_multi_user' | 'user_multi_ip' | 'ua'

export interface RiskAutoBanRule {
  enabled: boolean
  metric: 'ip_multi_user' | 'user_multi_ip'
  window_hours: number
  threshold: number
  action: 'alert' | 'disable_user'
}

export interface ApiResponse<T = unknown> {
  success: boolean
  message: string
  data?: T
}
