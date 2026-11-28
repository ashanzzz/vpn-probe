import React, { useState, useEffect } from 'react';
import {
  Terminal,
  ChevronUp,
  ChevronDown,
  Trash2,
  ArrowRight,
  RefreshCw,
} from 'lucide-react';
import { api } from '../api/client';
import { ActivityLevel, useFeedback } from '../context/FeedbackContext';

export const ActivityConsoleDrawer: React.FC = () => {
  const {
    logs,
    unreadErrors,
    consoleOpen,
    setConsoleOpen,
    clearLogs,
    navigateTo,
  } = useFeedback();

  const [viewMode, setViewMode] = useState<'station' | 'backend'>('station');
  const [levelFilter, setLevelFilter] = useState<'all' | ActivityLevel>('all');
  const [backendLines, setBackendLines] = useState<string[]>([]);
  const [loadingBackend, setLoadingBackend] = useState(false);

  const latest = logs[0];
  const filteredLogs = logs.filter(
    (l) => levelFilter === 'all' || l.level === levelFilter
  );

  const loadBackendLogs = async () => {
    setLoadingBackend(true);
    try {
      const res = await api.getRecentLogs('runtime', 100);
      setBackendLines(res.lines || []);
    } catch {
      setBackendLines(['[获取后端日志失败，请检查网络连接或 Admin 权限]']);
    } finally {
      setLoadingBackend(false);
    }
  };

  useEffect(() => {
    if (consoleOpen && viewMode === 'backend') {
      loadBackendLogs();
    }
  }, [consoleOpen, viewMode]);

  const badgeClass = (lvl: ActivityLevel) => {
    switch (lvl) {
      case 'success':
        return 'bg-emerald-500/20 text-emerald-300 border-emerald-500/40';
      case 'warn':
        return 'bg-amber-500/20 text-amber-300 border-amber-500/40';
      case 'error':
        return 'bg-rose-500/20 text-rose-300 border-rose-500/40';
      default:
        return 'bg-sky-500/20 text-sky-300 border-sky-500/40';
    }
  };

  return (
    <div className="fixed bottom-0 inset-x-0 z-40 bg-slate-950 text-slate-200 border-t border-slate-800 shadow-2xl">
      {/* Compact Slim Bar (Always Accessible) */}
      <div
        onClick={() => setConsoleOpen((o) => !o)}
        className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 h-9 flex items-center justify-between cursor-pointer select-none hover:bg-slate-900/90 transition"
      >
        <div className="flex items-center gap-2.5 min-w-0 flex-1">
          <div className="flex items-center gap-1.5 text-xs font-mono font-semibold text-slate-100 shrink-0">
            <Terminal className="w-3.5 h-3.5 text-emerald-400" />
            <span>台位操作日志</span>
            <span className="text-[10px] px-1.5 py-0.2 rounded bg-slate-800 text-slate-400">
              {logs.length}
            </span>
          </div>

          {unreadErrors > 0 && (
            <span className="px-1.5 py-0.2 rounded bg-rose-600 text-white text-[10px] font-mono font-bold shrink-0 animate-pulse">
              {unreadErrors} 异常
            </span>
          )}

          {latest ? (
            <div className="flex items-center gap-2 min-w-0 text-[11px] font-mono truncate">
              <span className="text-slate-500 shrink-0">[{latest.timestamp}]</span>
              <span
                className={`px-1.5 py-0.2 rounded border text-[10px] uppercase shrink-0 ${badgeClass(
                  latest.level
                )}`}
              >
                {latest.source}
              </span>
              <span
                className={`truncate ${
                  latest.level === 'error'
                    ? 'text-rose-300'
                    : latest.level === 'warn'
                    ? 'text-amber-300'
                    : 'text-slate-300'
                }`}
              >
                {latest.title}
                {latest.detail ? ` — ${latest.detail}` : ''}
              </span>
            </div>
          ) : (
            <span className="text-[11px] font-mono text-slate-500 truncate">
              等待操作事件 · 测速/巡检/自愈/发布状态将实时流式记录于此（无阻断弹窗）
            </span>
          )}
        </div>

        <div
          className="flex items-center gap-2 shrink-0 ml-3"
          onClick={(e) => e.stopPropagation()}
        >
          {latest?.action && (
            <button
              type="button"
              onClick={() => navigateTo(latest.action!.tab, latest.action!.focusSection)}
              className="inline-flex items-center px-2 py-0.5 rounded bg-blue-600/90 hover:bg-blue-500 text-white text-[10px] font-semibold transition"
            >
              <span>{latest.action.label}</span>
              <ArrowRight className="w-3 h-3 ml-1" />
            </button>
          )}
          <button
            type="button"
            onClick={() => setConsoleOpen((o) => !o)}
            className="inline-flex items-center px-2 py-0.5 rounded bg-slate-800 hover:bg-slate-700 text-slate-300 text-[11px] font-mono"
          >
            {consoleOpen ? (
              <>
                收起 <ChevronDown className="w-3.5 h-3.5 ml-1" />
              </>
            ) : (
              <>
                展开 <ChevronUp className="w-3.5 h-3.5 ml-1" />
              </>
            )}
          </button>
        </div>
      </div>

      {/* Expanded Dark Micro-Terminal Body */}
      {consoleOpen && (
        <div className="border-t border-slate-800 bg-slate-950/98">
          <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-2 flex flex-wrap items-center justify-between gap-2 border-b border-slate-800/80">
            <div className="flex items-center gap-1.5 text-[11px]">
              <button
                type="button"
                onClick={() => setViewMode('station')}
                className={`px-2.5 py-1 rounded font-semibold transition ${
                  viewMode === 'station'
                    ? 'bg-slate-800 text-white border border-slate-700'
                    : 'text-slate-400 hover:text-slate-200'
                }`}
              >
                前端台位事件流 ({logs.length})
              </button>
              <button
                type="button"
                onClick={() => setViewMode('backend')}
                className={`px-2.5 py-1 rounded font-semibold transition ${
                  viewMode === 'backend'
                    ? 'bg-slate-800 text-white border border-slate-700'
                    : 'text-slate-400 hover:text-slate-200'
                }`}
              >
                服务端运行日志 (vpn-probe.log)
              </button>
            </div>

            <div className="flex items-center gap-1.5 text-[11px]">
              {viewMode === 'station' ? (
                <>
                  {(['all', 'error', 'warn', 'success', 'info'] as const).map((lvl) => (
                    <button
                      key={lvl}
                      type="button"
                      onClick={() => setLevelFilter(lvl)}
                      className={`px-2 py-0.5 rounded font-mono uppercase ${
                        levelFilter === lvl
                          ? 'bg-blue-600 text-white'
                          : 'bg-slate-900 text-slate-400 hover:text-slate-200 border border-slate-800'
                      }`}
                    >
                      {lvl}
                    </button>
                  ))}
                  <button
                    type="button"
                    onClick={clearLogs}
                    className="inline-flex items-center px-2 py-0.5 rounded bg-slate-900 hover:bg-slate-800 text-slate-400 hover:text-rose-300 border border-slate-800 ml-1"
                  >
                    <Trash2 className="w-3 h-3 mr-1" />
                    清空
                  </button>
                </>
              ) : (
                <button
                  type="button"
                  onClick={loadBackendLogs}
                  disabled={loadingBackend}
                  className="inline-flex items-center px-2.5 py-0.5 rounded bg-slate-900 hover:bg-slate-800 text-slate-300 border border-slate-800"
                >
                  <RefreshCw className={`w-3 h-3 mr-1 ${loadingBackend ? 'animate-spin' : ''}`} />
                  刷新后端日志
                </button>
              )}
            </div>
          </div>

          <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 h-56 overflow-y-auto py-2 font-mono text-[11px] space-y-1">
            {viewMode === 'station' ? (
              filteredLogs.length === 0 ? (
                <div className="text-slate-500 py-8 text-center">
                  当前筛选条件下暂无台位操作记录。
                </div>
              ) : (
                filteredLogs.map((entry) => (
                  <div
                    key={entry.id}
                    className="flex items-start justify-between gap-3 py-1 px-2 rounded hover:bg-slate-900/90 border border-transparent hover:border-slate-800"
                  >
                    <div className="flex items-start gap-2 min-w-0">
                      <span className="text-slate-500 shrink-0">{entry.timestamp}</span>
                      <span
                        className={`px-1.5 py-0.2 rounded border text-[10px] shrink-0 ${badgeClass(
                          entry.level
                        )}`}
                      >
                        {entry.source}
                      </span>
                      <div className="min-w-0 break-all">
                        <span
                          className={
                            entry.level === 'error'
                              ? 'text-rose-300 font-semibold'
                              : entry.level === 'warn'
                              ? 'text-amber-300 font-semibold'
                              : entry.level === 'success'
                              ? 'text-emerald-300 font-semibold'
                              : 'text-slate-200 font-semibold'
                          }
                        >
                          {entry.title}
                        </span>
                        {entry.detail && (
                          <span className="text-slate-400 ml-2">{entry.detail}</span>
                        )}
                      </div>
                    </div>
                    {entry.action && (
                      <button
                        type="button"
                        onClick={() => navigateTo(entry.action!.tab, entry.action!.focusSection)}
                        className="shrink-0 inline-flex items-center px-2 py-0.5 rounded bg-blue-600/80 hover:bg-blue-500 text-white text-[10px]"
                      >
                        {entry.action.label}
                        <ArrowRight className="w-3 h-3 ml-1" />
                      </button>
                    )}
                  </div>
                ))
              )
            ) : backendLines.length === 0 ? (
              <div className="text-slate-500 py-8 text-center">暂无后端日志文件记录。</div>
            ) : (
              backendLines.map((line, idx) => (
                <div
                  key={idx}
                  className={`whitespace-pre-wrap break-all leading-relaxed ${
                    line.includes('ERROR') || line.includes('FAIL')
                      ? 'text-rose-300'
                      : line.includes('WARN')
                      ? 'text-amber-300'
                      : 'text-slate-300'
                  }`}
                >
                  {line}
                </div>
              ))
            )}
          </div>
        </div>
      )}
    </div>
  );
};
