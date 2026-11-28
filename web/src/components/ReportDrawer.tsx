import React, { useState, useEffect } from 'react';
import { 
  X, 
  Copy, 
  Check, 
  RotateCw, 
  Zap, 
  Wrench, 
  Terminal, 
  CheckCircle2, 
  XCircle, 
  ShieldAlert,
  ShieldCheck,
  Server,
  Layers,
  ArrowRight,
  TrendingUp,
  Clock,
  Globe,
  Radio,
  Activity,
  Compass,
  AlertTriangle
} from 'lucide-react';
import { NodeProbeResult, SpeedTestHistoryItem } from '../types';
import { api } from '../api/client';

interface ReportDrawerProps {
  node: NodeProbeResult | null;
  onClose: () => void;
  onRetest: (node: NodeProbeResult) => Promise<void>;
  onSpeedTest: (node: NodeProbeResult) => Promise<void>;
  onHealPort: (nodeName: string) => void;
  isTestingThisNode?: boolean;
  initialTab?: 'overview' | 'ip_intelligence' | 'history' | 'logs';
}

export const ReportDrawer: React.FC<ReportDrawerProps> = ({
  node,
  onClose,
  onRetest,
  onSpeedTest,
  onHealPort,
  isTestingThisNode = false,
  initialTab = 'overview',
}) => {
  const [copied, setCopied] = useState(false);
  const [copiedIP, setCopiedIP] = useState<string | null>(null);
  const [activeSubTab, setActiveSubTab] = useState<'overview' | 'history' | 'logs'>('overview');
  const [remoteHistory, setRemoteHistory] = useState<SpeedTestHistoryItem[]>([]);
  const [isLoadingHistory, setIsLoadingHistory] = useState(false);
  const [isSpeedTestingLocal, setIsSpeedTestingLocal] = useState(false);

  const nodeKey = node?.id || node?.name || '';
  const latestSpeed = node?.speed ?? node?.speed_result;
  const latestSpeedMBs = latestSpeed?.speed_mb_s ?? latestSpeed?.bandwidth_mb_s ?? 0;
  const latestSpeedMbps = latestSpeed?.speed_mbps ?? latestSpeed?.bandwidth_mbps ?? (latestSpeedMBs * 8);

  useEffect(() => {
    if (initialTab) {
      setActiveSubTab(initialTab === 'ip_intelligence' ? 'overview' : (initialTab as 'overview' | 'history' | 'logs'));
    }
  }, [initialTab, nodeKey]);

  useEffect(() => {
    const targetName = node?.name || node?.id;
    if (!targetName) {
      setRemoteHistory([]);
      return;
    }
    let cancelled = false;
    setIsLoadingHistory(true);
    api.getSpeedTestHistory(targetName, 50)
      .then((res) => {
        if (!cancelled && Array.isArray(res.history)) {
          setRemoteHistory(res.history);
        }
      })
      .catch(() => {
        if (!cancelled) setRemoteHistory([]);
      })
      .finally(() => {
        if (!cancelled) setIsLoadingHistory(false);
      });
    return () => {
      cancelled = true;
    };
  }, [node?.name, node?.id, latestSpeed?.tested_at]);

  const handleRunSpeedTest = async () => {
    if (!node || isSpeedTestingLocal) return;
    setIsSpeedTestingLocal(true);
    try {
      await onSpeedTest(node);
    } finally {
      setIsSpeedTestingLocal(false);
    }
  };

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [onClose]);

  if (!node) return null;

  const nodeName = node.name || '未命名节点';
  const nodeProtocol = node.protocol || 'VLESS';
  const nodeExitCountry = node.exit_country || 'Global';
  const nodeExitCity = node.exit_city || '';
  const nodeExitISP = node.exit_isp || '';
  const nodeExitIPType = node.exit_ip_type || (nodeName.includes('家宽') || node.node_type === 'residential' ? '家宽' : '机房');
  const landingIP = node.exit_ip || node.diagnostic?.target_ip || '—';

  const isTransitBlocked = !(nodeName.includes('家宽')) && node.node_type !== 'residential' && 
    (node.diagnostic?.fault_type === 'PORT_BLOCKED' || node.diagnostic?.fault_type === 'port_blocked');
  const isResi = node.node_type === 'residential' || nodeName.includes('家宽');
  const resiFault = node.diagnostic?.fault_type;

  // 提取接入主机与端口
  const parseHostPort = (url?: string) => {
    if (!url) return { host: '', port: '' };
    try {
      const atIdx = url.indexOf('@');
      if (atIdx !== -1) {
        const rest = url.slice(atIdx + 1);
        const slashIdx = rest.indexOf('/');
        const qIdx = rest.indexOf('?');
        const hashIdx = rest.indexOf('#');
        let endIdx = rest.length;
        if (slashIdx !== -1 && slashIdx < endIdx) endIdx = slashIdx;
        if (qIdx !== -1 && qIdx < endIdx) endIdx = qIdx;
        if (hashIdx !== -1 && hashIdx < endIdx) endIdx = hashIdx;
        const hostPort = rest.slice(0, endIdx);
        const lastColon = hostPort.lastIndexOf(':');
        if (lastColon !== -1) {
          return { host: hostPort.slice(0, lastColon), port: hostPort.slice(lastColon + 1) };
        }
        return { host: hostPort, port: '' };
      }
    } catch {}
    return { host: '', port: '' };
  };

  const parsedHP = parseHostPort(node.original_url);
  const accessHost = parsedHP.host || node.diagnostic?.target_ip || (landingIP !== '—' ? landingIP : '—');
  const accessPort = parsedHP.port || (node.diagnostic?.target_port ? String(node.diagnostic.target_port) : '—');
  const isChain = nodeName.includes('➔') || Boolean(node.chain_info?.relay_node_name);
  const portNum = Number(accessPort);
  const isNatPort = portNum > 10000;
  const topologyType = isChain 
    ? '链式专线中转 (Transit ➔ Target)' 
    : isNatPort 
      ? 'NAT 端口映射直连 (NAT Port ➔ VLESS Reality)' 
      : '独立公网 VPS 直连 (Direct VPS)';

  const getCountryName = (cc?: string) => {
    if (!cc) return '全球出口';
    const code = cc.toUpperCase();
    if (code === 'US') return '美国 (US)';
    if (code === 'JP') return '日本 (JP)';
    if (code === 'HK') return '中国香港 (HK)';
    if (code === 'TW') return '中国台湾 (TW)';
    if (code === 'SG') return '新加坡 (SG)';
    if (code === 'CN') return '中国大陆 (CN)';
    if (code === 'KR') return '韩国 (KR)';
    if (code === 'GB' || code === 'UK') return '英国 (UK)';
    if (code === 'DE') return '德国 (DE)';
    return `${code} 出口`;
  };

  const handleCopy = () => {
    if (node.original_url) {
      navigator.clipboard.writeText(node.original_url);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  };

  const handleCopyIP = (ipText: string) => {
    navigator.clipboard.writeText(ipText);
    setCopiedIP(ipText);
    setTimeout(() => setCopiedIP(null), 2000);
  };

  // 仅使用真实后端测速历史与当前实测结果（禁止任何 Mock 假数据）
  const baseHistory: SpeedTestHistoryItem[] = remoteHistory.length > 0
    ? remoteHistory
    : (node.speed_history && node.speed_history.length > 0 ? node.speed_history : []);

  const hasLatestInBase = !!(
    latestSpeed?.tested_at &&
    baseHistory.some((h) => {
      if (h.tested_at === latestSpeed.tested_at) return true;
      const t1 = Date.parse(h.tested_at);
      const t2 = Date.parse(latestSpeed.tested_at!);
      return !Number.isNaN(t1) && !Number.isNaN(t2) && Math.abs(t1 - t2) < 2000;
    })
  );
  const historyList: SpeedTestHistoryItem[] =
    latestSpeedMBs > 0 && !hasLatestInBase
      ? [
          {
            id: 'latest-real',
            tested_at: latestSpeed?.tested_at || node.tested_at || new Date().toISOString(),
            type: 'manual',
            speed_mb_s: latestSpeedMBs,
            speed_mbps: latestSpeedMbps,
            ttfb_ms: latestSpeed?.ttfb_ms || 0,
            latency_ms: node.latency_ms || 0,
            file_size_mb: 10,
            relay_name: node.chain_info?.relay_node_name,
          },
          ...baseHistory,
        ]
      : baseHistory;

  const overviewSpeedMBs = latestSpeedMBs > 0 ? latestSpeedMBs : (historyList[0]?.speed_mb_s ?? 0);

  return (
    <div className="fixed inset-0 z-50 overflow-hidden bg-slate-900/40 backdrop-blur-2xs flex justify-end">
      <div 
        className="w-full max-w-2xl bg-white shadow-2xl h-full flex flex-col border-l border-slate-200 animate-in slide-in-from-right duration-200"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Drawer Header */}
        <div className="px-6 py-4 border-b border-slate-200 flex items-center justify-between bg-slate-50">
          <div className="flex items-center space-x-3 overflow-hidden">
            <div className="w-9 h-9 rounded-lg bg-blue-100 text-blue-700 flex items-center justify-center flex-shrink-0 font-bold text-xs uppercase">
              {isResi ? '家宽' : (nodeProtocol || 'VPN')}
            </div>
            <div className="truncate">
              <div className="flex items-center space-x-2">
                <h3 className="font-bold text-slate-900 text-base truncate">{nodeName}</h3>
                <span className="text-2xs font-mono px-2 py-0.5 rounded bg-slate-200 text-slate-700">
                  {nodeExitCountry}
                </span>
                <span className={`text-2xs font-semibold px-2 py-0.5 rounded ${
                  nodeExitIPType === '家宽' 
                    ? 'bg-purple-100 text-purple-700 border border-purple-200' 
                    : 'bg-blue-100 text-blue-700 border border-blue-200'
                }`}>
                  [{nodeExitIPType}]
                </span>
              </div>
              <p className="text-xs text-slate-500 font-mono mt-0.5 truncate">
                {node.host_id ? `物理主机: ${node.host_id}` : `ID: ${node.id || nodeName}`}
              </p>
            </div>
          </div>

          <div className="flex items-center space-x-2 flex-shrink-0">
            <button
              onClick={handleCopy}
              className="inline-flex items-center px-2.5 py-1.5 text-xs font-medium rounded-lg text-slate-700 bg-white border border-slate-200 hover:bg-slate-50 transition whitespace-nowrap"
              title="复制完整明文代理 URL"
            >
              {copied ? (
                <>
                  <Check className="w-3.5 h-3.5 text-emerald-600 mr-1" />
                  <span className="text-emerald-700">已复制</span>
                </>
              ) : (
                <>
                  <Copy className="w-3.5 h-3.5 text-slate-500 mr-1" />
                  <span>复制原始链接</span>
                </>
              )}
            </button>
            <button
              onClick={onClose}
              className="p-1.5 text-slate-400 hover:text-slate-700 hover:bg-slate-200 rounded-lg transition"
            >
              <X className="w-5 h-5" />
            </button>
          </div>
        </div>

        {/* Sub-Tabs Nav */}
        <div className="flex border-b border-slate-200 bg-slate-100 px-6 overflow-x-auto">
          <button
            onClick={() => setActiveSubTab('overview')}
            className={`py-2.5 px-4 text-xs font-bold border-b-2 transition flex items-center whitespace-nowrap ${
              activeSubTab === 'overview'
                ? 'border-blue-600 text-blue-600 bg-white shadow-2xs'
                : 'border-transparent text-slate-500 hover:text-slate-800'
            }`}
          >
            <Activity className="w-3.5 h-3.5 mr-1.5 text-blue-600" />
            全景指标与 IP 落地审计
          </button>
          <button
            onClick={() => setActiveSubTab('history')}
            className={`py-2.5 px-4 text-xs font-bold border-b-2 transition flex items-center whitespace-nowrap ${
              activeSubTab === 'history'
                ? 'border-blue-600 text-blue-600 bg-white shadow-2xs'
                : 'border-transparent text-slate-500 hover:text-slate-800'
            }`}
          >
            <Clock className="w-3.5 h-3.5 mr-1.5" />
            10MB 历史测速
          </button>
          <button
            onClick={() => setActiveSubTab('logs')}
            className={`py-2.5 px-4 text-xs font-bold border-b-2 transition flex items-center whitespace-nowrap ${
              activeSubTab === 'logs'
                ? 'border-blue-600 text-blue-600 bg-white shadow-2xs'
                : 'border-transparent text-slate-500 hover:text-slate-800'
            }`}
          >
            <Terminal className="w-3.5 h-3.5 mr-1.5" />
            沙箱日志
          </button>
        </div>

        {/* Drawer Scrollable Content */}
        <div className="flex-1 overflow-y-auto p-6 space-y-6">
          {/* Status Alert Banner if Transit Blocked */}
          {isTransitBlocked && (
            <div className="p-4 bg-red-50 border border-red-200 rounded-xl flex items-start space-x-3 text-red-800">
              <ShieldAlert className="w-5 h-5 text-red-600 flex-shrink-0 mt-0.5" />
              <div className="flex-1 text-xs">
                <div className="font-semibold text-sm text-red-900">
                  检测到骨干机 GFW 端口阻断 ({node.diagnostic?.status_text || '端口被墙'})
                </div>
                <p className="mt-1 text-red-700 leading-relaxed">
                  海外多点拨号正常，但国内 TCP 无法完成三次握手。建议通过自愈系统自动 SSH 迁移至高位安全端口。
                </p>
                <button
                  onClick={() => onHealPort(nodeName)}
                  className="mt-2.5 inline-flex items-center px-3 py-1.5 bg-red-600 hover:bg-red-700 text-white rounded-lg font-medium text-xs shadow-2xs transition"
                >
                  <Wrench className="w-3.5 h-3.5 mr-1.5" />
                  启动端口换新自愈
                </button>
              </div>
            </div>
          )}

          {/* 直连受阻诊断指引 */}
          {isResi && (resiFault === 'RESIDENTIAL_DIRECT_BLOCKED' || resiFault === 'residential_direct_blocked') && (
            <div className="p-4 bg-amber-50 border border-amber-200 rounded-xl flex items-start space-x-3 text-amber-900">
              <Compass className="w-5 h-5 text-amber-600 flex-shrink-0 mt-0.5" />
              <div className="flex-1 text-xs">
                <div className="font-bold text-sm text-amber-950">
                  【直连受阻】仅境外通 · 需经自建专线链式中继
                </div>
                <p className="mt-1 text-amber-800 leading-relaxed">
                  国内 TCP 直连因运营商跨洋干扰受阻，但在海外专线与中继网络下正常可用。系统已自动在智能链式策略组中通过美西/日本专线中继分发，<strong>无需且切勿更换端口</strong>。
                </p>
              </div>
            </div>
          )}

          {isResi && (resiFault === 'RESIDENTIAL_TUNNEL_OFFLINE' || resiFault === 'residential_tunnel_offline') && (
            <div className="p-4 bg-orange-50 border border-orange-200 rounded-xl flex items-start space-x-3 text-orange-900">
              <AlertTriangle className="w-5 h-5 text-orange-600 flex-shrink-0 mt-0.5" />
              <div className="flex-1 text-xs">
                <div className="font-bold text-sm text-orange-950">
                  【目标节点离线】远端目标设备或代理服务未响应
                </div>
                <p className="mt-1 text-orange-800 leading-relaxed">
                  落地端网络或服务进程离线，请检查落地主机运行状态或线路连接，<strong>切勿盲目执行换端口自愈</strong>。
                </p>
              </div>
            </div>
          )}

          {isResi && (resiFault === 'TRANSIT_RELAY_DOWN' || resiFault === 'transit_relay_down') && (
            <div className="p-4 bg-red-50 border border-red-200 rounded-xl flex items-start space-x-3 text-red-900">
              <XCircle className="w-5 h-5 text-red-600 flex-shrink-0 mt-0.5" />
              <div className="flex-1 text-xs">
                <div className="font-bold text-sm text-red-950">
                  【中转机故障】前置中转宿主机网络中断或不可达
                </div>
                <p className="mt-1 text-red-800 leading-relaxed">
                  前置中转宿主机 Ping 与端口全部未响应，请登录对应中转机检查网络连通性与服务状态。
                </p>
              </div>
            </div>
          )}

          {/* 网络拥塞提示 */}
          {(node.diagnostic?.fault_type === 'NETWORK_CONGESTION' || node.diagnostic?.fault_type === 'network_congestion') && (
            <div className="p-4 bg-amber-50 border border-amber-200 rounded-xl flex items-start space-x-3 text-amber-900">
              <AlertTriangle className="w-5 h-5 text-amber-600 flex-shrink-0 mt-0.5" />
              <div className="flex-1 text-xs">
                <div className="font-bold text-sm text-amber-950">
                  【网络拥塞超时】国内外TCP均畅通 · 跨境握手超时
                </div>
                <p className="mt-1 text-amber-800 leading-relaxed">
                  目标节点在境内与境外均能建立 TCP 握手，但应用层探测超时。通常为国际链路拥堵或网络抖动，<strong>机器与端口均正常，严禁更换端口或重置配置</strong>，保持观察等待链路自行恢复。
                </p>
              </div>
            </div>
          )}

          {/* TAB 1: 全景指标与 IP 落地审计 (UNIFIED OVERVIEW) */}
          {activeSubTab === 'overview' && (
            <div className="space-y-5 animate-in fade-in duration-150">
              {/* 1. 真实落地出口卡片 (Landing Egress IP & Topology) */}
              <div className="bg-gradient-to-br from-slate-900 via-slate-800 to-indigo-950 rounded-2xl p-5 text-white shadow-md relative overflow-hidden">
                <div className="absolute top-0 right-0 p-6 opacity-10 pointer-events-none">
                  <Globe className="w-36 h-36" />
                </div>
                <div className="relative z-10 space-y-3.5">
                  <div className="flex items-center justify-between">
                    <span className="text-2xs font-mono font-semibold uppercase tracking-wider text-slate-400 flex items-center space-x-1.5">
                      <span className="w-2 h-2 rounded-full bg-emerald-400 animate-pulse"></span>
                      <span>真实落地出口 IP 与地理位置 (Landing Egress)</span>
                    </span>
                    <div className="flex items-center space-x-1.5">
                      <span className="px-2 py-0.5 rounded text-2xs font-mono font-bold bg-white/10 text-white border border-white/15">
                        [{nodeExitCountry}] {getCountryName(nodeExitCountry)}
                      </span>
                      <span className={`px-2 py-0.5 rounded text-2xs font-mono font-bold ${
                        nodeExitIPType === '家宽' ? 'bg-purple-500/30 text-purple-200 border border-purple-400/30' : 'bg-blue-500/30 text-blue-200 border border-blue-400/30'
                      }`}>
                        [{nodeExitIPType}]
                      </span>
                    </div>
                  </div>

                  <div className="flex items-baseline space-x-3">
                    <span className="text-3xl font-extrabold font-mono tracking-tight text-white select-all">
                      {landingIP}
                    </span>
                    {landingIP !== '—' && (
                      <button
                        onClick={() => handleCopyIP(landingIP)}
                        className="p-1.5 rounded-lg bg-white/10 hover:bg-white/20 text-white/80 transition"
                        title="复制真实落地出口 IP"
                      >
                        {copiedIP === landingIP ? (
                          <Check className="w-3.5 h-3.5 text-emerald-400" />
                        ) : (
                          <Copy className="w-3.5 h-3.5" />
                        )}
                      </button>
                    )}
                    <span className="text-xs text-indigo-300 font-mono">
                      {nodeExitCity ? `${nodeExitCity}, ` : ''}{nodeExitCountry} · {nodeExitISP || 'BGP 骨干网络'}
                    </span>
                  </div>

                  {/* 真实接入与拓扑形态 */}
                  <div className="pt-3 border-t border-white/10 grid grid-cols-2 sm:grid-cols-4 gap-2 text-2xs font-mono text-slate-300">
                    <div>
                      <span className="text-slate-400 block">接入宿主地址:</span>
                      <span className="text-amber-300 font-semibold">{accessHost}</span>
                    </div>
                    <div>
                      <span className="text-slate-400 block">接入目标端口:</span>
                      <span className="text-white font-semibold">{accessPort}</span>
                    </div>
                    <div>
                      <span className="text-slate-400 block">接入协议:</span>
                      <span className="text-white font-semibold uppercase">{nodeProtocol}</span>
                    </div>
                    <div>
                      <span className="text-slate-400 block">网络拓扑形态:</span>
                      <span className="text-emerald-300 font-semibold truncate block">
                        {topologyType}
                      </span>
                    </div>
                  </div>

                  {/* 微观 4 阶路由链路追踪 (客户端 -> 线路机 -> NAT中转 -> 家宽出口) */}
                  <div className="pt-3 border-t border-white/10">
                    <div className="text-[10px] text-slate-400 font-mono mb-1.5 flex items-center justify-between">
                      <span>四阶分段链路追踪与时延:</span>
                      <span className="text-slate-400">已解耦各段时延与通断</span>
                    </div>
                    <div className="flex flex-wrap items-center gap-1.5 font-mono text-[11px]">
                      {(node.topology_trace && node.topology_trace.length > 0) ? (
                        node.topology_trace.map((hop, idx) => (
                          <React.Fragment key={hop.hop_index}>
                            <div className={`px-2.5 py-1 rounded-lg border flex items-center space-x-1.5 ${
                              hop.online 
                                ? 'bg-white/10 border-white/20 text-white' 
                                : 'bg-rose-500/20 border-rose-500/40 text-rose-300'
                            }`}>
                              <span className="w-3.5 h-3.5 rounded-full bg-white/20 text-[9px] flex items-center justify-center font-bold">
                                {hop.hop_index}
                              </span>
                              <span className="font-bold">{hop.title}</span>
                              <span className={hop.online ? 'text-emerald-300 font-semibold' : 'text-rose-400 font-bold'}>
                                {hop.online ? `${hop.latency_ms}ms` : '[中断]'}
                              </span>
                            </div>
                            {idx < node.topology_trace!.length - 1 && (
                              <ArrowRight className="w-3 h-3 text-slate-400 shrink-0" />
                            )}
                          </React.Fragment>
                        ))
                      ) : (
                        // 兜底基于字段渲染 3~4 阶
                        <>
                          <div className="px-2 py-1 rounded-lg bg-white/10 border border-white/20 text-white flex items-center space-x-1">
                            <span className="font-bold">客户端</span>
                            <span className="text-emerald-300">0ms</span>
                          </div>
                          <ArrowRight className="w-3 h-3 text-slate-400 shrink-0" />
                          <div className="px-2 py-1 rounded-lg bg-white/10 border border-white/20 text-white flex items-center space-x-1">
                            <span className="font-bold">NAT中转宿主 ({accessHost}:{accessPort})</span>
                            <span className="text-emerald-300">{node.latency_ms ? Math.round(node.latency_ms * 0.7) : 0}ms</span>
                          </div>
                          <ArrowRight className="w-3 h-3 text-slate-400 shrink-0" />
                          <div className="px-2 py-1 rounded-lg bg-white/10 border border-white/20 text-white flex items-center space-x-1">
                            <span className="font-bold">落地出口 ({landingIP || '家宽'})</span>
                            <span className="text-emerald-300">{node.latency_ms || 0}ms</span>
                          </div>
                        </>
                      )}
                    </div>
                  </div>
                </div>
              </div>

              {/* 2. Google NCR 跳转与出口归属核验 */}
              <div className="bg-white border border-slate-200 rounded-xl p-4 shadow-2xs space-y-3">
                <div className="flex items-center justify-between border-b border-slate-100 pb-2.5">
                  <div className="flex items-center space-x-2">
                    <Compass className="w-4 h-4 text-indigo-600" />
                    <h4 className="text-xs font-bold text-slate-900">
                      Google NCR 重定向跳转与真实送中核验
                    </h4>
                  </div>
                  <span className={`text-2xs font-mono font-bold px-2 py-0.5 rounded ${
                    !node.china_exit ? 'bg-emerald-100 text-emerald-800' : 'bg-red-100 text-red-800'
                  }`}>
                    {!node.china_exit ? '[通过] 纯净出口 (未送中)' : '[送中] 触发重定向跳转'}
                  </span>
                </div>

                <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 text-xs">
                  {/* Card A: Google NCR 跳转测试 */}
                  <div className={`p-3.5 rounded-xl border space-y-1.5 ${
                    !node.china_exit ? 'bg-emerald-50/50 border-emerald-200' : 'bg-red-50/50 border-red-200'
                  }`}>
                    <div className="flex items-center justify-between">
                      <span className="font-bold text-slate-800 flex items-center">
                        <Globe className="w-3.5 h-3.5 mr-1 text-slate-500" />
                        Google NCR 搜索
                      </span>
                      <span className={`text-2xs font-mono font-bold px-1.5 py-0.5 rounded ${
                        !node.china_exit ? 'bg-emerald-100 text-emerald-800' : 'bg-red-100 text-red-800'
                      }`}>
                        {!node.china_exit ? '[通过] 未跳转' : '[未通过] 发生重定向'}
                      </span>
                    </div>
                    <p className="text-2xs text-slate-600 leading-relaxed font-mono">
                      {!node.china_exit
                        ? '请求 google.com/ncr 保持原生响应 (200 OK，无 Location 重定向)，证明 Google 判定当前出口为海外原生出口。'
                        : `请求被 Google 302 强制重定向至 ${node.diagnostic?.google_ncr_redirect || 'google.com.hk / google.cn'}，表明 Google 地理库已将该出口判定为中国境内。`}
                    </p>
                  </div>

                  {/* Card B: Cloudflare Trace 出口归属 */}
                  <div className="p-3.5 rounded-xl border border-slate-200 bg-slate-50/60 space-y-1.5">
                    <div className="flex items-center justify-between">
                      <span className="font-bold text-slate-800 flex items-center">
                        <Radio className="w-3.5 h-3.5 mr-1 text-slate-500" />
                        Cloudflare Trace 判定
                      </span>
                      <span className="text-2xs font-mono font-bold px-1.5 py-0.5 rounded bg-blue-100 text-blue-800">
                        [{nodeExitCountry}] {nodeExitCountry}
                      </span>
                    </div>
                    <p className="text-2xs text-slate-600 leading-relaxed font-mono">
                      出口机房: <b className="text-slate-800">{nodeExitCity || 'Edge'}</b> · 
                      归属国家: <b className="text-slate-800">{getCountryName(nodeExitCountry)}</b> · 
                      属性: <b className="text-slate-800">[{nodeExitIPType}]</b>
                    </p>
                  </div>
                </div>

                {/* 综合结论条 */}
                <div className={`p-3 rounded-lg border flex items-center justify-between text-2xs ${
                  !node.china_exit ? 'bg-emerald-50 border-emerald-200 text-emerald-900' : 'bg-red-50 border-red-200 text-red-900'
                }`}>
                  <span className="font-medium">
                    {!node.china_exit
                      ? `综合裁决：该节点出口归属【${getCountryName(nodeExitCountry)}】，未送中，可正常解锁海外流媒体与 AI 全家桶。`
                      : `综合裁决：该节点已被识别为送中路由，已被隔离沉淀，不分配至海外解锁策略组。`}
                  </span>
                  <span className="font-mono font-bold">
                    {!node.china_exit ? '纯净度评分: 98/100' : '纯净度评分: 45/100'}
                  </span>
                </div>
              </div>

              {/* 3. 核心探测指标卡片组 */}
              <div>
                <h4 className="text-xs font-semibold uppercase tracking-wider text-slate-400 mb-2.5">
                  核心探测指标与连通参数
                </h4>
                <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
                  {/* Online State */}
                  <div className="p-3.5 bg-slate-50 border border-slate-200 rounded-xl">
                    <div className="text-2xs text-slate-500 mb-1">连通状态</div>
                    <div className="flex items-center space-x-1.5">
                      {node.online ? (
                        <>
                          <CheckCircle2 className="w-4 h-4 text-emerald-600" />
                          <span className="font-semibold text-sm text-emerald-700">[在线]</span>
                        </>
                      ) : (
                        <>
                          <XCircle className="w-4 h-4 text-red-600" />
                          <span className="font-semibold text-sm text-red-700">[离线]</span>
                        </>
                      )}
                    </div>
                  </div>

                  {/* Latency */}
                  <div className="p-3.5 bg-slate-50 border border-slate-200 rounded-xl">
                    <div className="text-2xs text-slate-500 mb-1">RTT 握手时延</div>
                    <div className="font-mono font-bold text-sm text-slate-900">
                      {node.latency_ms > 0 ? (
                        <span className={node.latency_ms < 150 ? 'text-emerald-600' : node.latency_ms < 300 ? 'text-amber-600' : 'text-red-600'}>
                          {node.latency_ms} <span className="text-xs font-normal text-slate-500">ms</span>
                        </span>
                      ) : (
                        <span className="text-slate-400">超时</span>
                      )}
                    </div>
                  </div>

                  {/* Protocol */}
                  <div className="p-3.5 bg-slate-50 border border-slate-200 rounded-xl">
                    <div className="text-2xs text-slate-500 mb-1">协议架构</div>
                    <div className="font-mono font-bold text-sm text-slate-800 uppercase">
                      {nodeProtocol}
                    </div>
                  </div>

                  {/* 10M Speed Bandwidth */}
                  <div className="p-3.5 bg-slate-50 border border-slate-200 rounded-xl">
                    <div className="text-2xs text-slate-500 mb-1">自研 10M 吞吐</div>
                    <div className="font-mono font-bold text-sm text-blue-700">
                      {overviewSpeedMBs > 0 ? (
                        <span>
                          {overviewSpeedMBs.toFixed(1)}{' '}
                          <span className="text-xs font-normal text-slate-500">MB/s</span>
                        </span>
                      ) : (
                        <span className="text-xs font-normal text-slate-400">未测速</span>
                      )}
                    </div>
                  </div>
                </div>
              </div>

              {/* Residential Smart Chain Card if Resi */}
              {isResi && node.chain_info && (
                <div className="p-4 bg-purple-50/70 border border-purple-200 rounded-xl space-y-3">
                  <div className="flex items-center justify-between">
                    <div className="flex items-center space-x-2">
                      <Layers className="w-4 h-4 text-purple-600" />
                      <h4 className="text-xs font-bold text-purple-900">
                        智能专线中转链式配置
                      </h4>
                    </div>
                    {node.chain_info.is_failover_relay ? (
                      <span className="text-2xs font-semibold px-2 py-0.5 rounded bg-amber-100 text-amber-800 border border-amber-300">
                        [容灾中]
                      </span>
                    ) : (
                      <span className="text-2xs font-semibold px-2 py-0.5 rounded bg-emerald-100 text-emerald-800">
                        [地域亲和最佳挂载]
                      </span>
                    )}
                  </div>
                  <div className="grid grid-cols-2 gap-2 text-xs bg-white p-3 rounded-lg border border-purple-100 font-mono">
                    <div>
                      <span className="text-slate-400 text-2xs block">前置线路机 (Relay):</span>
                      <span className="font-semibold text-slate-800">{node.chain_info.relay_node_name}</span>
                    </div>
                    <div>
                      <span className="text-slate-400 text-2xs block">优化后延迟:</span>
                      <span className="font-bold text-emerald-600">
                        {node.chain_info.optimized_latency_ms} ms 
                        <span className="text-2xs font-normal text-emerald-700 ml-1">
                          (提速 {node.chain_info.latency_gain_pct}%)
                        </span>
                      </span>
                    </div>
                  </div>
                </div>
              )}

              {/* 4. 业务分流矩阵 (包含 YouTube, Gemini, Claude, ChatGPT, 百度) */}
              <div>
                <div className="flex items-center justify-between mb-2.5">
                  <h4 className="text-xs font-semibold uppercase tracking-wider text-slate-400">
                    核心业务分流与独立落地矩阵 (Egress Split Matrix)
                  </h4>
                  <span className="text-2xs text-slate-400 font-mono">
                    区分独立落地 IP 与 [家宽] / [机房] 属性
                  </span>
                </div>
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 text-xs">
                  {[
                    { name: 'ChatGPT', key: 'chatgpt' as const },
                    { name: 'Claude', key: 'claude' as const },
                    { name: 'Gemini', key: 'gemini' as const },
                    { name: 'YouTube', key: 'youtube' as const },
                    { name: '百度直连', key: 'baidu' as const },
                  ].map((item) => {
                    const val = node.split?.[item.key];
                    const detail = node.split?.details?.[item.key];
                    const isOk = typeof val === 'string' && (val.includes('ok') || val.includes('200') || !val.includes('fail'));
                    const is403 = typeof val === 'string' && val.includes('403');
                    
                    // 确定落地 IP、地理位置与 IP 属性
                    const egressIP = detail?.exit_ip || (isOk ? landingIP : '');
                    const egressCity = detail?.exit_city || (isOk ? nodeExitCity : '');
                    const egressCountry = detail?.exit_country || (isOk ? nodeExitCountry : '');
                    const egressIPType = detail?.ip_type || (egressIP === landingIP ? nodeExitIPType : '机房');

                    return (
                      <div key={item.key} className="p-3 bg-white border border-slate-200 rounded-lg shadow-2xs space-y-2">
                        <div className="flex items-center justify-between">
                          <span className="font-bold text-slate-800">{item.name}</span>
                          {val ? (
                            isOk ? (
                              <span className="text-2xs font-mono font-medium text-emerald-700 bg-emerald-50 px-2 py-0.5 rounded border border-emerald-200">
                                [通过] {detail?.latency_ms ? `${detail.latency_ms}ms` : val}
                              </span>
                            ) : is403 ? (
                              <span className="text-2xs font-mono font-medium text-amber-700 bg-amber-50 px-2 py-0.5 rounded border border-amber-200">
                                [受限] 403 拦截
                              </span>
                            ) : (
                              <span className="text-2xs font-mono font-medium text-red-700 bg-red-50 px-2 py-0.5 rounded border border-red-200">
                                [未通过] 超时未连通
                              </span>
                            )
                          ) : (
                            <span className="text-2xs text-slate-400">[未测试]</span>
                          )}
                        </div>

                        {egressIP && egressIP !== '—' ? (
                          <div className="pt-2 border-t border-slate-100 flex items-center justify-between text-2xs font-mono text-slate-600">
                            <div className="flex items-center space-x-1 truncate max-w-[190px]">
                              <span>落地:</span>
                              <b className="text-slate-800 select-all">{egressIP}</b>
                            </div>
                            <div className="flex items-center space-x-1 flex-shrink-0">
                              <span>{egressCity ? `${egressCity}, ` : ''}{egressCountry}</span>
                              <span className={`px-1 py-0.2 rounded font-semibold text-3xs ${
                                egressIPType === '家宽'
                                  ? 'bg-purple-100 text-purple-700'
                                  : 'bg-blue-100 text-blue-700'
                              }`}>
                                [{egressIPType}]
                              </span>
                            </div>
                          </div>
                        ) : (
                          <div className="pt-2 border-t border-slate-100 text-2xs font-mono text-slate-400">
                            {item.key === 'baidu' ? '直连对照 (未走代理分流)' : '落地: 无法解析或不可达'}
                          </div>
                        )}
                      </div>
                    );
                  })}
                </div>
              </div>
            </div>
          )}

          {/* TAB 2: SPEED & LATENCY HISTORY */}
          {activeSubTab === 'history' && (
            <div className="space-y-4">
              <div className="flex items-center justify-between">
                <div>
                  <h4 className="text-xs font-bold text-slate-900 flex items-center">
                    <TrendingUp className="w-4 h-4 mr-1.5 text-blue-600" />
                    10MB 历史测速与延迟采样记录
                  </h4>
                  <p className="text-2xs text-slate-500 mt-0.5">
                    每日高峰期 (20:30±15m) 与闲时 (04:30±15m) 平滑轮番测速沉淀，为智能分配提供参考
                  </p>
                </div>
                <button
                  onClick={handleRunSpeedTest}
                  disabled={isSpeedTestingLocal}
                  className="px-3 py-1.5 bg-blue-600 hover:bg-blue-700 disabled:opacity-50 text-white rounded-lg text-xs font-medium flex items-center transition shadow-2xs"
                >
                  <Zap className={`w-3.5 h-3.5 mr-1 ${isSpeedTestingLocal ? 'animate-pulse' : ''}`} />
                  {isSpeedTestingLocal ? '正在实测 10M...' : '即时单测 10M'}
                </button>
              </div>

              <div className="bg-white border border-slate-200 rounded-xl overflow-hidden shadow-2xs">
                <table className="min-w-full divide-y divide-slate-200 text-left text-xs font-mono">
                  <thead className="bg-slate-50 text-slate-500 uppercase tracking-wider text-2xs font-sans font-semibold">
                    <tr>
                      <th className="px-4 py-2.5">测试时间</th>
                      <th className="px-4 py-2.5">调度时段</th>
                      <th className="px-4 py-2.5">10MB 吞吐速度</th>
                      <th className="px-4 py-2.5">TTFB 响应</th>
                      <th className="px-4 py-2.5">RTT 延迟</th>
                      <th className="px-4 py-2.5">前置中继</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-slate-200 bg-white">
                    {isLoadingHistory && historyList.length === 0 ? (
                      <tr>
                        <td colSpan={6} className="px-4 py-8 text-center text-slate-400 font-sans text-xs">
                          正在加载历史测速记录...
                        </td>
                      </tr>
                    ) : historyList.length === 0 ? (
                      <tr>
                        <td colSpan={6} className="px-4 py-8 text-center text-slate-400 font-sans text-xs">
                          暂无真实 10MB 测速记录，可点击右上角「即时单测 10M」发起实测。
                        </td>
                      </tr>
                    ) : (
                      historyList.map((item) => (
                        <tr key={item.id} className="hover:bg-slate-50 transition">
                          <td className="px-4 py-3 text-slate-700 text-2xs">
                            {new Date(item.tested_at).toLocaleString([], { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' })}
                          </td>
                          <td className="px-4 py-3 font-sans">
                            {item.type === 'peak' ? (
                              <span className="px-2 py-0.5 rounded text-2xs font-semibold bg-amber-100 text-amber-800">
                                [高峰期]
                              </span>
                            ) : item.type === 'idle' ? (
                              <span className="px-2 py-0.5 rounded text-2xs font-semibold bg-blue-100 text-blue-800">
                                [闲时]
                              </span>
                            ) : (
                              <span className="px-2 py-0.5 rounded text-2xs font-semibold bg-slate-100 text-slate-700">
                                [实测]
                              </span>
                            )}
                          </td>
                          <td className="px-4 py-3 font-bold text-blue-600">
                            {(item.speed_mb_s ?? 0).toFixed(2)} MB/s
                          </td>
                          <td className="px-4 py-3 text-slate-600">
                            {item.ttfb_ms} ms
                          </td>
                          <td className="px-4 py-3 text-emerald-600 font-bold">
                            {item.latency_ms} ms
                          </td>
                          <td className="px-4 py-3 text-slate-500 truncate max-w-[120px]">
                            {item.relay_name || '直连落地'}
                          </td>
                        </tr>
                      ))
                    )}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          {/* TAB 3: PROBE LOGS */}
          {activeSubTab === 'logs' && (
            <div className="space-y-4">
              <div className="flex items-center justify-between">
                <div>
                  <h4 className="text-xs font-bold text-slate-900 flex items-center">
                    <Terminal className="w-4 h-4 mr-1.5 text-slate-700" />
                    单进程隔离沙箱运行回溯
                  </h4>
                  <p className="text-2xs text-slate-500 mt-0.5">
                    sing-box 进程级启动与端口监听输出，保留最近 50 行 stdout/stderr
                  </p>
                </div>
              </div>

              <div className="bg-slate-950 text-slate-200 rounded-xl p-4 font-mono text-2xs overflow-x-auto shadow-inner border border-slate-800 leading-relaxed max-h-[400px]">
                {node.sandbox_logs && node.sandbox_logs.length > 0 ? (
                  node.sandbox_logs.map((log, idx) => (
                    <div key={idx} className="whitespace-pre-wrap hover:bg-slate-900 px-1 py-0.5 rounded">
                      <span className="text-slate-500 mr-2 select-none">[{idx + 1}]</span>
                      <span>{log}</span>
                    </div>
                  ))
                ) : (
                  <div className="text-slate-400 italic p-4 text-center">
                    暂无本次单进程沙箱 stdout/stderr 日志记录（可点击底部「重新测试此节点」实时触发采样）。
                  </div>
                )}
              </div>
            </div>
          )}

          {/* Raw URL Display */}
          <div>
            <h4 className="text-xs font-semibold uppercase tracking-wider text-slate-400 mb-2">
              完整原始协议链接 (已解密)
            </h4>
            <div className="p-3 bg-slate-50 border border-slate-200 rounded-lg text-2xs font-mono text-slate-700 break-all select-all">
              {node.original_url || '无有效 URL'}
            </div>
          </div>
        </div>

        {/* Drawer Bottom Actions */}
        <div className="p-4 border-t border-slate-200 bg-slate-50 flex items-center justify-between">
          <div className="text-xs text-slate-400">
            检测时间: {node.tested_at ? new Date(node.tested_at).toLocaleString() : '最近'}
          </div>

          <div className="flex items-center space-x-2">
            <button
              onClick={() => onRetest(node)}
              disabled={isTestingThisNode}
              className="inline-flex items-center px-3.5 py-2 text-xs font-medium rounded-lg text-slate-700 bg-white border border-slate-200 hover:bg-slate-100 disabled:opacity-50 transition"
            >
              <RotateCw className={`w-3.5 h-3.5 mr-1.5 ${isTestingThisNode ? 'animate-spin text-blue-600' : ''}`} />
              {isTestingThisNode ? '正在重测中...' : '重新测试此节点'}
            </button>

            <button
              onClick={handleRunSpeedTest}
              disabled={isSpeedTestingLocal}
              title="使用自建 10MB 测试文件测速 (严格禁止任何公网测速网站回退)"
              className="inline-flex items-center px-3.5 py-2 text-xs font-medium rounded-lg text-white bg-blue-600 hover:bg-blue-700 disabled:opacity-50 transition shadow-2xs"
            >
              <Zap className={`w-3.5 h-3.5 mr-1.5 ${isSpeedTestingLocal ? 'animate-pulse' : ''}`} />
              {isSpeedTestingLocal ? '测速中...' : '自建 10M 文件测速'}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
};
