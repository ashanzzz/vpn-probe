export type FaultType = 'PORT_BLOCKED' | 'SERVER_BLOCKED' | 'MACHINE_DOWN' | 'HEALTHY' | 'PROTOCOL_MISMATCH' | 'UNKNOWN';

export interface SplitResult {
  chatgpt?: string;
  claude?: string;
  youtube?: string;
  baidu?: string;
}

export interface NodeDiagnostic {
  fault_type: FaultType;
  status_text: string;
  target_ip: string;
  target_port: number;
  abroad_source?: string;
  abroad_rtt_ms?: number;
  action_type?: string;
  troubleshoot_tip?: string;
  domestic_tcp?: boolean;
  abroad_tcp?: boolean;
  google_ncr_url?: string;
  google_ncr_redirect?: string;
  cf_loc?: string;
}

export interface SpeedResult {
  bandwidth_mbps: number;
  bandwidth_mb_s: number;
  ttfb_ms: number;
  duration_sec: number;
  bytes_transferred: number;
  speed_rank?: number;
  tested_at?: string;
}

export interface NodeProbeResult {
  id: string;
  name: string;
  protocol: string;
  online: boolean;
  latency_ms: number;
  china_exit: boolean;
  exit_country: string;
  category: string;
  original_url: string;
  is_self?: boolean;
  split?: SplitResult;
  diagnostic?: NodeDiagnostic;
  speed_result?: SpeedResult;
  tested_at?: string;
  sandbox_logs?: string[];
}

export interface MonitoredNodeState {
  config: {
    name: string;
    url: string;
    is_self: boolean;
  };
  last_result: NodeProbeResult | null;
  last_checked: string;
  last_error?: string;
}

export type MonitorStatusMap = Record<string, MonitoredNodeState>;

export interface DailyScheduleQuota {
  speedtest_count: number;
  speedtest_max: number;
  speedtest_last: string | null;
  split_count: number;
  split_max: number;
  split_last: string | null;
}

export type SubStoreMode = 'external+self' | 'self' | 'external';

export interface SubStoreConfig {
  enabled: boolean;
  url: string;
  username: string;
  password?: string;
  token?: string;
  group_self_online?: string;
  group_self_offline?: string;
  group_self_online_china?: string;
  group_self_online_clean?: string;
  group_ext_online_clean?: string;
  group_ext_online_china?: string;
}

export interface SubStoreModeInfo {
  current_mode: SubStoreMode;
  allowed_modes: SubStoreMode[];
  subscriptions: string[];
}

export interface SubStoreShareLink {
  name: string;
  path: string;
  url: string;
  full_url: string;
  type: string;
  substore_managed: boolean;
  status?: string;
}

export interface SubStoreScriptInfo {
  name: string;
  path: string;
  size: number;
  content?: string;
}

export interface SubStoreManifest {
  substore_url: string;
  share_links: SubStoreShareLink[];
  scripts: SubStoreScriptInfo[];
}

export interface VPSInstance {
  id: string;
  name: string;
  ip: string;
  ssh_port: number;
  ssh_user: string;
  status: string;
  region: string;
}

export interface SoftwareInfo {
  instance_id: string;
  instance_name: string;
  ip: string;
  software_type: string;
  deploy_mode: string;
  current_version: string;
  latest_version: string;
  has_update: boolean;
  active_ports: number[];
  status: string;
  error_message?: string;
  last_checked_at: string;
}

export interface BlockedAnalysisResult {
  node_name: string;
  server_ip: string;
  server_port: number;
  protocol: string;
  fault_type: FaultType;
  domestic_tcp: boolean;
  abroad_tcp: boolean;
  domestic_ping: boolean;
  abroad_rtt_ms: number;
  abroad_source: string;
  diagnostic_message: string;
  recommended_action: string;
  can_auto_heal_port: boolean;
  can_relay_rescue: boolean;
}

export interface PortHealResponse {
  success: boolean;
  node_name: string;
  old_port: number;
  new_port: number;
  new_url: string;
  substore_synced: boolean;
  config_synced: boolean;
  message: string;
}

export interface AuditLogItem {
  timestamp: string;
  level: string;
  action: string;
  ip: string;
  target: string;
  status: string;
  detail: string;
}
