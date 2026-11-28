import React, { useState } from 'react';
import {
  Terminal,
  Play,
  CheckCircle2,
  Loader2,
  X,
} from 'lucide-react';
import { api } from '../api/client';
import { NodeProbeResult } from '../types';
import { useFeedback } from '../context/FeedbackContext';

interface SandboxProbeDrawerProps {
  open: boolean;
  onClose: () => void;
}

export const SandboxProbeDrawer: React.FC<SandboxProbeDrawerProps> = ({ open, onClose }) => {
  const { pushActivity } = useFeedback();
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

  if (!open) return null;

  const handleRunProbe = async () => {
    const links = linksText
      .split('\n')
      .map((l) => l.trim())
      .filter(Boolean);

    if (links.length === 0) {
      pushActivity({
        level: 'warn',
        source: '沙箱探针',
        title: '请输入至少一条代理链接后再测试',
      });
      return;
    }

    setLoading(true);
    try {
      const resp = await api.probeBatch(links, isSelf, syncSubStore, checkSplit, timeoutSec);
      setResults(resp);
      pushActivity({
        level: 'success',
        source: '沙箱探针',
        title: `批量沙箱测试完成（${resp.online}/${resp.total} 在线）`,
        detail: `不送中 ${resp.clean_exit} · 送中 ${resp.china_exit}`,
      });
    } catch (err: any) {
      pushActivity({
        level: 'error',
        source: '沙箱探针',
        title: '沙箱批量测试失败',
        detail: err.message || '执行超时或服务错误',
      });
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex justify-end">
      <div className="fixed inset-0 bg-slate-900/35 backdrop-blur-2xs" onClick={onClose} />
      <div className="relative bg-white w-full max-w-3xl h-full shadow-2xl border-l border-slate-200 flex flex-col z-10">
        <div className="px-5 py-4 border-b border-slate-200 flex items-center justify-between bg-slate-50">
          <div className="flex items-center space-x-2">
            <Terminal className="w-4 h-4 text-blue-600" />
            <div>
              <h3 className="text-sm font-bold text-slate-900">临时沙箱批量测链接</h3>
              <p className="text-[11px] text-slate-500">单进程 sing-box 隔离启动 · 按需分配端口 · 测完即销毁</p>
            </div>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="p-1.5 text-slate-400 hover:text-slate-700 hover:bg-slate-200 rounded-lg transition"
          >
            <X className="w-4 h-4" />
          </button>
        </div>

        <div className="flex-1 overflow-y-auto p-5 space-y-4">
          <textarea
            rows={5}
            value={linksText}
            onChange={(e) => setLinksText(e.target.value)}
            placeholder="每行输入一条节点链接（vless://, trojan://, ss://, socks5:// ...）"
            className="w-full p-3 font-mono text-xs border border-slate-300 rounded-xl focus:outline-none focus:ring-2 focus:ring-blue-600 bg-slate-50/50"
          />

          <div className="flex flex-wrap items-center justify-between gap-3 pt-1">
            <div className="flex flex-wrap items-center gap-4 text-xs">
              <label className="flex items-center space-x-1.5 cursor-pointer">
                <input
                  type="checkbox"
                  checked={isSelf}
                  onChange={(e) => setIsSelf(e.target.checked)}
                  className="rounded border-slate-300 text-blue-600"
                />
                <span>自有节点 (is_self)</span>
              </label>

              <label className="flex items-center space-x-1.5 cursor-pointer">
                <input
                  type="checkbox"
                  checked={syncSubStore}
                  onChange={(e) => setSyncSubStore(e.target.checked)}
                  className="rounded border-slate-300 text-blue-600"
                />
                <span>同步到 SubStore</span>
              </label>

              <label className="flex items-center space-x-1.5 cursor-pointer">
                <input
                  type="checkbox"
                  checked={checkSplit}
                  onChange={(e) => setCheckSplit(e.target.checked)}
                  className="rounded border-slate-300 text-blue-600"
                />
                <span>检测 GPT/Claude/Gemini/YT</span>
              </label>

              <select
                value={timeoutSec}
                onChange={(e) => setTimeoutSec(Number(e.target.value))}
                className="border border-slate-300 rounded px-2 py-1 text-xs bg-white"
              >
                <option value={5}>5 秒超时</option>
                <option value={8}>8 秒超时</option>
                <option value={15}>15 秒超时</option>
                <option value={30}>30 秒超时</option>
              </select>
            </div>

            <button
              type="button"
              onClick={handleRunProbe}
              disabled={loading}
              className="inline-flex items-center px-4 py-1.5 text-xs font-semibold rounded-lg text-white bg-blue-600 hover:bg-blue-700 disabled:opacity-50 transition shadow-2xs"
            >
              {loading ? (
                <>
                  <Loader2 className="w-3.5 h-3.5 mr-1.5 animate-spin" />
                  探测中...
                </>
              ) : (
                <>
                  <Play className="w-3.5 h-3.5 mr-1.5 fill-current" />
                  开始沙箱探测
                </>
              )}
            </button>
          </div>

          {results && (
            <div className="border border-slate-200 rounded-xl overflow-hidden">
              <div className="px-4 py-2.5 bg-slate-50 border-b border-slate-200 flex flex-wrap items-center justify-between gap-2 text-xs font-mono">
                <div className="flex items-center gap-4">
                  <span className="font-bold text-slate-700">总计 {results.total}</span>
                  <span className="font-bold text-emerald-700">在线 {results.online}</span>
                  <span className="font-bold text-rose-700">离线 {results.offline}</span>
                  <span className="font-bold text-amber-700">送中 {results.china_exit}</span>
                </div>
                {results.substore_synced && (
                  <span className="inline-flex items-center text-emerald-700 font-semibold">
                    <CheckCircle2 className="w-3.5 h-3.5 mr-1" />
                    已同步 SubStore
                  </span>
                )}
              </div>

              <div className="overflow-x-auto">
                <table className="min-w-full divide-y divide-slate-200 text-left text-xs">
                  <thead className="bg-slate-50 text-slate-500 uppercase text-[11px]">
                    <tr>
                      <th className="px-3 py-2">节点</th>
                      <th className="px-3 py-2">延迟</th>
                      <th className="px-3 py-2">出口</th>
                      <th className="px-3 py-2">分组</th>
                      <th className="px-3 py-2">分流</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-slate-100 bg-white">
                    {results.results?.map((res, idx) => (
                      <tr key={idx} className="hover:bg-slate-50">
                        <td className="px-3 py-2 font-semibold text-slate-900">{res.name}</td>
                        <td className="px-3 py-2 font-mono">
                          {res.online ? (
                            <span className="text-emerald-600 font-bold">{res.latency_ms} ms</span>
                          ) : (
                            <span className="text-rose-600">超时</span>
                          )}
                        </td>
                        <td className="px-3 py-2 text-xs">
                          {res.online
                            ? `${res.china_exit ? '送中' : '纯净'} (${res.exit_country || '-'})`
                            : '-'}
                        </td>
                        <td className="px-3 py-2">{res.category || '-'}</td>
                        <td className="px-3 py-2">
                          <div className="flex items-center gap-1 text-[10px] font-mono">
                            {(['chatgpt', 'claude', 'gemini', 'youtube', 'baidu'] as const).map((srv) => {
                              const val = res.split?.[srv];
                              const isOk = typeof val === 'string' && (val.includes('ok') || val.includes('200') || !val.includes('fail'));
                              return (
                                <span
                                  key={srv}
                                  className={`px-1 py-0.5 rounded ${
                                    val
                                      ? isOk
                                        ? 'bg-emerald-50 text-emerald-700 border border-emerald-200'
                                        : 'bg-rose-50 text-rose-700 border border-rose-200'
                                      : 'bg-slate-100 text-slate-400'
                                  }`}
                                >
                                  {srv === 'youtube' ? 'YT' : srv === 'baidu' ? '百度' : srv}
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
        </div>
      </div>
    </div>
  );
};
