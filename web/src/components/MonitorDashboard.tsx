import React, { useState, useEffect, useMemo, useCallback } from 'react';
import { 
  CheckCircle2, 
  AlertTriangle, 
  Globe, 
  Layers, 
  Home, 
  ShieldCheck, 
  ShieldAlert, 
  Plus, 
  Trash2, 
  ArrowRight, 
  TrendingUp, 
  Radio, 
  ExternalLink, 
  Wrench,
  RefreshCw,
  FileText
} from 'lucide-react';
import { api } from '../api/client';
import { useFeedback } from '../context/FeedbackContext';
import type { 
  MonitorStatusMap, 
  NodeProbeResult, 
  TransitHostGroup, 
  AtomNode,
  CustomChainBind 
} from '../types';

interface MonitorDashboardProps {
  statusMap: MonitorStatusMap;
  onOpenReport: (node: NodeProbeResult, initialTab?: 'overview' | 'ip_intelligence' | 'history' | 'logs') => void;
  onRetestNode: (node: NodeProbeResult) => Promise<void>;
  onSpeedTestNode: (node: NodeProbeResult) => Promise<void>;
  onOpenHealingWizard: (nodeName: string) => void;
  isInspecting: boolean;
}

export const MonitorDashboard: React.FC<MonitorDashboardProps> = ({
  statusMap,
  onOpenReport,
  onOpenHealingWizard,
  isInspecting,
}) => {
  const { pushActivity } = useFeedback();
  // Real residential atoms & chain bindings from backend
  const [residentialAtoms, setResidentialAtoms] = useState<AtomNode[]>([]);
  const [customChains, setCustomChains] = useState<CustomChainBind[]>([]);
  const [loadingAtoms, setLoadingAtoms] = useState(false);

  // Add residential node modal state
  const [showAddResiModal, setShowAddResiModal] = useState(false);
  const [newResiName, setNewResiName] = useState('');
  const [newResiURL, setNewResiURL] = useState('');
  const [targetSubGroup, setTargetSubGroup] = useState<'ashan-us-resi' | 'ashan-jp-resi' | 'ashan-other'>('ashan-us-resi');
  const [isSubmittingResi, setIsSubmittingResi] = useState(false);

  // Fetch real atoms and custom chains
  const fetchResiData = useCallback(async () => {
    setLoadingAtoms(true);
    try {
      const [atomsRes, chainsRes] = await Promise.all([
        api.getAtoms({ kind: 'residential' }).catch(() => ({ status: '', count: 0, atoms: [] })),
        api.getCustomChains().catch(() => ({ status: '', count: 0, chains: [] })),
      ]);
      setResidentialAtoms(atomsRes.atoms || []);
      setCustomChains(chainsRes.chains || []);
    } catch {
      // Graceful fallback
    } finally {
      setLoadingAtoms(false);
    }
  }, []);

  useEffect(() => {
    fetchResiData();
  }, [fetchResiData]);

  // Extract all tested probe results from statusMap
  const rawNodes = useMemo(() => {
    return Object.values(statusMap)
      .map((s) => s.last_result)
      .filter((n): n is NodeProbeResult => Boolean(n));
  }, [statusMap]);

  // Dynamically aggregate Transit Hosts from actual statusMap nodes
  const transitHostGroups = useMemo<TransitHostGroup[]>(() => {
    const hostMap = new Map<string, {
      host_name: string;
      region: string;
      region_flag: string;
      ip: string;
      realityNode?: NodeProbeResult;
      rescueNode?: NodeProbeResult;
      otherNodes: NodeProbeResult[];
    }>();

    // Helper: determine country & flag from name or country code
    const getRegionInfo = (name: string, country?: string) => {
      const n = name.toLowerCase();
      const c = (country || '').toUpperCase();
      if (c === 'US' || n.includes('美') || n.includes('us') || n.includes('lax')) {
        return { region: '美国', flag: '🇺🇸' };
      }
      if (c === 'JP' || n.includes('日') || n.includes('jp') || n.includes('tokyo')) {
        return { region: '日本', flag: '🇯🇵' };
      }
      if (c === 'HK' || n.includes('港') || n.includes('hk')) {
        return { region: '中国香港', flag: '🇭🇰' };
      }
      if (c === 'SG' || n.includes('新') || n.includes('sg')) {
        return { region: '新加坡', flag: '🇸🇬' };
      }
      if (c === 'TW' || n.includes('台') || n.includes('tw')) {
        return { region: '中国台湾', flag: '🇹🇼' };
      }
      return { region: country || '全球', flag: '🌐' };
    };

    // Helper: extract Host IP or domain key
    const extractHostKey = (n: NodeProbeResult): string => {
      if (n.diagnostic?.target_ip && n.diagnostic.target_ip !== '127.0.0.1') return n.diagnostic.target_ip;
      if (n.exit_ip && n.exit_ip !== '127.0.0.1') return n.exit_ip;
      // Match from URL
      const match = n.original_url.match(/@([^:]+):/);
      if (match && match[1]) return match[1];
      // Fallback to node prefix
      const parts = n.name.split(/[—\-\s]/);
      return parts[0] || n.name;
    };

    // Filter nodes that belong to pure transit / backbone infrastructure
    rawNodes.forEach((node) => {
      // 严格排除家宽节点：家宽节点专属于下方的「原生家宽与链式网络」大盘，严禁混入自建骨干线路机大盘
      const isResi = node.node_type === 'residential' || node.name.includes('家宽') || node.name.toLowerCase().includes('resi') || node.category?.includes('家宽');
      if (isResi) return;

      // 仅纳入纯骨干/线路机 (transit)
      const isTransit = node.node_type === 'transit' || node.category?.includes('线路') || /4837|IIJ|Softbank|软银|专线|骨干|Transit|BGP/i.test(node.name);
      if (!isTransit) return;

      const hostKey = extractHostKey(node);
      let entry = hostMap.get(hostKey);
      if (!entry) {
        const { region, flag } = getRegionInfo(node.name, node.exit_country);
        const ipVal = (node.diagnostic?.target_ip && node.diagnostic.target_ip !== '127.0.0.1') 
          ? node.diagnostic.target_ip 
          : (node.exit_ip || hostKey);
        entry = {
          host_name: node.name.replace(/—.*$/, '').trim() || `Host ${hostKey}`,
          region,
          region_flag: flag,
          ip: ipVal,
          otherNodes: [],
        };
        hostMap.set(hostKey, entry);
      }

      // Identify node purpose
      const isRescue = node.is_rescue || node.name.includes('CF') || node.name.includes('WS') || node.original_url.includes('443') || node.original_url.includes('ws');
      if (isRescue && !entry.rescueNode) {
        entry.rescueNode = node;
      } else if (!isRescue && !entry.realityNode) {
        entry.realityNode = node;
      } else {
        entry.otherNodes.push(node);
      }
    });

    const groups: TransitHostGroup[] = [];
    hostMap.forEach((entry, key) => {
      const primary = entry.realityNode || entry.otherNodes[0] || entry.rescueNode;
      if (!primary) return;

      const isRealityBlocked = primary.diagnostic?.fault_type === 'PORT_BLOCKED' || primary.diagnostic?.fault_type === 'port_blocked';
      const isRealityCongested = primary.diagnostic?.fault_type === 'NETWORK_CONGESTION' || primary.diagnostic?.fault_type === 'network_congestion';
      const isRealityDown = !primary.online && !isRealityBlocked && !isRealityCongested;
      const isAllBlocked = isRealityDown && Boolean(entry.rescueNode);

      let status: 'healthy' | 'china' | 'port_blocked' | 'all_blocked' | 'congested' = 'healthy';
      let status_label = '🟢 正常在线';

      if (isAllBlocked) {
        status = 'all_blocked';
        status_label = '🔴 全部阻断 (CF救活接管)';
      } else if (isRealityBlocked) {
        status = 'port_blocked';
        status_label = '🟠 端口阻断';
      } else if (isRealityCongested) {
        status = 'congested';
        status_label = '⚡ 跨境拥塞';
      } else if (primary.china_exit) {
        status = 'china';
        status_label = '🟡 送中';
      } else if (!primary.online) {
        status = 'all_blocked';
        status_label = '🔴 节点离线';
      }

      groups.push({
        host_id: key,
        host_name: entry.host_name,
        region: entry.region,
        region_flag: entry.region_flag,
        ip: entry.ip,
        status,
        status_label,
        primary_reality: primary,
        rescue_cf_ws: entry.rescueNode,
        is_rescue_active: isAllBlocked,
      });
    });

    return groups;
  }, [rawNodes]);

  // Real Macro Statistics
  const macroStats = useMemo(() => {
    let transitHealthy = 0;
    let transitChina = 0;
    let transitPortBlocked = 0;
    let transitAllBlocked = 0;

    transitHostGroups.forEach((h) => {
      if (h.status === 'healthy') transitHealthy++;
      else if (h.status === 'china') transitChina++;
      else if (h.status === 'port_blocked') transitPortBlocked++;
      else if (h.status === 'all_blocked' || h.status === 'congested') transitAllBlocked++;
    });

    const resiTotal = residentialAtoms.length;
    const resiClean = residentialAtoms.filter((r) => !r.china_exit).length;
    const resiChained = customChains.length;

    const rttList = residentialAtoms.map((r) => r.latency_ms).filter((r) => r > 0);
    const avgLatency = rttList.length > 0 
      ? Math.round(rttList.reduce((a, b) => a + b, 0) / rttList.length) 
      : 0;

    return {
      transit: {
        total: transitHostGroups.length,
        healthy: transitHealthy,
        china: transitChina,
        port_blocked: transitPortBlocked,
        all_blocked: transitAllBlocked,
      },
      resi: {
        total: resiTotal,
        clean: resiClean,
        chained: resiChained,
        avg_latency: avgLatency,
      },
    };
  }, [transitHostGroups, residentialAtoms, customChains]);

  // Handle Add Residential Node (Persists to SubStore)
  const handleAddResi = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!newResiName.trim() || !newResiURL.trim()) return;

    setIsSubmittingResi(true);
    try {
      // 1. Fetch current subscription nodes
      const existing = await api.getSubStoreNodes(targetSubGroup).catch(() => ({ nodes: [] }));
      const newLink = newResiURL.includes('#') ? newResiURL : `${newResiURL}#${encodeURIComponent(newResiName)}`;
      const updatedLinks = [...(existing.nodes || []), newLink];

      // 2. Persist update
      await api.updateSubStoreNodes(targetSubGroup, updatedLinks);

      // 3. Reset form and refresh
      setNewResiName('');
      setNewResiURL('');
      setShowAddResiModal(false);
      await fetchResiData();
      pushActivity({
        level: 'success',
        source: '家宽池',
        title: `已添加家宽节点至 ${targetSubGroup}`,
        detail: '已同步至 Sub-Store',
      });
    } catch (err: any) {
      pushActivity({
        level: 'error',
        source: '家宽池',
        title: '添加家宽节点失败',
        detail: err.message,
      });
    } finally {
      setIsSubmittingResi(false);
    }
  };

  return (
    <div className="space-y-6">
      {/* 1. TOP MACRO OVERVIEW BAR (TRANSIT VS RESIDENTIAL) */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        {/* Transit Overview Card */}
        <div className="bg-white border border-slate-200 rounded-xl p-4 shadow-2xs">
          <div className="flex items-center justify-between pb-2 border-b border-slate-100">
            <div className="flex items-center space-x-2">
              <Radio className="w-4 h-4 text-blue-600" />
              <h3 className="font-bold text-xs uppercase tracking-wider text-slate-800">
                骨干专线集群 (Transit VPS 矩阵)
              </h3>
            </div>
            <span className="text-2xs font-mono px-2 py-0.5 rounded bg-blue-50 text-blue-700 font-semibold border border-blue-200">
              纯净 VLESS Reality 架构
            </span>
          </div>

          <div className="grid grid-cols-4 gap-2 mt-3 text-center">
            <div className="p-2.5 bg-slate-50 border border-slate-200 rounded-lg">
              <div className="text-2xs text-emerald-600 font-semibold flex items-center justify-center">
                <CheckCircle2 className="w-3 h-3 mr-1" />
                正常在线
              </div>
              <div className="text-xl font-bold font-mono text-emerald-700 mt-1">
                {macroStats.transit.healthy}
              </div>
              <div className="text-2xs text-slate-400">直连优质</div>
            </div>

            <div className="p-2.5 bg-slate-50 border border-slate-200 rounded-lg">
              <div className="text-2xs text-amber-600 font-semibold flex items-center justify-center">
                <span>🇨🇳</span>
                <span className="ml-1">送中路由</span>
              </div>
              <div className="text-xl font-bold font-mono text-amber-700 mt-1">
                {macroStats.transit.china}
              </div>
              <div className="text-2xs text-slate-400">重定向 CN</div>
            </div>

            <div className="p-2.5 bg-slate-50 border border-slate-200 rounded-lg">
              <div className="text-2xs text-orange-600 font-semibold flex items-center justify-center">
                <AlertTriangle className="w-3 h-3 mr-1" />
                端口阻断
              </div>
              <div className="text-xl font-bold font-mono text-orange-700 mt-1">
                {macroStats.transit.port_blocked}
              </div>
              <div className="text-2xs text-slate-400">可一键自愈</div>
            </div>

            <div className="p-2.5 bg-slate-50 border border-slate-200 rounded-lg">
              <div className="text-2xs text-red-600 font-semibold flex items-center justify-center">
                <ShieldAlert className="w-3 h-3 mr-1" />
                离线/待测
              </div>
              <div className="text-xl font-bold font-mono text-red-700 mt-1">
                {macroStats.transit.all_blocked}
              </div>
              <div className="text-2xs text-slate-400">应急热备/排查</div>
            </div>
          </div>
        </div>

        {/* Residential Overview Card */}
        <div className="bg-white border border-slate-200 rounded-xl p-4 shadow-2xs">
          <div className="flex items-center justify-between pb-2 border-b border-slate-100">
            <div className="flex items-center space-x-2">
              <Home className="w-4 h-4 text-purple-600" />
              <h3 className="font-bold text-xs uppercase tracking-wider text-slate-800">
                原生家宽矩阵 (Residential Atoms)
              </h3>
            </div>
            <span className="text-2xs font-mono px-2 py-0.5 rounded bg-purple-50 text-purple-700 font-semibold border border-purple-200">
              真实后端原子库驱动
            </span>
          </div>

          <div className="grid grid-cols-4 gap-2 mt-3 text-center">
            <div className="p-2.5 bg-slate-50 border border-slate-200 rounded-lg">
              <div className="text-2xs text-slate-500 font-semibold">登记家宽</div>
              <div className="text-xl font-bold font-mono text-slate-900 mt-1">
                {macroStats.resi.total}
              </div>
              <div className="text-2xs text-slate-400">真实原子节点</div>
            </div>

            <div className="p-2.5 bg-slate-50 border border-slate-200 rounded-lg">
              <div className="text-2xs text-emerald-600 font-semibold flex items-center justify-center">
                <ShieldCheck className="w-3 h-3 mr-1" />
                NCR 纯净
              </div>
              <div className="text-xl font-bold font-mono text-emerald-700 mt-1">
                {macroStats.resi.clean}
              </div>
              <div className="text-2xs text-slate-400">不送中比例</div>
            </div>

            <div className="p-2.5 bg-slate-50 border border-slate-200 rounded-lg">
              <div className="text-2xs text-purple-600 font-semibold flex items-center justify-center">
                <Layers className="w-3 h-3 mr-1" />
                活跃链路
              </div>
              <div className="text-xl font-bold font-mono text-purple-700 mt-1">
                {macroStats.resi.chained}
              </div>
              <div className="text-2xs text-slate-400">网状中继绑定</div>
            </div>

            <div className="p-2.5 bg-slate-50 border border-slate-200 rounded-lg">
              <div className="text-2xs text-blue-600 font-semibold flex items-center justify-center">
                <TrendingUp className="w-3 h-3 mr-1" />
                平均 RTT
              </div>
              <div className="text-xl font-bold font-mono text-blue-700 mt-1">
                {macroStats.resi.avg_latency > 0 ? `${macroStats.resi.avg_latency}` : '—'}
                {macroStats.resi.avg_latency > 0 && <span className="text-xs font-normal ml-0.5">ms</span>}
              </div>
              <div className="text-2xs text-slate-400">端到端响应</div>
            </div>
          </div>
        </div>
      </div>

      {/* 2. SECTION 1: DYNAMIC TRANSIT HOST CARDS */}
      <div className="space-y-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center space-x-2">
            <Radio className="w-5 h-5 text-blue-600" />
            <h2 className="text-sm font-bold text-slate-900 tracking-tight">
              自建线路机大盘（按主机/IP动态聚合 · VLESS Reality 主力 + CF-WS 自动救援）
            </h2>
          </div>
          <span className="text-2xs text-slate-400">
            动态聚合成 {transitHostGroups.length} 台物理/云主机 · 零假数据
          </span>
        </div>

        {transitHostGroups.length === 0 ? (
          <div className="p-8 bg-white border border-slate-200 rounded-xl text-center space-y-2">
            <Radio className="w-8 h-8 text-slate-400 mx-auto animate-pulse" />
            <p className="text-xs font-semibold text-slate-700">暂无已探测的骨干专线节点</p>
            <p className="text-2xs text-slate-400">系统将在巡检完成后自动聚合呈现，或在顶栏点击「即刻巡检」触发。</p>
          </div>
        ) : (
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            {transitHostGroups.map((host) => {
              const reality = host.primary_reality;
              const cfWs = host.rescue_cf_ws;
              const isBlocked = reality?.diagnostic?.fault_type === 'PORT_BLOCKED' || reality?.diagnostic?.fault_type === 'port_blocked';

              return (
                <div
                  key={host.host_id}
                  className={`bg-white border rounded-xl shadow-2xs overflow-hidden flex flex-col transition ${
                    host.is_rescue_active
                      ? 'border-red-500 ring-2 ring-red-100'
                      : isBlocked
                      ? 'border-orange-400'
                      : 'border-slate-200 hover:border-slate-300'
                  }`}
                >
                  {/* Host Card Header */}
                  <div className="p-4 bg-slate-50 border-b border-slate-200 flex items-center justify-between">
                    <div className="flex items-center space-x-2.5">
                      <span className="text-xl">{host.region_flag}</span>
                      <div>
                        <h3 className="font-bold text-xs text-slate-900">{host.host_name}</h3>
                        <div className="text-2xs font-mono text-slate-500 mt-0.5">IP: {host.ip}</div>
                      </div>
                    </div>
                    <span
                      className={`px-2 py-0.5 rounded text-2xs font-semibold ${
                        host.status === 'healthy'
                          ? 'bg-emerald-100 text-emerald-800'
                          : host.status === 'china'
                          ? 'bg-amber-100 text-amber-800'
                          : host.status === 'port_blocked'
                          ? 'bg-orange-100 text-orange-800 animate-pulse'
                          : host.status === 'congested'
                          ? 'bg-sky-100 text-sky-800'
                          : 'bg-red-100 text-red-800 font-bold'
                      }`}
                    >
                      {host.status_label}
                    </span>
                  </div>

                  {/* Host Card Body */}
                  <div className="p-4 space-y-3 flex-1">
                    {/* Primary Reality Lane */}
                    {reality && (
                      <div 
                        onClick={() => onOpenReport(reality)}
                        className="p-3 bg-slate-50 hover:bg-slate-100/80 rounded-xl border border-slate-200 transition cursor-pointer space-y-1.5"
                      >
                        <div className="flex items-center justify-between">
                          <div className="flex items-center space-x-2">
                            <span className={`w-2 h-2 rounded-full ${reality.online ? 'bg-emerald-500' : 'bg-red-500'}`} />
                            <span className="font-bold text-xs text-slate-900">主力直连线路 (Reality)</span>
                          </div>
                          <span className="text-2xs font-mono px-1.5 py-0.2 rounded bg-blue-50 text-blue-700 font-semibold border border-blue-200">
                            VLESS Reality
                          </span>
                        </div>

                        <div className="flex items-center justify-between text-xs pt-1 font-mono">
                          <div className="text-slate-600">
                            RTT 时延: <span className="font-bold text-slate-900">{reality.latency_ms > 0 ? `${reality.latency_ms} ms` : (isInspecting ? '探测中...' : '待测')}</span>
                          </div>
                          <div className="text-blue-700 font-semibold">
                            {(reality.speed?.speed_mb_s ?? reality.speed_result?.bandwidth_mb_s)
                              ? `${(reality.speed?.speed_mb_s ?? reality.speed_result?.bandwidth_mb_s)!.toFixed(1)} MB/s`
                              : (reality.online ? '可用' : '离线')}
                          </div>
                        </div>
                      </div>
                    )}

                    {/* CF-WS Emergency Rescue Lane */}
                    {cfWs && (
                      <div 
                        onClick={() => onOpenReport(cfWs)}
                        className={`p-3 rounded-xl border transition cursor-pointer space-y-1.5 ${
                          host.is_rescue_active
                            ? 'bg-emerald-50 border-emerald-300 ring-1 ring-emerald-400'
                            : 'bg-slate-50/60 border-dashed border-slate-300 opacity-80 hover:opacity-100'
                        }`}
                      >
                        <div className="flex items-center justify-between">
                          <div className="flex items-center space-x-2">
                            <ShieldCheck className={`w-3.5 h-3.5 ${host.is_rescue_active ? 'text-emerald-600' : 'text-slate-400'}`} />
                            <span className="font-semibold text-xs text-slate-800">
                              CF-WS 应急救援节点 (443 救活)
                            </span>
                          </div>
                          {host.is_rescue_active ? (
                            <span className="px-2 py-0.5 rounded text-2xs font-bold bg-emerald-600 text-white animate-pulse">
                              🚨 已自动接管
                            </span>
                          ) : (
                            <span className="px-1.5 py-0.2 rounded text-2xs font-mono bg-slate-200 text-slate-600">
                              待命热备 (Standby)
                            </span>
                          )}
                        </div>

                        <div className="flex items-center justify-between text-xs pt-1 font-mono text-slate-500">
                          <span>端口: 443 (Cloudflare)</span>
                          <span>{host.is_rescue_active ? '活跃分发中' : '直连正常时隐藏'}</span>
                        </div>
                      </div>
                    )}

                    {isBlocked && reality && (
                      <div className="pt-1">
                        <button
                          onClick={() => onOpenHealingWizard(reality.name)}
                          className="w-full py-1.5 px-3 bg-red-600 hover:bg-red-700 text-white rounded-lg text-xs font-semibold shadow-2xs transition flex items-center justify-center"
                        >
                          <Wrench className="w-3.5 h-3.5 mr-1.5" />
                          启动换端口自愈向导
                        </button>
                      </div>
                    )}
                  </div>

                  {/* Host Card Footer */}
                  <div className="p-3 bg-slate-50 border-t border-slate-200 flex items-center justify-between text-2xs">
                    <span className="text-slate-400">已部署 sing-box 内核</span>
                    {reality && (
                      <button
                        onClick={() => onOpenReport(reality)}
                        className="text-blue-600 hover:text-blue-800 font-semibold flex items-center"
                      >
                        查看详细诊断
                        <ArrowRight className="w-3 h-3 ml-1" />
                      </button>
                    )}
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </div>

      {/* 3. SECTION 2: REAL RESIDENTIAL NODES (FROM BACKEND ATOMS & CHAINS) */}
      <div className="space-y-4 pt-4 border-t border-slate-200">
        <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
          <div>
            <div className="flex items-center space-x-2">
              <Home className="w-5 h-5 text-purple-600" />
              <h2 className="text-sm font-bold text-slate-900 tracking-tight">
                原生家宽与链式网络（真实后端原子库 · 直连与中继链路透视）
              </h2>
            </div>
            <p className="text-xs text-slate-500 mt-0.5">
              共挂载 {residentialAtoms.length} 条真实原生家宽 · {customChains.length} 条网状中继绑定 · 零 Mock 纯真实驱动
            </p>
          </div>

          <div className="flex items-center space-x-2">
            <button
              onClick={fetchResiData}
              disabled={loadingAtoms}
              className="inline-flex items-center px-2.5 py-1.5 text-xs font-medium rounded-lg text-slate-700 bg-white border border-slate-300 hover:bg-slate-50 transition"
              title="刷新家宽原子库"
            >
              <RefreshCw className={`w-3.5 h-3.5 mr-1 ${loadingAtoms ? 'animate-spin' : ''}`} />
              刷新
            </button>
            <button
              onClick={() => setShowAddResiModal(true)}
              className="inline-flex items-center px-3.5 py-1.5 text-xs font-semibold rounded-lg text-white bg-purple-600 hover:bg-purple-700 transition shadow-2xs"
            >
              <Plus className="w-3.5 h-3.5 mr-1" />
              添加家宽节点
            </button>
          </div>
        </div>

        {/* Residential Card Stream */}
        {residentialAtoms.length === 0 ? (
          <div className="p-8 bg-white border border-slate-200 rounded-xl text-center space-y-2">
            <Home className="w-8 h-8 text-slate-400 mx-auto" />
            <p className="text-xs font-semibold text-slate-700">暂无原生家宽原子节点</p>
            <p className="text-2xs text-slate-400 max-w-md mx-auto">
              原生家宽节点由 SubStore 订阅（如 ashan-us-resi、ashan-jp-resi）解析提供。点击上方「添加家宽节点」即可快速录入！
            </p>
          </div>
        ) : (
          <div className="space-y-3">
            {residentialAtoms.map((atom) => {
              // Match if any chain uses this residential node
              const matchedChain = customChains.find((c) => c.residential_name === atom.name || (c.chain_name && c.chain_name.includes(atom.name)));
              const flag = atom.country === 'JP' ? '🇯🇵' : (atom.country === 'US' ? '🇺🇸' : (atom.country === 'TW' ? '🇹🇼' : '🌐'));

              // Match corresponding probe result from statusMap if available
              const probeNode: NodeProbeResult = rawNodes.find((n) => n.name === atom.name) || {
                id: atom.id,
                name: atom.name,
                protocol: atom.protocol || 'vless',
                online: atom.latency_ms > 0,
                latency_ms: atom.latency_ms,
                china_exit: atom.china_exit,
                exit_country: atom.country,
                category: '原生家宽',
                original_url: atom.name,
              };

              return (
                <div
                  key={atom.id}
                  className="bg-white border border-slate-200 rounded-xl p-4 shadow-2xs hover:border-slate-300 transition"
                >
                  {/* Card Header */}
                  <div className="flex flex-col sm:flex-row sm:items-center justify-between pb-3 border-b border-slate-100 gap-2">
                    <div className="flex items-start sm:items-center space-x-2.5">
                      <span className="text-xl mt-0.5 sm:mt-0">{flag}</span>
                      <div>
                        <div className="flex items-center space-x-2">
                          <span className="font-bold text-xs text-slate-900">{atom.name}</span>
                          <span className="text-2xs font-mono px-1.5 py-0.2 rounded bg-purple-50 text-purple-700 font-semibold border border-purple-200 uppercase">
                            {atom.infra_type || atom.protocol || '原生家宽'}
                          </span>
                        </div>

                        {/* Badges & Tags */}
                        <div className="flex flex-wrap items-center gap-1.5 mt-1">
                          {!atom.china_exit ? (
                            <span className="inline-flex items-center px-1.5 py-0.5 rounded text-2xs font-mono bg-emerald-50 text-emerald-700 border border-emerald-200">
                              <ShieldCheck className="w-3 h-3 mr-0.5 text-emerald-600" />
                              NCR 纯净
                            </span>
                          ) : (
                            <span className="inline-flex items-center px-1.5 py-0.5 rounded text-2xs font-mono bg-amber-50 text-amber-700 border border-amber-200">
                              <ShieldAlert className="w-3 h-3 mr-0.5 text-amber-600" />
                              送中出口
                            </span>
                          )}

                          {atom.tags && atom.tags.map((t) => (
                            <span key={t} className="text-2xs font-mono px-1.5 py-0.5 rounded bg-slate-100 text-slate-600 border border-slate-200">
                              #{t}
                            </span>
                          ))}
                        </div>
                      </div>
                    </div>

                    <div className="flex items-center space-x-2 self-end sm:self-auto">
                      <button
                        onClick={() => onOpenReport(probeNode, 'ip_intelligence')}
                        className="inline-flex items-center px-2.5 py-1 text-2xs font-semibold rounded-lg text-blue-700 bg-blue-50 hover:bg-blue-100 border border-blue-200 transition"
                      >
                        <Globe className="w-3 h-3 mr-1 text-blue-600" />
                        IP 体检
                      </button>
                      <button
                        onClick={() => onOpenReport(probeNode, 'history')}
                        className="inline-flex items-center px-2.5 py-1 text-2xs font-medium rounded-lg text-slate-700 bg-slate-100 hover:bg-slate-200 transition"
                      >
                        <FileText className="w-3 h-3 mr-1 text-slate-500" />
                        测试记录
                      </button>
                    </div>
                  </div>

                  {/* Direct vs Chained Optimization Comparison */}
                  <div className="grid grid-cols-1 md:grid-cols-2 gap-3 mt-3">
                    {/* Left: Native Direct Lane */}
                    <div 
                      onClick={() => onOpenReport(probeNode)}
                      className="p-3 bg-slate-50 rounded-lg border border-slate-200 hover:bg-slate-100/70 cursor-pointer transition space-y-1"
                    >
                      <div className="flex items-center justify-between">
                        <span className="text-xs font-semibold text-slate-700 flex items-center">
                          <span className={`w-2 h-2 rounded-full ${atom.latency_ms > 0 ? 'bg-emerald-500' : 'bg-slate-400'} mr-1.5`} />
                          🔌 原生直连通道
                        </span>
                        <span className="text-2xs font-medium text-slate-500">
                          {atom.latency_ms > 0 ? '已连通' : '待巡检'}
                        </span>
                      </div>
                      <div className="flex items-center justify-between text-xs font-mono pt-1 text-slate-600">
                        <span>直连 RTT: <b className="text-slate-800">{atom.latency_ms > 0 ? `${atom.latency_ms} ms` : '—'}</b></span>
                        <span>国家: <b className="text-slate-800">{atom.country}</b></span>
                      </div>
                    </div>

                    {/* Right: Smart Mesh Chain Optimization Lane */}
                    <div 
                      className={`p-3 rounded-lg border transition space-y-1 ${
                        matchedChain 
                          ? 'bg-purple-50/60 border-purple-200 hover:bg-purple-100/70 cursor-pointer' 
                          : 'bg-slate-50/40 border-dashed border-slate-200 text-slate-400'
                      }`}
                    >
                      <div className="flex items-center justify-between">
                        <span className={`text-xs font-bold flex items-center ${matchedChain ? 'text-purple-900' : 'text-slate-400'}`}>
                          <span className={`w-2 h-2 rounded-full ${matchedChain ? 'bg-emerald-500' : 'bg-slate-300'} mr-1.5`} />
                          🚀 智能网状中继 (链式优化)
                        </span>
                        {matchedChain ? (
                          <span className="px-1.5 py-0.2 rounded text-2xs font-semibold bg-emerald-100 text-emerald-800">
                            已绑定链路
                          </span>
                        ) : (
                          <span className="text-2xs text-slate-400">
                            未绑定中转前置
                          </span>
                        )}
                      </div>
                      <div className="flex items-center justify-between text-xs font-mono pt-1">
                        <span className="text-purple-800 truncate max-w-xs text-2xs">
                          {matchedChain ? `前置: ${matchedChain.transit_name}` : '待编排中继链路'}
                        </span>
                        <span className="text-emerald-700 font-bold">
                          {matchedChain ? '网状就绪' : '—'}
                        </span>
                      </div>
                    </div>
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </div>

      {/* Add Residential Node Modal */}
      {showAddResiModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/50 backdrop-blur-2xs p-4">
          <div className="bg-white rounded-xl shadow-xl max-w-md w-full border border-slate-200 p-6 animate-in zoom-in-95 duration-150">
            <h3 className="font-bold text-sm text-slate-900 mb-1">动态添加家宽代理节点</h3>
            <p className="text-2xs text-slate-500 mb-4">
              直接持久化至 SubStore 订阅文件，并自动接入原子库与网状中继编排。
            </p>

            <form onSubmit={handleAddResi} className="space-y-4 text-xs">
              <div>
                <label className="block font-semibold text-slate-700 mb-1">目标家宽订阅归属</label>
                <select
                  value={targetSubGroup}
                  onChange={(e: any) => setTargetSubGroup(e.target.value)}
                  className="w-full px-3 py-2 border border-slate-300 rounded-lg focus:ring-1 focus:ring-purple-600 focus:outline-none bg-white text-slate-800"
                >
                  <option value="ashan-us-resi">ashan-us-resi (美国家宽池)</option>
                  <option value="ashan-jp-resi">ashan-jp-resi (日本家宽池)</option>
                  <option value="ashan-other">ashan-other (其他地区家宽池)</option>
                </select>
              </div>

              <div>
                <label className="block font-semibold text-slate-700 mb-1">家宽节点名称</label>
                <input
                  type="text"
                  required
                  value={newResiName}
                  onChange={(e) => setNewResiName(e.target.value)}
                  placeholder="例如: US-Pavlov-家宽-11"
                  className="w-full px-3 py-2 border border-slate-300 rounded-lg focus:ring-1 focus:ring-purple-600 focus:outline-none"
                />
              </div>

              <div>
                <label className="block font-semibold text-slate-700 mb-1">节点链接 (Socks5 / Vless / Shadowsocks)</label>
                <textarea
                  rows={3}
                  required
                  value={newResiURL}
                  onChange={(e) => setNewResiURL(e.target.value)}
                  placeholder="socks5://user:pass@167.104.x.x:port#Name"
                  className="w-full px-3 py-2 border border-slate-300 rounded-lg font-mono text-2xs focus:ring-1 focus:ring-purple-600 focus:outline-none"
                />
              </div>

              <div className="flex justify-end space-x-2 pt-2">
                <button
                  type="button"
                  onClick={() => setShowAddResiModal(false)}
                  className="px-3.5 py-1.5 text-slate-600 hover:bg-slate-100 rounded-lg font-medium"
                >
                  取消
                </button>
                <button
                  type="submit"
                  disabled={isSubmittingResi}
                  className="px-4 py-1.5 bg-purple-600 hover:bg-purple-700 text-white rounded-lg font-semibold shadow-2xs disabled:opacity-50"
                >
                  {isSubmittingResi ? '持久化中...' : '确认添加至 SubStore'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
};
