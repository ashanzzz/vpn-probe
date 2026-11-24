import React, { useState } from 'react';
import { 
  Terminal, 
  Play, 
  CheckCircle2, 
  XCircle, 
  RotateCw, 
  Copy, 
  Check, 
  Loader2,
  Globe,
  Sliders,
  Radio,
  Layers
} from 'lucide-react';
import { api } from '../api/client';
import { NodeProbeResult } from '../types';
import { ClientLiveSplitVerifier } from './ClientLiveSplitVerifier';

export const QuickProbe: React.FC = () => {
  const [probeMode, setProbeMode] = useState<'sandbox' | 'client_split'>('sandbox');
  const [linksText, setLinksText] = useState('');
  const [isSelf, setIsSelf] = useState(true);
  const [syncSubStore, setSyncSubStore] = useState(false);
  const [checkSplit, setCheckSplit] = useState(true);
  const [timeoutSec, setTimeoutSec] = useState(8);
  const [loading, setLoading] = useState(false);
  const [results, setResults] = useState<{
    total: number;
    online: number;
    offline: number;
    china_exit: number;
    clean_exit: number;
    substore_synced: boolean;
    results: NodeProbeResult[];
  } | null>(null);
  const [error, setError] = useState<string | null>(null);

  const handleRunProbe = async () => {
    const links = linksText
      .split('\n')
      .map((l) => l.trim())
      .filter(Boolean);

    if (links.length === 0) {
      setError('请至少输入一条有效的代理链接 (vless://, trojan://, ss://, 等)');
      return;
    }

    setLoading(true);
    setError(null);
    try {
      const resp = await api.probeBatch(links, isSelf, syncSubStore, checkSplit, timeoutSec);
      setResults(resp);
    } catch (err: any) {
      setError(`沙箱测试失败: ${err.message || '执行超时或服务错误'}`);
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="space-y-6">
      {/* View Switcher Tabs */}
      <div className="flex items-center space-x-2 border-b border-slate-200 pb-3">
        <button
          onClick={() => setProbeMode('sandbox')}
          className={`flex items-center space-x-2 px-4 py-2 rounded-xl text-xs font-bold transition ${
            probeMode === 'sandbox'
              ? 'bg-blue-600 text-white shadow-xs'
              : 'bg-white text-slate-600 hover:bg-slate-100 border border-slate-200'
          }`}
        >
          <Terminal className="w-4 h-4" />
          <span>节点沙箱批量探测 (Sandbox Batch Probe)</span>
        </button>

        <button
          onClick={() => setProbeMode('client_split')}
          className={`flex items-center space-x-2 px-4 py-2 rounded-xl text-xs font-bold transition ${
            probeMode === 'client_split'
              ? 'bg-blue-600 text-white shadow-xs'
              : 'bg-white text-slate-600 hover:bg-slate-100 border border-slate-200'
          }`}
        >
          <Radio className="w-4 h-4 text-indigo-400" />
          <span>本机客户端实时分流验证 (Net.Coffee 分流体检)</span>
          <span className="px-1.5 py-0.5 rounded text-3xs font-mono font-semibold bg-emerald-100 text-emerald-800">
            Live Lab
          </span>
        </button>
      </div>

      {probeMode === 'client_split' ? (
        <ClientLiveSplitVerifier />
      ) : (
        <>
          {/* 1. Sandbox Input Card */}
          <div className="bg-white border border-slate-200 rounded-xl p-5 shadow-2xs space-y-4">
            <div>
              <h2 className="text-base font-bold text-slate-900 flex items-center">
                <Terminal className="w-5 h-5 mr-2 text-blue-600" />
                即时沙箱测试台 (Sandbox Batch Probe)
              </h2>
              <p className="text-xs text-slate-500 mt-0.5">
                单进程 sing-box 隔离启动 · 按需分配动态端口 · 测完即销毁
              </p>
            </div>

            <div>
              <label className="block text-xs font-semibold text-slate-700 mb-1.5">
                输入待测代理链接 (支持批量，每行一条)
              </label>
              <textarea
                rows={5}
                value={linksText}
                onChange={(e) => setLinksText(e.target.value)}
                placeholder="vless://uuid@host:port?security=reality&sni=...#Node-01&#10;trojan://password@host:port?sni=...#Node-02"
                className="w-full p-3 font-mono text-xs border border-slate-300 rounded-xl focus:outline-none focus:ring-2 focus:ring-blue-600 focus:border-blue-600 bg-slate-50/50 leading-relaxed"
              />
            </div>

            {/* Options Row */}
            <div className="flex flex-wrap items-center justify-between gap-4 pt-2 border-t border-slate-100">
              <div className="flex flex-wrap items-center gap-4 text-xs">
                <label className="flex items-center space-x-2 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={isSelf}
                    onChange={(e) => setIsSelf(e.target.checked)}
                    className="rounded border-slate-300 text-blue-600 focus:ring-blue-500 w-4 h-4"
                  />
                  <span className="text-slate-700">自有服务 (is_self)</span>
                </label>

                <label className="flex items-center space-x-2 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={syncSubStore}
                    onChange={(e) => setSyncSubStore(e.target.checked)}
                    className="rounded border-slate-300 text-blue-600 focus:ring-blue-500 w-4 h-4"
                  />
                  <span className="text-slate-700">自动同步四分组到 SubStore</span>
                </label>

                <label className="flex items-center space-x-2 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={checkSplit}
                    onChange={(e) => setCheckSplit(e.target.checked)}
                    className="rounded border-slate-300 text-blue-600 focus:ring-blue-500 w-4 h-4"
                  />
                  <span className="text-slate-700">探测拓展分流 (ChatGPT/Claude/Gemini/YT/百度)</span>
                </label>

                <div className="flex items-center space-x-1.5">
                  <span className="text-slate-500">超时:</span>
                  <select
                    value={timeoutSec}
                    onChange={(e) => setTimeoutSec(Number(e.target.value))}
                    className="border border-slate-300 rounded px-2 py-0.5 text-xs bg-white"
                  >
                    <option value={5}>5 秒</option>
                    <option value={8}>8 秒</option>
                    <option value={15}>15 秒</option>
                    <option value={30}>30 秒</option>
                  </select>
                </div>
              </div>

              <button
                onClick={handleRunProbe}
                disabled={loading}
                className="inline-flex items-center px-5 py-2 text-xs font-semibold rounded-lg text-white bg-blue-600 hover:bg-blue-700 disabled:opacity-50 transition shadow-2xs"
              >
                {loading ? (
                  <>
                    <Loader2 className="w-3.5 h-3.5 mr-1.5 animate-spin" />
                    沙箱测试运行中...
                  </>
                ) : (
                  <>
                    <Play className="w-3.5 h-3.5 mr-1.5 fill-current" />
                    开始沙箱批量测试
                  </>
                )}
              </button>
            </div>

            {error && (
              <div className="p-3 bg-red-50 border border-red-200 rounded-lg text-xs text-red-700">
                {error}
              </div>
            )}
          </div>

          {/* 2. Results Output Card */}
          {results && (
            <div className="bg-white border border-slate-200 rounded-xl shadow-2xs overflow-hidden space-y-4">
              <div className="p-5 border-b border-slate-200 bg-slate-50 flex flex-wrap items-center justify-between gap-3">
                <div className="flex items-center space-x-4 text-xs font-mono">
                  <span className="text-slate-700 font-bold">总计: {results.total}</span>
                  <span className="text-emerald-700 font-bold">[在线] {results.online}</span>
                  <span className="text-red-700 font-bold">[离线] {results.offline}</span>
                  <span className="text-amber-700 font-bold">[送中] {results.china_exit}</span>
                  <span className="text-blue-700 font-bold">[不送中] {results.clean_exit}</span>
                </div>

                {results.substore_synced && (
                  <span className="inline-flex items-center px-2.5 py-1 rounded-full text-xs font-semibold bg-emerald-100 text-emerald-800">
                    <CheckCircle2 className="w-3.5 h-3.5 mr-1" />
                    已同步至 SubStore 四分组
                  </span>
                )}
              </div>

              <div className="overflow-x-auto">
                <table className="min-w-full divide-y divide-slate-200 text-left text-xs">
                  <thead className="bg-slate-50 text-slate-500 uppercase tracking-wider font-semibold">
                    <tr>
                      <th scope="col" className="px-4 py-2.5">节点名称</th>
                      <th scope="col" className="px-4 py-2.5">协议</th>
                      <th scope="col" className="px-4 py-2.5">连通与 RTT 延迟</th>
                      <th scope="col" className="px-4 py-2.5">出口与送中</th>
                      <th scope="col" className="px-4 py-2.5">归属四分组</th>
                      <th scope="col" className="px-4 py-2.5">业务分流 (GPT/Claude/Gemini/YT/百度)</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-slate-200 bg-white">
                    {results.results?.map((res, idx) => (
                      <tr key={idx} className="hover:bg-slate-50 transition">
                        <td className="px-4 py-3 font-semibold text-slate-900">
                          {res.name}
                        </td>
                        <td className="px-4 py-3 font-mono text-2xs uppercase text-slate-600">
                          {res.protocol}
                        </td>
                        <td className="px-4 py-3 whitespace-nowrap font-mono">
                          {res.online ? (
                            <span className="font-bold text-emerald-600">
                              {res.latency_ms} ms
                            </span>
                          ) : (
                            <span className="font-semibold text-red-600">超时</span>
                          )}
                        </td>
                        <td className="px-4 py-3 whitespace-nowrap">
                          {res.online ? (
                            res.china_exit ? (
                              <span className="text-amber-800 font-medium">
                                [送中] ({res.exit_city ? `${res.exit_city}, ` : ''}{res.exit_country || 'CN'})
                              </span>
                            ) : (
                              <span className="text-emerald-800 font-medium">
                                [不送中] ({res.exit_city ? `${res.exit_city}, ` : ''}{res.exit_country || 'Global'})
                              </span>
                            )
                          ) : (
                            <span className="text-slate-400">-</span>
                          )}
                        </td>
                        <td className="px-4 py-3 font-medium text-slate-700">
                          {res.category || '-'}
                        </td>
                        <td className="px-4 py-3 whitespace-nowrap">
                          <div className="flex items-center space-x-1 text-2xs">
                            {(['chatgpt', 'claude', 'gemini', 'youtube', 'baidu'] as const).map((srv) => {
                              const val = res.split?.[srv];
                              const detail = res.split?.details?.[srv];
                              const isOk = typeof val === 'string' && (val.includes('ok') || val.includes('200') || !val.includes('fail'));
                              const label = srv === 'chatgpt' ? 'GPT' : srv === 'claude' ? 'Claude' : srv === 'gemini' ? 'Gemini' : srv === 'youtube' ? 'YT' : '百度';
                              const tooltip = detail?.exit_ip
                                ? `${label}: ${detail.exit_ip} (${detail.exit_city || detail.exit_country || ''}) [${detail.ip_type || '机房'}]`
                                : val || '未测试';
                              return (
                                <span
                                  key={srv}
                                  title={tooltip}
                                  className={`px-1.5 py-0.5 rounded font-mono ${
                                    val
                                      ? isOk
                                        ? 'bg-emerald-50 text-emerald-700 border border-emerald-200'
                                        : 'bg-red-50 text-red-700 border border-red-200'
                                      : 'bg-slate-100 text-slate-400'
                                  }`}
                                >
                                  {label}
                                </span>
                              );
                            })}
                          </div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          )}
        </>
      )}
    </div>
  );
};
