export type FaultType = 
  | 'PORT_BLOCKED' 
  | 'port_blocked'
  | 'SERVER_BLOCKED' 
  | 'server_blocked'
  | 'ip_blocked'
  | 'MACHINE_DOWN' 
  | 'machine_offline'
  | 'HEALTHY' 
  | 'none'
  | 'PROTOCOL_MISMATCH' 
  | 'protocol_mismatch'
  | 'RESIDENTIAL_TUNNEL_OFFLINE'
  | 'residential_tunnel_offline'
  | 'TRANSIT_RELAY_DOWN'
  | 'transit_relay_down'
  | 'RESIDENTIAL_DIRECT_BLOCKED'
  | 'residential_direct_blocked'
  | 'NETWORK_CONGESTION'
  | 'network_congestion'
  | 'UNKNOWN';

export type NodeType = 'transit' | 'residential' | 'cf_ws' | 'chain';

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
  exit_ip?: string;
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

export interface SpeedTestHistoryItem {
  id: string;
  tested_at: string;
  type: 'peak' | 'idle' | 'manual'; // 高峰期 | 闲时 | 手工单测
  speed_mb_s: number;
  speed_mbps: number;
  ttfb_ms: number;
  latency_ms: number;
  relay_name?: string;
  file_size_mb: number; // 10MB
}

export interface SpeedResult {
  bandwidth_mbps: number;
  bandwidth_mb_s: number;
  ttfb_ms: number;
  duration_sec: number;
  bytes_transferred: number;
  speed_rank?: number;
  tested_at?: string;
  method?: string; // 'vps_10m' | 'vps_30m'
}

export interface ChainInfo {
  relay_node_name: string;
  relay_protocol: string;
  relay_region: string;
  target_resi_name: string;
  target_resi_ip: string;
  target_region: string;
  is_failover_relay: boolean;
  failover_reason?: string;
  direct_latency_ms: number;
  optimized_latency_ms: number;
  latency_gain_pct: number;
  direct_speed_mb_s?: number;
  optimized_speed_mb_s?: number;
}

export interface NodeProbeResult {
  id: string;
  name: string;
  protocol: string;
  online: boolean;
  latency_ms: number;
  china_exit: boolean;
  exit_ip?: string;
  exit_country: string;
  category: string;
  original_url: string;
  is_self?: boolean;
  node_type?: NodeType;
  host_id?: string; // e.g. "US-LAX-ACCK-4837", "JP-Tokyo-IIJ", "JP-Tokyo-Softbank"
  is_rescue?: boolean;
  rescue_status?: 'standby' | 'active'; // CF救活待命热备 vs 阻断激活接管
  chain_info?: ChainInfo;
  split?: SplitResult;
  diagnostic?: NodeDiagnostic;
  speed_result?: SpeedResult;
  tested_at?: string;
  sandbox_logs?: string[];
  speed_history?: SpeedTestHistoryItem[];
}

export interface MonitoredNodeState {
  config: {
    name: string;
    url: string;
    is_self: boolean;
    node_type?: NodeType;
  };
  last_result: NodeProbeResult | null;
  last_checked: string;
  last_error?: string;
}

export type MonitorStatusMap = Record<string, MonitoredNodeState>;

// 线路机主机聚合组模型 (美西4837 / 东京IIJ / 东京软银)
export interface TransitHostGroup {
  host_id: string;
  host_name: string;
  region: string;
  region_flag: string;
  ip: string;
  status: 'healthy' | 'china' | 'port_blocked' | 'all_blocked' | 'congested';
  status_label: string;
  primary_reality: NodeProbeResult; // 仅采用 VLESS Reality (淘汰 SS2022)
  rescue_cf_ws?: NodeProbeResult;   // CF-WS 443 应急救活节点
  is_rescue_active: boolean;        // 直连阻断时自动点亮为 true
}

// 家宽机直连与链式优化对比模型
export interface ResidentialPairItem {
  id: string;
  name: string;
  region: string;
  region_flag: string;
  target_ip: string;
  exit_ip?: string;
  direct_node: NodeProbeResult;
  chain_node?: NodeProbeResult;
  assigned_relay_name: string;      // 挂载的线路机 (美拉美, 日拉日, 富余匀配)
  is_failover_relay: boolean;       // 是否因线路机故障触发容灾接力
  failover_notice?: string;
  speed_history?: SpeedTestHistoryItem[];
}

export interface DailyScheduleQuota {
  speedtest_count: number;
  speedtest_max: number; // 2 次
  speedtest_last: string | null;
  speedtest_next_slot?: 'peak' | 'idle'; // 当前处于高峰期还是闲时
  split_count: number;
  split_max: number;     // 1 次
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
  backups?: BackupSnapshot[];
  last_smoke_test?: SmokeTestResult;
}

export interface BackupSnapshot {
  id: string;
  file_name: string;
  file_path: string;
  created_at: string;
  size_bytes: number;
}

export interface SmokeTestResult {
  success: boolean;
  latency_ms: number;
  tested_at: string;
  exit_ip?: string;
  message?: string;
  error?: string;
}

export interface SafeUpgradeResponse {
  success: boolean;
  vps_id: string;
  ip: string;
  software_type: string;
  previous_version: string;
  current_version: string;
  rolled_back: boolean;
  rollback_reason?: string;
  backup_created?: string;
  smoke_test?: SmokeTestResult;
  message: string;
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

// 外部机场订阅管理类型 (PR #20 / Issue #19)
export interface ExternalSubDetail {
  name: string;
  display_name: string;
  enabled: boolean;
  url?: string;
  auto_mount?: boolean;
}

export interface ExternalSubsResponse {
  status: string;
  count: number;
  sources: string[];
  details: ExternalSubDetail[];
}

// 预烘焙快照极速直出大盘类型 (PR #22 / Issue #21)
export interface SnapshotMetaInfo {
  file_name: string;
  size: number;
  size_human: string;
  updated_at_str: string;
  duration_ms: number;
  proxy_count: number;
  sha256: string;
  status: string;
}

export interface SnapshotStatusResponse {
  status: string;
  count: number;
  storage_dir: string;
  snapshots: Record<string, SnapshotMetaInfo>;
}

// 自定义分流规则类型
export interface CustomRulesData {
  direct_domains?: string[];
  proxy_domains?: string[];
  reject_domains?: string[];
  version?: string;
}

export interface AtomNode {
  id: string;
  name: string;
  url: string;
  protocol: string;
  country: string;
  kind: 'transit' | 'residential' | 'chain';
  infra_type?: InfraType;
  tags?: string[];
  china_exit: boolean;
  latency_ms: number;
  online: boolean;
  tested_at: string;
}

export type InfraType = 'nat_resi' | 'vps_resi' | 'nat_line' | 'vps_line' | 'commercial';
export interface GroupFilter {
  mode?: 'filter' | 'manual';
  node_ids?: string[];
  countries?: string[];
  kinds?: string[];
  infra_types?: string[];
  tags_any?: string[];
  tags_all?: string[];
  exclude_tags?: string[];
  online_only?: boolean;
  clean_only?: boolean;
  keywords?: string[];
  exclude?: string[];
  // 兼容辅助字段
  include_transit?: boolean;
  include_residential?: boolean;
  include_chain?: boolean;
  exclude_china_leak?: boolean;
  custom_match_regex?: string;
  protocols?: string[];
}

export interface CustomGroupDef {
  name: string;
  display_name: string;
  enabled: boolean;
  type?: 'select' | 'url-test' | 'fallback' | 'load-balance' | string;
  order?: number;
  add_to_entry?: boolean;
  filter: GroupFilter;
  created_at?: string;
  updated_at?: string;
}

export interface PublishJob {
  id: string;
  revision: number;
  status: 'queued' | 'running' | 'verifying' | 'succeeded' | 'failed' | 'superseded' | string;
  attempts?: number;
  error?: string;
  created_at?: string;
  updated_at?: string;
}

export interface CustomGroupSummaryItem {
  name: string;
  display_name: string;
  enabled: boolean;
  filter: GroupFilter;
  count: number;
  created_at?: string;
  updated_at?: string;
}

export interface CategoryStat {
  country?: string;
  display_name: string;
  transit_count: number;
  residential_count: number;
  chain_count: number;
  total_clean_count: number;
  nodes: AtomNode[];
}

export interface ExternalCategoryStat {
  display_name: string;
  sources: string[];
  source_count: number;
  description: string;
}

export interface CategoriesSummary {
  status: string;
  total_atoms: number;
  categories: {
    external?: ExternalCategoryStat;
    residential?: CategoryStat;
    us_resi?: CategoryStat;
    jp_resi?: CategoryStat;
    other_resi?: CategoryStat;
    transit?: CategoryStat;
    china_leaks: AtomNode[];
    // 兼容字段
    us?: CategoryStat;
    jp?: CategoryStat;
    other?: CategoryStat;
  };
  custom_groups?: CustomGroupSummaryItem[];
}

export interface CustomChainBind {
  id: string;
  transit_name: string;
  residential_name: string;
  chain_name: string;
  chain_url: string;
  created_at: string;
}

export interface ProxyInspectSummary {
  name: string;
  type: string;
  country?: string;
  kind?: 'transit' | 'residential' | 'chain' | 'airport' | string;
  is_self_hosted: boolean;
  latency_ms: number;
  online: boolean;
  tags?: string[];
}

export interface GroupInspectItem {
  name: string;
  display_name: string;
  type: string;
  is_external: boolean;
  category: string;
  tags: string[];
  proxies: ProxyInspectSummary[];
  proxy_count: number;
  rule_count: number;
  custom_def?: CustomGroupDef;
}

export interface RuleInspectItem {
  type: string;
  payload: string;
  target: string;
  provider?: string;
  raw: string;
}

export interface SubscriptionInspectResponse {
  status: string;
  subscription: string;
  total_proxies: number;
  total_groups: number;
  self_groups: number;
  ext_groups: number;
  groups: GroupInspectItem[];
  rules: RuleInspectItem[];
  rule_providers: string[];
  routing_script?: string;
  updated_at: string;
}
