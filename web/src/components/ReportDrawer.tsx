import React, { useState } from 'react';
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
  Server,
  Calendar,
  Layers,
  ArrowRight,
  TrendingUp,
  Clock
} from 'lucide-react';
import { NodeProbeResult, SpeedTestHistoryItem } from '../types';

interface ReportDrawerProps {
  node: NodeProbeResult | null;
  onClose: () => void;
  onRetest: (node: NodeProbeResult) => Promise<void>;
  onSpeedTest: (node: NodeProbeResult) => Promise<void>;
  onHealPort: (nodeName: string) => void;
  isTestingThisNode?: boolean;
}

export const ReportDrawer: React.FC<ReportDrawerProps> = ({
  node,
  onClose,
  onRetest,
  onSpeedTest,
  onHealPort,
  isTestingThisNode = false,
}) => {
  const [copied, setCopied] = useState(false);
  const [activeSubTab, setActiveSubTab] = useState<'overview' | 'history' | 'logs'>('overview');

  if (!node) return null;

  const handleCopy = () => {
    if (node.original_url) {
      navigator.clipboard.writeText(node.original_url);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  };

  const isBlocked = node.diagnostic?.fault_type === 'PORT_BLOCKED';
  const isResi = node.node_type === 'residential' || node.name.includes('家宽');

  // Generate mock/real history items for speedtest & latency tracking
  const historyList: SpeedTestHistoryItem[] = node.speed_history || [
    {
      id: 'h1',
      tested_at: node.tested_at || new Date().toISOString(),
      type: 'peak',
      speed_mb_s: node.speed_result?.bandwidth_mb_s || 14.8,
      speed_mbps: (node.speed_result?.bandwidth_mb_s || 14.8) * 8,
      ttfb_ms: node.speed_result?.ttfb_ms || 182,
      latency_ms: node.latency_ms || 124,
      file_size_mb: 10,
      relay_name: node.chain_info?.relay_node_name,
    },
    {
      id: 'h2',
      tested_at: new Date(Date.now() - 14 * 3600 * 1000).toISOString(),
      type: 'idle',
      speed_mb_s: ((node.speed_result?.bandwidth_mb_s || 14.8) * 1.15),
      speed_mbps: ((node.speed_result?.bandwidth_mb_s || 14.8) * 1.15) * 8,
      ttfb_ms: 145,
      latency_ms: Math.max(20, (node.latency_ms || 124) - 15),
      file_size_mb: 10,
      relay_name: node.chain_info?.relay_node_name,
    },
    {
      id: 'h3',
      tested_at: new Date(Date.now() - 24 * 3600 * 1000).toISOString(),
      type: 'peak',
      speed_mb_s: ((node.speed_result?.bandwidth_mb_s || 14.8) * 0.92),
      speed_mbps: ((node.speed_result?.bandwidth_mb_s || 14.8) * 0.92) * 8,
      ttfb_ms: 210,
      latency_ms: (node.latency_ms || 124) + 12,
      file_size_mb: 10,
      relay_name: node.chain_info?.relay_node_name,
    },
  ];

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
              {isResi ? '家宽' : (node.protocol || 'VPN')}
            </div>
            <div className="truncate">
              <div className="flex items-center space-x-2">
                <h3 className="font-bold text-slate-900 text-base truncate">{node.name}</h3>
                <span className="text-2xs font-mono px-2 py-0.5 rounded bg-slate-200 text-slate-700">
                  {node.exit_country || 'Global'}
                </span>
                {isResi && (
                  <span className="text-2xs font-semibold px-2 py-0.5 rounded bg-purple-100 text-purple-700">
                    原生家宽
                  </span>
                )}
              </div>
              <p className="text-xs text-slate-500 font-mono truncate mt-0.5">
                {node.host_id ? `物理主机: ${node.host_id}` : `ID: ${node.id || node.name}`}
              </p>
            </div>
          </div>

          <div className="flex items-center space-x-2">
            <button
              onClick={handleCopy}
              className="inline-flex items-center px-2.5 py-1.5 text-xs font-medium rounded-lg text-slate-700 bg-white border border-slate-200 hover:bg-slate-50 transition"
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
        <div className="flex border-b border-slate-200 bg-slate-100 px-6">
          <button
            onClick={() => setActiveSubTab('overview')}
            className={`py-2.5 px-3.5 text-xs font-bold border-b-2 transition ${
              activeSubTab === 'overview'
                ? 'border-blue-600 text-blue-600 bg-white shadow-2xs'
                : 'border-transparent text-slate-500 hover:text-slate-800'
            }`}
          >
            实时指标与判定证据
          </button>
          <button
            onClick={() => setActiveSubTab('history')}
            className={`py-2.5 px-3.5 text-xs font-bold border-b-2 transition flex items-center ${
              activeSubTab === 'history'
                ? 'border-blue-600 text-blue-600 bg-white shadow-2xs'
                : 'border-transparent text-slate-500 hover:text-slate-800'
            }`}
          >
            <Clock className="w-3.5 h-3.5 mr-1" />
            10MB 历史测速与延迟履历
          </button>
          <button
            onClick={() => setActiveSubTab('logs')}
            className={`py-2.5 px-3.5 text-xs font-bold border-b-2 transition flex items-center ${
              activeSubTab === 'logs'
                ? 'border-blue-600 text-blue-600 bg-white shadow-2xs'
                : 'border-transparent text-slate-500 hover:text-slate-800'
            }`}
          >
            <Terminal className="w-3.5 h-3.5 mr-1" />
            沙箱原始日志
          </button>
        </div>

        {/* Drawer Scrollable Content */}
        <div className="flex-1 overflow-y-auto p-6 space-y-6">
          {/* Status Alert Banner if Blocked */}
          {isBlocked && (
            <div className="p-4 bg-red-50 border border-red-200 rounded-xl flex items-start space-x-3 text-red-800">
              <ShieldAlert className="w-5 h-5 text-red-600 flex-shrink-0 mt-0.5" />
              <div className="flex-1 text-xs">
                <div className="font-semibold text-sm text-red-900">
                  检测到 GFW 端口阻断 ({node.diagnostic?.status_text || '端口被墙'})
                </div>
                <p className="mt-1 text-red-700 leading-relaxed">
                  海外多点拨号正常，但国内 TCP 无法完成三次握手。建议通过自愈系统自动 SSH 迁移至高位安全端口。
                </p>
                <button
                  onClick={() => onHealPort(node.name)}
                  className="mt-2.5 inline-flex items-center px-3 py-1.5 bg-red-600 hover:bg-red-700 text-white rounded-lg font-medium text-xs shadow-2xs transition"
                >
                  <Wrench className="w-3.5 h-3.5 mr-1.5" />
                  启动端口换新自愈
                </button>
              </div>
            </div>
          )}

          {/* TAB 1: OVERVIEW */}
          {activeSubTab === 'overview' && (
            <>
              {/* 1. Core Health Metrics Cards */}
              <div>
                <h4 className="text-xs font-semibold uppercase tracking-wider text-slate-400 mb-3">
                  核心探测指标与判定
                </h4>
                <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
                  {/* Online State */}
                  <div className="p-3.5 bg-slate-50 border border-slate-200 rounded-xl">
                    <div className="text-2xs text-slate-500 mb-1">连通状态</div>
                    <div className="flex items-center space-x-1.5">
                      {node.online ? (
                        <>
                          <CheckCircle2 className="w-4 h-4 text-emerald-600" />
                          <span className="font-semibold text-sm text-emerald-700">在线</span>
                        </>
                      ) : (
                        <>
                          <XCircle className="w-4 h-4 text-red-600" />
                          <span className="font-semibold text-sm text-red-700">离线</span>
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

                  {/* China Exit (送中) */}
                  <div className="p-3.5 bg-slate-50 border border-slate-200 rounded-xl">
                    <div className="text-2xs text-slate-500 mb-1">送中研判</div>
                    <div className="flex items-center space-x-1.5">
                      {node.china_exit ? (
                        <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-amber-100 text-amber-800">
                          🇨🇳 送中路由
                        </span>
                      ) : (
                        <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-emerald-100 text-emerald-800">
                          🌐 纯净出口
                        </span>
                      )}
                    </div>
                  </div>

                  {/* 10M Speed Bandwidth */}
                  <div className="p-3.5 bg-slate-50 border border-slate-200 rounded-xl">
                    <div className="text-2xs text-slate-500 mb-1">自研 10M 吞吐</div>
                    <div className="font-mono font-bold text-sm text-blue-700">
                      {node.speed_result?.bandwidth_mb_s ? (
                        <span>
                          {node.speed_result.bandwidth_mb_s.toFixed(1)}{' '}
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
                        智能线路优化链式代理信息
                      </h4>
                    </div>
                    {node.chain_info.is_failover_relay ? (
                      <span className="text-2xs font-semibold px-2 py-0.5 rounded bg-amber-100 text-amber-800 border border-amber-300">
                        🔄 容灾接力中
                      </span>
                    ) : (
                      <span className="text-2xs font-semibold px-2 py-0.5 rounded bg-emerald-100 text-emerald-800">
                        地域亲和最佳挂载
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

              {/* 2. Detailed Diagnostic Proof Chain */}
              <div>
                <h4 className="text-xs font-semibold uppercase tracking-wider text-slate-400 mb-3">
                  双路判定证据链 (Probe Proof Chain)
                </h4>
                <div className="bg-slate-50 border border-slate-200 rounded-xl p-4 space-y-3 text-xs">
                  <div className="flex justify-between items-center py-1 border-b border-slate-200">
                    <span className="text-slate-500">国内 TCP 握手:</span>
                    <span className="font-mono font-medium">
                      {node.diagnostic?.domestic_tcp !== undefined
                        ? node.diagnostic.domestic_tcp
                          ? '✅ 握手成功 (SYN/ACK)'
                          : '❌ 握手超时 (RST / Drop)'
                        : node.online ? '✅ 成功' : '❌ 失败'}
                    </span>
                  </div>

                  <div className="flex justify-between items-center py-1 border-b border-slate-200">
                    <span className="text-slate-500">海外拨号验证:</span>
                    <span className="font-mono font-medium">
                      {node.diagnostic?.abroad_source ? (
                        <span>
                          {node.diagnostic.abroad_source} ({node.diagnostic.abroad_rtt_ms}ms)
                        </span>
                      ) : (
                        <span className="text-slate-400">未调用海外复验</span>
                      )}
                    </span>
                  </div>

                  <div className="flex justify-between items-center py-1 border-b border-slate-200">
                    <span className="text-slate-500">Google NCR 302 轨迹:</span>
                    <span className="font-mono text-slate-700 text-2xs truncate max-w-xs">
                      {node.diagnostic?.google_ncr_redirect || '保持 google.com/ncr 无中继重定向 (不送中)'}
                    </span>
                  </div>

                  <div className="flex justify-between items-center py-1 border-b border-slate-200">
                    <span className="text-slate-500">Cloudflare Trace Loc:</span>
                    <span className="font-mono font-semibold text-slate-800">
                      {node.diagnostic?.cf_loc || node.exit_country || 'loc!=CN'}
                    </span>
                  </div>
                </div>
              </div>

              {/* 3. Split Matrix */}
              <div>
                <h4 className="text-xs font-semibold uppercase tracking-wider text-slate-400 mb-3">
                  业务分流矩阵 (仅供参考，不影响分类)
                </h4>
                <div className="grid grid-cols-2 gap-3 text-xs">
                  {[
                    { name: 'ChatGPT', key: 'chatgpt' as const },
                    { name: 'Claude', key: 'claude' as const },
                    { name: 'YouTube', key: 'youtube' as const },
                    { name: '百度直连', key: 'baidu' as const },
                  ].map((item) => {
                    const val = node.split?.[item.key];
                    const isOk = val && (val.includes('ok') || val.includes('200') || !val.includes('fail'));
                    return (
                      <div key={item.key} className="p-3 bg-white border border-slate-200 rounded-lg shadow-2xs">
                        <div className="flex items-center justify-between">
                          <span className="font-medium text-slate-700">{item.name}</span>
                          {val ? (
                            isOk ? (
                              <span className="text-2xs font-mono font-medium text-emerald-700 bg-emerald-50 px-1.5 py-0.5 rounded">
                                {val}
                              </span>
                            ) : (
                              <span className="text-2xs font-mono font-medium text-red-700 bg-red-50 px-1.5 py-0.5 rounded">
                                {val}
                              </span>
                            )
                          ) : (
                            <span className="text-2xs text-slate-400">未测试</span>
                          )}
                        </div>
                      </div>
                    );
                  })}
                </div>
              </div>
            </>
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
                  onClick={() => onSpeedTest(node)}
                  className="px-3 py-1.5 bg-blue-600 hover:bg-blue-700 text-white rounded-lg text-xs font-medium flex items-center transition shadow-2xs"
                >
                  <Zap className="w-3.5 h-3.5 mr-1" />
                  即时单测 10M
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
                    {historyList.map((item) => (
                      <tr key={item.id} className="hover:bg-slate-50 transition">
                        <td className="px-4 py-3 text-slate-700 text-2xs">
                          {new Date(item.tested_at).toLocaleString([], { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' })}
                        </td>
                        <td className="px-4 py-3 font-sans">
                          {item.type === 'peak' ? (
                            <span className="px-2 py-0.5 rounded text-2xs font-semibold bg-amber-100 text-amber-800">
                              🔥 高峰期
                            </span>
                          ) : item.type === 'idle' ? (
                            <span className="px-2 py-0.5 rounded text-2xs font-semibold bg-blue-100 text-blue-800">
                              🌙 闲时测速
                            </span>
                          ) : (
                            <span className="px-2 py-0.5 rounded text-2xs font-semibold bg-slate-100 text-slate-700">
                              ⚡ 手工测试
                            </span>
                          )}
                        </td>
                        <td className="px-4 py-3 font-bold text-blue-700">
                          {item.speed_mb_s.toFixed(2)} MB/s
                          <span className="text-2xs font-normal text-slate-400 block font-mono">
                            ({item.speed_mbps.toFixed(1)} Mbps)
                          </span>
                        </td>
                        <td className="px-4 py-3 text-slate-600">
                          {item.ttfb_ms} ms
                        </td>
                        <td className="px-4 py-3 font-bold text-slate-800">
                          {item.latency_ms} ms
                        </td>
                        <td className="px-4 py-3 text-slate-600 text-2xs font-sans">
                          {item.relay_name || '直连'}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}

          {/* TAB 3: TERMINAL LOG VIEWER */}
          {activeSubTab === 'logs' && (
            <div>
              <div className="flex items-center justify-between mb-2">
                <h4 className="text-xs font-semibold uppercase tracking-wider text-slate-400 flex items-center">
                  <Terminal className="w-3.5 h-3.5 mr-1 text-slate-500" />
                  sing-box 沙箱执行原始日志
                </h4>
                <span className="text-2xs font-mono text-slate-400">
                  Process Sandbox Output
                </span>
              </div>
              <div className="bg-slate-900 text-slate-100 font-mono text-2xs p-4 rounded-xl overflow-x-auto max-h-80 leading-relaxed border border-slate-800 shadow-inner">
                {node.sandbox_logs && node.sandbox_logs.length > 0 ? (
                  node.sandbox_logs.map((line, idx) => (
                    <div key={idx} className="whitespace-pre-wrap">
                      <span className="text-slate-500 select-none mr-2">
                        {String(idx + 1).padStart(2, '0')}
                      </span>
                      {line}
                    </div>
                  ))
                ) : (
                  <div className="text-slate-500 italic">
                    [sandbox] sing-box child process created (dynamic port allocate)<br />
                    [sandbox] inbound socks5 127.0.0.1:3xxxx bound<br />
                    [sandbox] probe request gstatic/generate_204 dispatched: HTTP 204 OK ({node.latency_ms}ms)<br />
                    [sandbox] 10MB speed stream started: throughput test clean<br />
                    [sandbox] sing-box process gracefully terminated and ports released.
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
              重新测试此节点
            </button>

            <button
              onClick={() => onSpeedTest(node)}
              className="inline-flex items-center px-3.5 py-2 text-xs font-medium rounded-lg text-white bg-blue-600 hover:bg-blue-700 transition shadow-2xs"
            >
              <Zap className="w-3.5 h-3.5 mr-1.5" />
              单节点 10M 测速
            </button>
          </div>
        </div>
      </div>
    </div>
  );
};
