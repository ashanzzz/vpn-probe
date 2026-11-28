import React, { createContext, useContext, useState, useCallback, useRef } from 'react';
import {
  CheckCircle2,
  AlertTriangle,
  XCircle,
  Info,
  X,
  ArrowRight,
} from 'lucide-react';

export type WorkspaceTab = 'overview' | 'pipeline' | 'vps' | 'settings';
export type ActivityLevel = 'info' | 'success' | 'warn' | 'error';

export interface ActivityAction {
  label: string;
  tab: WorkspaceTab;
  focusSection?: string;
}

export interface ActivityLogEntry {
  id: string;
  timestamp: string;
  level: ActivityLevel;
  source: string;
  title: string;
  detail?: string;
  nodeName?: string;
  action?: ActivityAction;
}

interface ToastItem extends ActivityLogEntry {}

interface FeedbackContextValue {
  logs: ActivityLogEntry[];
  nodeErrors: Record<string, { message: string; timestamp: string; action?: ActivityAction }>;
  unreadErrors: number;
  consoleOpen: boolean;
  setConsoleOpen: (open: boolean | ((prev: boolean) => boolean)) => void;
  clearUnreadErrors: () => void;
  clearLogs: () => void;
  clearNodeError: (nodeName: string) => void;
  pushActivity: (entry: {
    level: ActivityLevel;
    source: string;
    title: string;
    detail?: string;
    nodeName?: string;
    action?: ActivityAction;
    silentToast?: boolean;
  }) => void;
  navigateTo: (tab: WorkspaceTab, focusSection?: string) => void;
}

const FeedbackContext = createContext<FeedbackContextValue | null>(null);

export function inferActionFromError(message: string): ActivityAction | undefined {
  const lower = message.toLowerCase();
  if (
    message.includes('测速 URL 未配置') ||
    message.includes('测速源不可达') ||
    lower.includes('self_hosted_url') ||
    message.includes('HTTP 401') && message.includes('测速')
  ) {
    return {
      label: '去配置测速源',
      tab: 'settings',
      focusSection: 'speedtest',
    };
  }
  if (message.includes('4001') || message.includes('2FA') || message.includes('TOTP')) {
    return {
      label: '查看 VPS 状态',
      tab: 'vps',
    };
  }
  return undefined;
}

export const FeedbackProvider: React.FC<{
  onNavigate: (tab: WorkspaceTab, focusSection?: string) => void;
  children: React.ReactNode;
}> = ({ onNavigate, children }) => {
  const [logs, setLogs] = useState<ActivityLogEntry[]>([]);
  const [toasts, setToasts] = useState<ToastItem[]>([]);
  const [nodeErrors, setNodeErrors] = useState<
    Record<string, { message: string; timestamp: string; action?: ActivityAction }>
  >({});
  const [unreadErrors, setUnreadErrors] = useState(0);
  const [consoleOpen, setConsoleOpenState] = useState(false);
  const counterRef = useRef(0);

  const setConsoleOpen = useCallback((open: boolean | ((prev: boolean) => boolean)) => {
    setConsoleOpenState((prev) => {
      const next = typeof open === 'function' ? open(prev) : open;
      if (next) setUnreadErrors(0);
      return next;
    });
  }, []);

  const clearUnreadErrors = useCallback(() => setUnreadErrors(0), []);
  const clearLogs = useCallback(() => {
    setLogs([]);
    setUnreadErrors(0);
  }, []);

  const clearNodeError = useCallback((nodeName: string) => {
    if (!nodeName) return;
    setNodeErrors((prev) => {
      if (!(nodeName in prev)) return prev;
      const copy = { ...prev };
      delete copy[nodeName];
      return copy;
    });
  }, []);

  const dismissToast = useCallback((id: string) => {
    setToasts((prev) => prev.filter((t) => t.id !== id));
  }, []);

  const pushActivity = useCallback(
    (input: {
      level: ActivityLevel;
      source: string;
      title: string;
      detail?: string;
      nodeName?: string;
      action?: ActivityAction;
      silentToast?: boolean;
    }) => {
      counterRef.current += 1;
      const id = `${Date.now()}-${counterRef.current}`;
      const now = new Date();
      const timestamp = now.toLocaleTimeString([], {
        hour12: false,
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
      });

      const resolvedAction =
        input.action ||
        inferActionFromError(`${input.title} ${input.detail || ''}`);

      const entry: ActivityLogEntry = {
        id,
        timestamp,
        level: input.level,
        source: input.source,
        title: input.title,
        detail: input.detail,
        nodeName: input.nodeName,
        action: resolvedAction,
      };

      setLogs((prev) => [entry, ...prev].slice(0, 120));

      if (input.nodeName) {
        if (input.level === 'error' || input.level === 'warn') {
          setNodeErrors((prev) => ({
            ...prev,
            [input.nodeName!]: {
              message: input.detail ? `${input.title}: ${input.detail}` : input.title,
              timestamp,
              action: resolvedAction,
            },
          }));
        } else if (input.level === 'success') {
          setNodeErrors((prev) => {
            if (!(input.nodeName! in prev)) return prev;
            const copy = { ...prev };
            delete copy[input.nodeName!];
            return copy;
          });
        }
      }

      if (input.level === 'error' && !consoleOpen) {
        setUnreadErrors((prev) => prev + 1);
      }

      if (!input.silentToast) {
        setToasts((prev) => [entry, ...prev].slice(0, 4));
        const ttl = input.level === 'error' ? 9000 : 5000;
        window.setTimeout(() => {
          setToasts((prev) => prev.filter((t) => t.id !== id));
        }, ttl);
      }
    },
    [consoleOpen]
  );

  const navigateTo = useCallback(
    (tab: WorkspaceTab, focusSection?: string) => {
      onNavigate(tab, focusSection);
    },
    [onNavigate]
  );

  return (
    <FeedbackContext.Provider
      value={{
        logs,
        nodeErrors,
        unreadErrors,
        consoleOpen,
        setConsoleOpen,
        clearUnreadErrors,
        clearLogs,
        clearNodeError,
        pushActivity,
        navigateTo,
      }}
    >
      {children}

      {/* Top-Right Non-Blocking Toasts */}
      <div className="fixed top-16 right-4 z-50 flex flex-col gap-2 w-full max-w-md pointer-events-none px-2 sm:px-0">
        {toasts.map((t) => {
          const styleMap: Record<ActivityLevel, string> = {
            success: 'bg-white border-emerald-300 text-slate-900 shadow-lg',
            info: 'bg-white border-blue-300 text-slate-900 shadow-lg',
            warn: 'bg-amber-50 border-amber-300 text-amber-950 shadow-lg',
            error: 'bg-white border-rose-300 text-slate-900 shadow-lg',
          };
          const IconComp =
            t.level === 'success'
              ? CheckCircle2
              : t.level === 'warn'
              ? AlertTriangle
              : t.level === 'error'
              ? XCircle
              : Info;
          const iconColor =
            t.level === 'success'
              ? 'text-emerald-600'
              : t.level === 'warn'
              ? 'text-amber-600'
              : t.level === 'error'
              ? 'text-rose-600'
              : 'text-blue-600';

          return (
            <div
              key={t.id}
              className={`pointer-events-auto border rounded-xl p-3 flex items-start gap-2.5 transition-all ${styleMap[t.level]}`}
            >
              <IconComp className={`w-4 h-4 mt-0.5 shrink-0 ${iconColor}`} />
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-1.5">
                  <span className="text-[10px] font-mono px-1.5 py-0.5 rounded bg-slate-100 text-slate-600">
                    {t.source}
                  </span>
                  <span className="text-xs font-semibold truncate">{t.title}</span>
                </div>
                {t.detail && (
                  <p className="text-[11px] text-slate-600 mt-1 break-words leading-snug line-clamp-3">
                    {t.detail}
                  </p>
                )}
                <div className="flex items-center gap-3 mt-1.5">
                  {t.action && (
                    <button
                      type="button"
                      onClick={() => {
                        navigateTo(t.action!.tab, t.action!.focusSection);
                        dismissToast(t.id);
                      }}
                      className="inline-flex items-center text-[11px] font-semibold text-blue-600 hover:text-blue-800"
                    >
                      <span>{t.action.label}</span>
                      <ArrowRight className="w-3 h-3 ml-0.5" />
                    </button>
                  )}
                  <button
                    type="button"
                    onClick={() => setConsoleOpen(true)}
                    className="text-[11px] text-slate-500 hover:text-slate-800 underline"
                  >
                    台位日志
                  </button>
                </div>
              </div>
              <button
                type="button"
                onClick={() => dismissToast(t.id)}
                className="p-0.5 text-slate-400 hover:text-slate-700 rounded"
              >
                <X className="w-3.5 h-3.5" />
              </button>
            </div>
          );
        })}
      </div>
    </FeedbackContext.Provider>
  );
};

export function useFeedback(): FeedbackContextValue {
  const ctx = useContext(FeedbackContext);
  if (!ctx) {
    throw new Error('useFeedback must be used within FeedbackProvider');
  }
  return ctx;
}
