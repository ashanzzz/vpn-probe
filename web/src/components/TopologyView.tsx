import React, { useState, useEffect, useMemo } from 'react';
import { 
  Network, 
  RefreshCw, 
  Server, 
  ShieldCheck, 
  ShieldAlert, 
  ArrowRight, 
  Settings, 
  ExternalLink,
  Layers,
  Radio,
  CheckCircle2,
  XCircle,
  AlertTriangle
} from 'lucide-react';
import { api } from '../api/client';
import { useFeedback } from '../context/FeedbackContext';
import type { 
  TopologyOverviewResponse, 
  TopologyTransitNode, 
  TopologyNATRelayNode, 
  TopologyLandingNode, 
  TopologyNATPortItem,
  NodeProbeResult 
} from '../types';

interface TopologyViewProps {
  onOpenReport?: (node: NodeProbeResult) => void;
}

export const TopologyView: React.FC<TopologyViewProps> = ({ onOpenReport }) => {
  const { navigateTo, pushActivity } = useFeedback();
  const [data, setData] = useState<TopologyOverviewResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Region filter for NAT Stage 3: 'ALL' | 'JP' | 'US' | 'HK' | 'OTHER'
  const [selectedRegion, setSelectedRegion] = useState<string>('ALL');

  // Interactive path selection
  const [selectedTransitId, setSelectedTransitId] = useState<string | null>(null);
  const [selectedPort, setSelectedPort] = useState<number | null>(null);
  const [selectedLandingId, setSelectedLandingId] = useState<string | null>(null);

  const fetchTopology = async () => {
    setLoading(true);
    setError(null);
    try {
      const res = await api.getTopologyOverview();
      setData(res);
    } catch (err: any) {
      const msg = err.message || '获取网络拓扑数据失败';
      setError(msg);
      pushActivity({ level: 'error', source: '拓扑', title: '加载四阶拓扑失败', detail: msg });
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchTopology();
  }, []);

  // Filtered NAT Relays by region (supports multi-region NAT host by filtering its port pool)
  const filteredNATRelays = useMemo(() => {
    if (!data) return [];
    if (selectedRegion === 'ALL') return data.nat_relays;
    const regUp = selectedRegion.toUpperCase();
    return data.nat_relays
      .map((nat) => {
        const matchingPorts = nat.ports.filter(
          (p) => (p.region || nat.region || '').toUpperCase() === regUp
        );
        if (matchingPorts.length > 0) {
          return { ...nat, ports: matchingPorts };
        }
        if (nat.region.toUpperCase() === regUp) {
          return nat;
        }
        return null;
      })
      .filter((n): n is TopologyNATRelayNode => n !== null);
  }, [data, selectedRegion]);

  // Available regions for selector (drawn from port regions as well as NAT host regions)
  const availableRegions = useMemo(() => {
    if (!data) return ['ALL'];
    const regs = new Set<string>();
    data.nat_relays.forEach((n) => {
      if (n.region && n.region.toUpperCase() !== 'MULTI') {
        regs.add(n.region.toUpperCase());
      }
      n.ports.forEach((p) => {
        if (p.region) regs.add(p.region.toUpperCase());
      });
    });
    return ['ALL', ...Array.from(regs).sort()];
  }, [data]);

  // Compute active four-stage chain paths & highlight sets from data.links
  const { highlightedTransitIds, highlightedPorts, highlightedLandingIds, matchedChains } = useMemo(() => {
    const tSet = new Set<string>();
    const pSet = new Set<number>();
    const lSet = new Set<string>();
    const chains: Array<{
      id: string;
      transitName: string;
      transitOnline: boolean;
      natIP: string;
      port: number;
      landingTag: string;
      landingIP: string;
      status: 'ok' | 'fault' | 'warn';
      latencyMs: number;
    }> = [];

    if (!data) {
      return { highlightedTransitIds: tSet, highlightedPorts: pSet, highlightedLandingIds: lSet, matchedChains: chains };
    }

    const transitById = new Map(data.transits.map((t) => [t.id, t]));
    const natById = new Map(data.nat_relays.map((n) => [n.id, n]));
    const landingById = new Map(data.landings.map((l) => [l.id, l]));

    const t2nEdges = data.links.filter((e) => e.stage_from === 2 && e.stage_to === 3);
    const n2lEdges = data.links.filter((e) => e.stage_from === 3 && e.stage_to === 4);

    for (const t2n of t2nEdges) {
      const port = t2n.port || 0;
      const n2l = n2lEdges.find((e) => e.source_id === t2n.target_id && (e.port || 0) === port);
      const landingId = n2l?.target_id || '';

      if (selectedTransitId && t2n.source_id !== selectedTransitId) continue;
      if (selectedPort && port !== selectedPort) continue;
      if (selectedLandingId && landingId !== selectedLandingId) continue;

      tSet.add(t2n.source_id);
      if (port > 0) pSet.add(port);
      if (landingId) lSet.add(landingId);

      const transit = transitById.get(t2n.source_id);
      const nat = natById.get(t2n.target_id);
      const landing = landingId ? landingById.get(landingId) : undefined;
      chains.push({
        id: t2n.id,
        transitName: transit?.name || t2n.source_id.replace(/^transit-/, ''),
        transitOnline: transit?.online ?? true,
        natIP: nat?.public_ip || t2n.target_id.replace(/^nat-/, ''),
        port,
        landingTag: landing?.tag || nat?.ports.find((p) => p.port === port)?.target_node || '未知家宽',
        landingIP: landing?.exit_ip || '',
        status: t2n.status,
        latencyMs: t2n.latency_ms,
      });
    }

    // If a landing or port was selected and had direct (non-chain) edges only
    if (selectedLandingId || selectedPort) {
      for (const n2l of n2lEdges) {
        const port = n2l.port || 0;
        if (selectedPort && port !== selectedPort) continue;
        if (selectedLandingId && n2l.target_id !== selectedLandingId) continue;
        if (port > 0) pSet.add(port);
        lSet.add(n2l.target_id);
      }
    }

    return { highlightedTransitIds: tSet, highlightedPorts: pSet, highlightedLandingIds: lSet, matchedChains: chains };
  }, [data, selectedTransitId, selectedPort, selectedLandingId]);

  const hasSelection = Boolean(selectedTransitId || selectedPort || selectedLandingId);

  return (
    <div className="space-y-6">
      {/* 1. Header & Summary Metric Strip */}
      <div className="bg-white border border-slate-200 rounded-2xl p-5 shadow-xs">
        <div className="flex flex-col md:flex-row md:items-center md:justify-between gap-4 pb-4 border-b border-slate-100">
          <div>
            <div className="flex items-center space-x-2">
              <Network className="w-5 h-5 text-blue-600" />
              <h2 className="text-base font-bold text-slate-900 tracking-tight">
                四阶全景网络拓扑看板
              </h2>
              <span className="text-xs px-2.5 py-0.5 rounded-full font-mono bg-blue-50 text-blue-700 border border-blue-200">
                [客户端 ➔ 自建线路 ➔ 多区NAT端口 ➔ 家宽落地]
              </span>
            </div>
            <p className="text-xs text-slate-500 mt-1">
              展示全量住宅 IP、懒猫云/自建多区 NAT 中转、动态端口映射池与端到端分段时延链路
            </p>
          </div>

          <div className="flex items-center space-x-3">
            <button
              type="button"
              onClick={() => navigateTo('settings', 'speedtest')}
              className="flex items-center space-x-1.5 px-3 py-1.5 rounded-lg border border-slate-300 text-xs font-medium text-slate-700 hover:bg-slate-50 transition"
            >
              <Settings className="w-3.5 h-3.5 text-slate-500" />
              <span>配置测速源 ↗</span>
            </button>
            <button
              type="button"
              onClick={fetchTopology}
              disabled={loading}
              className="flex items-center space-x-1.5 px-3.5 py-1.5 rounded-lg bg-blue-600 text-white text-xs font-semibold hover:bg-blue-700 transition shadow-xs disabled:opacity-50"
            >
              <RefreshCw className={`w-3.5 h-3.5 ${loading ? 'animate-spin' : ''}`} />
              <span>刷新拓扑</span>
            </button>
          </div>
        </div>

        {/* Metric Badges */}
        {data && (
          <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-6 gap-3 pt-4">
            <div className="bg-slate-50 border border-slate-200/80 rounded-xl p-3">
              <div className="text-[11px] font-medium text-slate-500">家宽落地节点</div>
              <div className="text-lg font-bold text-slate-900 font-mono mt-0.5">
                {data.summary.total_landings} <span className="text-[10px] text-slate-400 font-normal">[个]</span>
              </div>
            </div>

            <div className="bg-slate-50 border border-slate-200/80 rounded-xl p-3">
              <div className="text-[11px] font-medium text-slate-500">NAT 中转宿主</div>
              <div className="text-lg font-bold text-amber-700 font-mono mt-0.5">
                {data.summary.total_nat_relays} <span className="text-[10px] text-slate-400 font-normal">[区]</span>
              </div>
            </div>

            <div className="bg-slate-50 border border-slate-200/80 rounded-xl p-3">
              <div className="text-[11px] font-medium text-slate-500">自建专线线路机</div>
              <div className="text-lg font-bold text-blue-700 font-mono mt-0.5">
                {data.summary.total_transits} <span className="text-[10px] text-slate-400 font-normal">[台]</span>
              </div>
            </div>

            <div className="bg-slate-50 border border-slate-200/80 rounded-xl p-3">
              <div className="text-[11px] font-medium text-slate-500">映射端口池</div>
              <div className="text-lg font-bold text-indigo-700 font-mono mt-0.5">
                {data.summary.total_ports} <span className="text-[10px] text-slate-400 font-normal">[口]</span>
              </div>
            </div>

            <div className="bg-emerald-50/60 border border-emerald-200/80 rounded-xl p-3">
              <div className="text-[11px] font-medium text-emerald-800">通畅链路</div>
              <div className="text-lg font-bold text-emerald-700 font-mono mt-0.5">
                {data.summary.online_links} <span className="text-[10px] text-emerald-600 font-normal">[在线]</span>
              </div>
            </div>

            <div className={`rounded-xl p-3 border ${
              data.summary.fault_links > 0 
                ? 'bg-rose-50 border-rose-200' 
                : 'bg-slate-50 border-slate-200/80'
            }`}>
              <div className={`text-[11px] font-medium ${
                data.summary.fault_links > 0 ? 'text-rose-700 font-bold' : 'text-slate-500'
              }`}>
                中断故障链路
              </div>
              <div className={`text-lg font-bold font-mono mt-0.5 ${
                data.summary.fault_links > 0 ? 'text-rose-600' : 'text-slate-400'
              }`}>
                {data.summary.fault_links} <span className="text-[10px] font-normal">{data.summary.fault_links > 0 ? '[中断]' : '[零故障]'}</span>
              </div>
            </div>
          </div>
        )}
      </div>

      {/* 2. Region Switcher Filter */}
      <div className="flex items-center justify-between bg-slate-100/70 p-2 rounded-xl border border-slate-200">
        <div className="flex items-center space-x-1">
          <span className="text-xs font-semibold text-slate-600 px-2">中转区域切换:</span>
          {availableRegions.map((reg) => (
            <button
              key={reg}
              onClick={() => setSelectedRegion(reg)}
              className={`px-3 py-1 rounded-lg text-xs font-bold transition ${
                selectedRegion === reg
                  ? 'bg-white text-blue-700 shadow-xs border border-slate-200'
                  : 'text-slate-600 hover:text-slate-900'
              }`}
            >
              {reg === 'ALL' ? '全部中转区' : `${reg} 区域`}
            </button>
          ))}
        </div>

        {(selectedTransitId || selectedPort || selectedLandingId) && (
          <button
            onClick={() => {
              setSelectedTransitId(null);
              setSelectedPort(null);
              setSelectedLandingId(null);
            }}
            className="text-[11px] font-mono text-blue-600 hover:underline px-2"
          >
            清除高亮选区
          </button>
        )}
      </div>

      {error && (
        <div className="bg-rose-50 border border-rose-200 text-rose-700 text-xs p-4 rounded-xl flex items-center space-x-2">
          <AlertTriangle className="w-4 h-4 shrink-0" />
          <span>{error}</span>
        </div>
      )}

      {/* 3. Four-Stage Pipeline Grid */}
      <div className="grid grid-cols-1 md:grid-cols-4 gap-4 items-start">
        {/* Stage 1: Client */}
        <div className="space-y-3">
          <div className="flex items-center space-x-2 pb-2 border-b-2 border-slate-400">
            <span className="w-5 h-5 rounded-full bg-slate-700 text-white text-[11px] font-bold flex items-center justify-center">1</span>
            <h3 className="text-xs font-bold uppercase tracking-wider text-slate-800">客户端接入</h3>
          </div>

          <div className="bg-white border border-slate-200 rounded-xl p-4 shadow-xs space-y-3">
            <div className="flex items-center justify-between">
              <div className="flex items-center space-x-2">
                <Radio className="w-4 h-4 text-emerald-600 animate-pulse" />
                <span className="text-xs font-bold text-slate-900">本地客户端 / 探针</span>
              </div>
              <span className="text-[10px] font-mono px-2 py-0.5 rounded bg-emerald-50 text-emerald-700 border border-emerald-200">
                [在线就绪]
              </span>
            </div>

            <div className="text-[11px] text-slate-500 font-mono space-y-1 bg-slate-50 p-2.5 rounded-lg border border-slate-100">
              <div>接入协议: SOCKS5 / HTTP</div>
              <div>发起模式: 沙箱隔离进程</div>
              <div>基准时延: ~0ms (本地环回)</div>
            </div>

            <div className="text-[10px] text-slate-400 border-t border-slate-100 pt-2">
              全流量统一由客户端经由前置线路机或直接 NAT 端口出海
            </div>
          </div>
        </div>

        {/* Stage 2: Transit Nodes */}
        <div className="space-y-3">
          <div className="flex items-center space-x-2 pb-2 border-b-2 border-blue-500">
            <span className="w-5 h-5 rounded-full bg-blue-600 text-white text-[11px] font-bold flex items-center justify-center">2</span>
            <h3 className="text-xs font-bold uppercase tracking-wider text-slate-800">自建专线线路机</h3>
          </div>

          <div className="space-y-2.5 max-h-[700px] overflow-y-auto pr-1">
            {(!data || data.transits.length === 0) ? (
              <div className="bg-white border border-dashed border-slate-200 rounded-xl p-4 text-center text-xs text-slate-400">
                暂无链式专线前置机 (直连模式透传)
              </div>
            ) : (
              data.transits.map((transit) => {
                const isSelected = selectedTransitId === transit.id;
                const isHighlighted = !hasSelection || highlightedTransitIds.has(transit.id);
                return (
                  <div
                    key={transit.id}
                    onClick={() => setSelectedTransitId(isSelected ? null : transit.id)}
                    className={`bg-white border rounded-xl p-3.5 shadow-xs transition cursor-pointer ${
                      isSelected
                        ? 'border-blue-500 ring-2 ring-blue-300 bg-blue-50/20'
                        : transit.online
                        ? 'border-slate-200 hover:border-blue-300'
                        : 'border-rose-200 bg-rose-50/20'
                    } ${!isHighlighted ? 'opacity-45' : ''}`}
                  >
                    <div className="flex items-center justify-between">
                      <span className="text-xs font-bold text-slate-900 truncate max-w-[140px]" title={transit.name}>
                        {transit.name}
                      </span>
                      <span className={`text-[10px] font-mono px-2 py-0.5 rounded ${
                        transit.online
                          ? 'bg-blue-50 text-blue-700 border border-blue-200'
                          : 'bg-rose-50 text-rose-700 border border-rose-200'
                      }`}>
                        {transit.online ? `[在线] ${transit.latency_ms}ms` : '[离线]'}
                      </span>
                    </div>

                    <div className="text-[11px] text-slate-500 font-mono mt-2 flex items-center justify-between">
                      <span>区域: {transit.country}</span>
                      <span className="text-slate-400 truncate max-w-[100px]">{transit.host}</span>
                    </div>
                  </div>
                );
              })
            )}
          </div>
        </div>

        {/* Stage 3: Multi-Region NAT Relays & Ports */}
        <div className="space-y-3">
          <div className="flex items-center space-x-2 pb-2 border-b-2 border-amber-500">
            <span className="w-5 h-5 rounded-full bg-amber-600 text-white text-[11px] font-bold flex items-center justify-center">3</span>
            <h3 className="text-xs font-bold uppercase tracking-wider text-slate-800">
              多区 NAT 中转与端口池
            </h3>
          </div>

          <div className="space-y-3 max-h-[700px] overflow-y-auto pr-1">
            {filteredNATRelays.length === 0 ? (
              <div className="bg-white border border-dashed border-slate-200 rounded-xl p-4 text-center text-xs text-slate-400">
                该区域暂无 NAT 中转实例
              </div>
            ) : (
              filteredNATRelays.map((nat) => (
                <div
                  key={nat.id}
                  className="bg-white border border-slate-200 rounded-xl p-3.5 shadow-xs space-y-2.5"
                >
                  <div className="flex items-center justify-between">
                    <div className="flex items-center space-x-1.5">
                      <Server className="w-3.5 h-3.5 text-amber-600" />
                      <span className="text-xs font-bold text-slate-900 truncate max-w-[120px]">
                        {nat.name}
                      </span>
                    </div>
                    <span className="text-[10px] font-mono px-2 py-0.5 rounded bg-amber-50 text-amber-800 border border-amber-200">
                      [{nat.region}] {nat.public_ip}
                    </span>
                  </div>

                  {/* Port Mapping Badges Pool */}
                  <div className="pt-1">
                    <div className="text-[10px] font-semibold text-slate-400 uppercase tracking-wider mb-1.5">
                      端口映射池 ({nat.ports.length} 个端口)
                    </div>
                    <div className="grid grid-cols-2 gap-1.5">
                      {nat.ports.map((p) => {
                        const isSelected = selectedPort === p.port;
                        const isPortMatched = !hasSelection || highlightedPorts.has(p.port);
                        return (
                          <button
                            key={p.port}
                            onClick={() => setSelectedPort(isSelected ? null : p.port)}
                            className={`p-2 rounded-lg text-left transition border text-[11px] font-mono ${
                              isSelected
                                ? 'bg-amber-100 border-amber-400 text-amber-900 ring-2 ring-amber-300'
                                : hasSelection && isPortMatched
                                ? 'bg-amber-50 border-amber-300 text-amber-900 ring-1 ring-amber-200'
                                : p.online
                                ? 'bg-slate-50 border-slate-200 hover:border-amber-300 text-slate-800'
                                : 'bg-rose-50 border-rose-200 text-rose-700'
                            } ${!isPortMatched ? 'opacity-45' : ''}`}
                          >
                            <div className="font-bold flex items-center justify-between">
                              <span>:{p.port}</span>
                              <span className="text-[9px] uppercase text-slate-400 font-sans">
                                {p.region ? `${p.region}·` : ''}{p.proto}
                              </span>
                            </div>
                            <div className="text-[10px] text-slate-500 truncate mt-0.5" title={p.target_node}>
                              {p.online ? `${p.latency_ms}ms` : '[断开]'} · {p.target_node}
                            </div>
                          </button>
                        );
                      })}
                    </div>
                  </div>
                </div>
              ))
            )}
          </div>
        </div>

        {/* Stage 4: Real Residential Landings */}
        <div className="space-y-3">
          <div className="flex items-center space-x-2 pb-2 border-b-2 border-emerald-500">
            <span className="w-5 h-5 rounded-full bg-emerald-600 text-white text-[11px] font-bold flex items-center justify-center">4</span>
            <h3 className="text-xs font-bold uppercase tracking-wider text-slate-800">真实家宽落地</h3>
          </div>

          <div className="space-y-2.5 max-h-[700px] overflow-y-auto pr-1">
            {(!data || data.landings.length === 0) ? (
              <div className="bg-white border border-dashed border-slate-200 rounded-xl p-4 text-center text-xs text-slate-400">
                暂无落地节点探测数据
              </div>
            ) : (
              data.landings.map((landing) => {
                const isSelected = selectedLandingId === landing.id;
                const isHighlighted = !hasSelection || highlightedLandingIds.has(landing.id);
                return (
                  <div
                    key={landing.id}
                    onClick={() => setSelectedLandingId(isSelected ? null : landing.id)}
                    className={`bg-white border rounded-xl p-3.5 shadow-xs transition cursor-pointer ${
                      isSelected
                        ? 'border-emerald-500 bg-emerald-50/20 ring-2 ring-emerald-300'
                        : hasSelection && isHighlighted
                        ? 'border-emerald-400 bg-emerald-50/10 ring-1 ring-emerald-200'
                        : landing.online
                        ? 'border-slate-200 hover:border-emerald-300'
                        : 'border-rose-200 bg-rose-50/30'
                    } ${!isHighlighted ? 'opacity-45' : ''}`}
                  >
                    <div className="flex items-center justify-between">
                      <span className="text-xs font-bold text-slate-900 truncate max-w-[130px]" title={landing.tag}>
                        {landing.tag}
                      </span>
                      <span className={`text-[10px] font-mono px-2 py-0.5 rounded font-bold ${
                        landing.online
                          ? 'bg-emerald-50 text-emerald-700 border border-emerald-200'
                          : 'bg-rose-50 text-rose-700 border border-rose-200'
                      }`}>
                        {landing.online ? '[在线]' : '[离线]'}
                      </span>
                    </div>

                    <div className="mt-2 text-[11px] font-mono text-slate-700 bg-slate-50 p-2 rounded border border-slate-100 space-y-0.5">
                      <div className="flex items-center justify-between">
                        <span className="text-slate-400">落地 IP:</span>
                        <span className="font-bold">{landing.exit_ip || '未获取'}</span>
                      </div>
                      <div className="flex items-center justify-between">
                        <span className="text-slate-400">归属地:</span>
                        <span>{landing.city || landing.country} ({landing.country})</span>
                      </div>
                      <div className="flex items-center justify-between">
                        <span className="text-slate-400">属性:</span>
                        <span className="font-bold text-blue-700">[{landing.ip_type}]</span>
                      </div>
                    </div>

                    <div className="mt-2.5 flex items-center justify-between text-[11px]">
                      <span className={`text-[10px] font-mono ${landing.china_exit ? 'text-amber-600' : 'text-emerald-600'}`}>
                        {landing.china_exit ? '[送中]' : '[不送中]'}
                      </span>
                      <span className="text-slate-400 text-[10px] truncate max-w-[120px]">{landing.isp}</span>
                    </div>
                  </div>
                );
              })
            )}
          </div>
        </div>
      </div>

      {/* 3.5 Live End-to-End Chained Path Verification Strip */}
      {data && matchedChains.length > 0 && (
        <div className="bg-white border border-slate-200 rounded-2xl p-4 shadow-xs space-y-3">
          <div className="flex items-center justify-between">
            <div className="flex items-center space-x-2">
              <Layers className="w-4 h-4 text-blue-600" />
              <h3 className="text-xs font-bold text-slate-900">
                实测四阶链式通路 ({matchedChains.filter((c) => c.status === 'ok').length}/{matchedChains.length} 条通畅)
              </h3>
            </div>
            <span className="text-[11px] text-slate-400 font-mono">
              点击上方线路机 / NAT端口 / 家宽节点可筛选对应链路
            </span>
          </div>

          <div className="grid grid-cols-1 lg:grid-cols-2 gap-2 max-h-[280px] overflow-y-auto pr-1">
            {matchedChains.map((c) => (
              <div
                key={c.id}
                className={`flex items-center justify-between px-3 py-2 rounded-xl border text-[11px] font-mono ${
                  c.status === 'ok'
                    ? 'bg-emerald-50/40 border-emerald-200 text-slate-800'
                    : 'bg-rose-50/50 border-rose-200 text-rose-800'
                }`}
              >
                <div className="flex items-center space-x-1.5 min-w-0 truncate">
                  <span className="px-1.5 py-0.5 rounded bg-blue-100 text-blue-800 font-semibold shrink-0">
                    {c.transitName}
                  </span>
                  <ArrowRight className="w-3 h-3 text-slate-400 shrink-0" />
                  <span className="px-1.5 py-0.5 rounded bg-amber-100 text-amber-800 shrink-0">
                    {c.natIP}:{c.port}
                  </span>
                  <ArrowRight className="w-3 h-3 text-slate-400 shrink-0" />
                  <span className="px-1.5 py-0.5 rounded bg-emerald-100 text-emerald-800 truncate" title={c.landingTag}>
                    {c.landingTag}
                  </span>
                </div>
                <span className={`ml-2 shrink-0 font-bold ${
                  c.status === 'ok' ? 'text-emerald-700' : 'text-rose-600'
                }`}>
                  {c.status === 'ok' ? `${c.latencyMs}ms` : '[中断]'}
                </span>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
};
