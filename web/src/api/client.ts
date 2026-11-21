import {
  MonitorStatusMap,
  NodeProbeResult,
  SubStoreModeInfo,
  SubStoreMode,
  SubStoreManifest,
  SubStoreConfig,
  SoftwareInfo,
  BlockedAnalysisResult,
  PortHealResponse,
  ExternalSubsResponse,
  ExternalSubDetail,
  SnapshotStatusResponse,
  CustomRulesData,
  AtomNode,
  GroupFilter,
  CustomGroupDef,
  CategoriesSummary,
} from '../types';

const TOKEN_KEY = 'vpn_probe_admin_token';

export function getStoredToken(): string {
  return localStorage.getItem(TOKEN_KEY) || '';
}

export function setStoredToken(token: string): void {
  localStorage.setItem(TOKEN_KEY, token);
}

export function removeStoredToken(): void {
  localStorage.removeItem(TOKEN_KEY);
}

async function request<T>(endpoint: string, options: RequestInit = {}): Promise<T> {
  const token = getStoredToken();
  const headers = new Headers(options.headers || {});
  
  headers.set('Content-Type', 'application/json');
  if (token) {
    headers.set('Authorization', `Bearer ${token}`);
    headers.set('X-API-Key', token);
  }

  // Ensure reveal=true is present to show raw full links without masking
  const separator = endpoint.includes('?') ? '&' : '?';
  const url = `${endpoint}${separator}reveal=true`;

  const resp = await fetch(url, {
    ...options,
    headers,
  });

  if (resp.status === 401 || resp.status === 403) {
    // If unauthorized, token might be invalid
    throw new Error('AUTH_FAILED: Token 无效或无管理员权限');
  }

  if (!resp.ok) {
    const errorBody = await resp.text().catch(() => '');
    throw new Error(`HTTP ${resp.status}: ${errorBody || resp.statusText}`);
  }

  const contentType = resp.headers.get('content-type') || '';
  if (contentType.includes('application/json')) {
    return (await resp.json()) as T;
  }
  return (await resp.text()) as unknown as T;
}

export const api = {
  // 1. 鉴权测试
  async verifyAuth(token: string): Promise<boolean> {
    const headers = new Headers({
      'Content-Type': 'application/json',
      'Authorization': `Bearer ${token}`,
      'X-API-Key': token,
    });
    const resp = await fetch(`/api/monitor/status?reveal=true`, { headers });
    return resp.ok;
  },

  // 2. 监控调度与状态
  async getMonitorStatus(): Promise<MonitorStatusMap> {
    return request<MonitorStatusMap>('/api/monitor/status');
  },

  async getBlockedPorts(): Promise<{ total: number; nodes: any[] }> {
    return request<{ total: number; nodes: any[] }>('/api/monitor/port-blocked');
  },

  async getOfflineNodes(): Promise<{ total: number; nodes: any[] }> {
    return request<{ total: number; nodes: any[] }>('/api/monitor/offline');
  },

  async triggerInspection(split: boolean = false): Promise<{ status: string; message: string }> {
    return request<{ status: string; message: string }>(`/api/monitor/trigger?split=${split}`, {
      method: 'POST',
    });
  },

  async refreshNodesFromSubStore(): Promise<{ status: string; message: string }> {
    return request<{ status: string; message: string }>('/api/monitor/refresh', {
      method: 'POST',
    });
  },

  // 3. 自研 30MB 吞吐量测速 (非开源测速库)
  async runSpeedTest(isStream: boolean = false, durationSec: number = 30): Promise<{ status: string; count: number; nodes: any[] }> {
    return request('/api/speedtest/run', {
      method: 'POST',
      body: JSON.stringify({ is_stream: isStream, duration_sec: durationSec }),
    });
  },

  async getSpeedTestRanking(): Promise<{ status: string; count: number; ranking: any[] }> {
    return request('/api/speedtest/ranking');
  },

  // 4. 历史记录查询
  async getHistory(limit: number = 50): Promise<NodeProbeResult[]> {
    return request<NodeProbeResult[]>(`/api/history?limit=${limit}`);
  },

  // 5. 批量沙箱检测
  async probeBatch(links: string[], isSelf: boolean = true, syncSubStore: boolean = false, checkSplit: boolean = true, timeoutSec: number = 8): Promise<any> {
    return request('/api/probe', {
      method: 'POST',
      body: JSON.stringify({
        links,
        is_self: isSelf,
        sync_substore: syncSubStore,
        check_detailed_split: checkSplit,
        timeout_seconds: timeoutSec,
      }),
    });
  },

  // 6. SubStore 拓扑中枢
  async getSubStoreMode(): Promise<SubStoreModeInfo> {
    return request<SubStoreModeInfo>('/api/substore/mode');
  },

  async setSubStoreMode(mode: SubStoreMode): Promise<any> {
    return request('/api/substore/mode', {
      method: 'POST',
      body: JSON.stringify({ mode }),
    });
  },

  async syncThreeTier(): Promise<{ status: string; message: string }> {
    return request<{ status: string; message: string }>('/api/substore/sync-threetier', {
      method: 'POST',
    });
  },

  async reconcileTopology(): Promise<{ status: string; message: string; audit: any }> {
    return request('/api/substore/reconcile', {
      method: 'POST',
    });
  },

  async cleanupDeadSocks(subscription: string = 'Q'): Promise<any> {
    return request('/api/substore/cleanup', {
      method: 'POST',
      body: JSON.stringify({ subscription }),
    });
  },

  async getSubStoreManifest(): Promise<SubStoreManifest> {
    return request<SubStoreManifest>('/api/substore/manifest');
  },

  async getSubStoreShares(): Promise<{ status: string; substore_url: string; total: number; shares: any[] }> {
    return request('/api/substore/shares');
  },

  async getSubStoreScripts(): Promise<{ status: string; total: number; scripts: any[] }> {
    return request('/api/substore/scripts');
  },

  async getScriptContent(name: string): Promise<string> {
    return request<string>(`/api/substore/scripts/${encodeURIComponent(name)}?raw=true`);
  },

  async updateScript(name: string, content: string): Promise<any> {
    return request(`/api/substore/scripts/${encodeURIComponent(name)}`, {
      method: 'PUT',
      body: JSON.stringify({ content }),
    });
  },

  async toggleOperator(shareName: string, operatorId: string, scriptFile: string, disabled: boolean): Promise<any> {
    return request(`/api/substore/shares/${encodeURIComponent(shareName)}/pipeline`, {
      method: 'PUT',
      body: JSON.stringify({ operator_id: operatorId, script_file: scriptFile, disabled }),
    });
  },

  async getSubStoreNodes(name: string = "ashan's vps"): Promise<{ status: string; subscription: string; count: number; nodes: string[]; raw_content?: string }> {
    return request(`/api/substore/nodes?name=${encodeURIComponent(name)}`);
  },

  async updateSubStoreNodes(name: string = "ashan's vps", links: string[]): Promise<any> {
    return request('/api/substore/nodes', {
      method: 'POST',
      body: JSON.stringify({ name, links }),
    });
  },

  async getSubStoreConfig(): Promise<SubStoreConfig> {
    return request<SubStoreConfig>('/api/substore/config');
  },

  async updateSubStoreConfig(cfg: Partial<SubStoreConfig>): Promise<any> {
    return request('/api/substore/config', {
      method: 'PUT',
      body: JSON.stringify(cfg),
    });
  },

  async verifySubscription(file: string = 'Q'): Promise<any> {
    return request(`/api/substore/verify?file=${encodeURIComponent(file)}`);
  },

  // 7. VPS 管理与自愈换端口
  async getVPSManagement(): Promise<{ count: number; instances: SoftwareInfo[] }> {
    return request<{ count: number; instances: SoftwareInfo[] }>('/api/vps/manage');
  },

  async getBlockedAnalysis(): Promise<{ count: number; results: BlockedAnalysisResult[] }> {
    return request<{ count: number; results: BlockedAnalysisResult[] }>('/api/vps/blocked-analysis');
  },

  async healPort(nodeName: string, targetPort?: number): Promise<PortHealResponse> {
    return request<PortHealResponse>('/api/vps/heal/port', {
      method: 'POST',
      body: JSON.stringify({ node_name: nodeName, target_port: targetPort || 0 }),
    });
  },

  async installVPSProxy(nodeName: string, softwareType: string = 'sing-box'): Promise<any> {
    return request('/api/vps/manage/install', {
      method: 'POST',
      body: JSON.stringify({ node_name: nodeName, software_type: softwareType }),
    });
  },

  async updateVPSProxy(nodeName: string): Promise<any> {
    return request('/api/vps/manage/update', {
      method: 'POST',
      body: JSON.stringify({ node_name: nodeName }),
    });
  },

  // 8. 运维与环境诊断
  async getSystemEnv(): Promise<any> {
    return request('/api/utils/env');
  },

  async reloadConfig(): Promise<any> {
    return request('/api/config/reload', {
      method: 'POST',
    });
  },

  // 9. 外部商业机场订阅动态纳管 API (PR #20 / Issue #19)
  async getExternalSubs(): Promise<ExternalSubsResponse> {
    return request<ExternalSubsResponse>('/api/external/subs');
  },

  async upsertExternalSub(sub: {
    name: string;
    url: string;
    display_name?: string;
    enabled?: boolean;
    auto_mount?: boolean;
  }): Promise<any> {
    return request('/api/external/subs', {
      method: 'POST',
      body: JSON.stringify(sub),
    });
  },

  async deleteExternalSub(name: string, purge: boolean = false): Promise<any> {
    return request(`/api/external/subs/${encodeURIComponent(name)}?purge=${purge}`, {
      method: 'DELETE',
    });
  },

  // 10. 预烘焙快照极速直出大盘 API (PR #22 / Issue #21)
  async getSnapshotStatus(): Promise<SnapshotStatusResponse> {
    return request<SnapshotStatusResponse>('/api/sub/snapshot/status');
  },

  async warmupSnapshot(fileNames: string[] = ['Q', 'ashan']): Promise<any> {
    return request('/api/sub/snapshot/warmup', {
      method: 'POST',
      body: JSON.stringify({ file_names: fileNames }),
    });
  },

  // 11. 自定义分流规则与直连域名 API
  async getCustomRules(): Promise<CustomRulesData> {
    return request<CustomRulesData>('/api/rules/custom');
  },

  async updateCustomRules(rules: CustomRulesData): Promise<any> {
    return request('/api/rules/custom', {
      method: 'POST',
      body: JSON.stringify(rules),
    });
  },

  async addDirectDomain(domain: string): Promise<any> {
    return request('/api/rules/direct-domain', {
      method: 'POST',
      body: JSON.stringify({ domain }),
    });
  },

  // 12. 编排与原子库 API (Issue #24: 方案B网状池化、五维策略、动态公式与自建全家桶)
  async getAtoms(params?: { country?: string; kind?: string; clean_only?: boolean; reveal?: boolean }): Promise<{ status: string; count: number; atoms: AtomNode[] }> {
    const q = new URLSearchParams();
    if (params?.country) q.set('country', params.country);
    if (params?.kind) q.set('kind', params.kind);
    if (params?.clean_only) q.set('clean_only', 'true');
    if (params?.reveal) q.set('reveal', 'true');
    const qs = q.toString();
    return request(`/api/orchestration/atoms${qs ? '?' + qs : ''}`);
  },

  async getCategoriesSummary(): Promise<CategoriesSummary> {
    return request('/api/orchestration/categories');
  },

  async evaluateGroup(filter: GroupFilter, reveal = false): Promise<{ status: string; count: number; nodes: string[] }> {
    return request(`/api/orchestration/evaluate${reveal ? '?reveal=true' : ''}`, {
      method: 'POST',
      body: JSON.stringify(filter),
    });
  },

  async getCustomGroups(): Promise<{ status: string; count: number; groups: CustomGroupDef[] }> {
    return request('/api/orchestration/groups');
  },

  async saveCustomGroup(def: CustomGroupDef, sync = true): Promise<{ status: string; message: string; group: CustomGroupDef }> {
    return request(`/api/orchestration/groups${sync ? '?sync=true' : '?sync=false'}`, {
      method: 'POST',
      body: JSON.stringify(def),
    });
  },

  async deleteCustomGroup(name: string, purge = false): Promise<{ status: string; deleted: string; purge: boolean }> {
    return request(`/api/orchestration/groups/${encodeURIComponent(name)}${purge ? '?purge=true' : ''}`, {
      method: 'DELETE',
    });
  },

  async syncAllCustomGroups(): Promise<{ status: string; message: string }> {
    return request('/api/orchestration/groups/sync', {
      method: 'POST',
    });
  },

  async rebuildFullMeshChains(): Promise<{ status: string; message: string }> {
    return request('/api/orchestration/chains/rebuild', {
      method: 'POST',
    });
  },

  async syncFiveGroups(): Promise<{ status: string; message: string }> {
    return request('/api/substore/sync-fivegroups', {
      method: 'POST',
    });
  },

  async getCustomChains(): Promise<{ status: string; count: number; chains: any[] }> {
    return request('/api/orchestration/chains');
  },

  async bindCustomChain(transit: string, residential: string): Promise<any> {
    return request('/api/orchestration/chains/bind', {
      method: 'POST',
      body: JSON.stringify({ transit, residential }),
    });
  },

  async unbindCustomChain(target: string): Promise<any> {
    return request('/api/orchestration/chains/unbind', {
      method: 'POST',
      body: JSON.stringify({ target }),
    });
  },
};
