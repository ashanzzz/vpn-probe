import React from 'react';
import {
  Activity,
  Wrench,
  Settings,
  LogOut,
  ShieldCheck,
  Radio,
  Layers,
  Terminal,
} from 'lucide-react';
import { WorkspaceTab, useFeedback } from '../context/FeedbackContext';

export type TabType = WorkspaceTab;

interface HeaderProps {
  currentTab: TabType;
  onTabChange: (tab: TabType) => void;
  onLogout: () => void;
  isInspecting: boolean;
}

export const Header: React.FC<HeaderProps> = ({
  currentTab,
  onTabChange,
  onLogout,
  isInspecting,
}) => {
  const { consoleOpen, setConsoleOpen, unreadErrors, logs } = useFeedback();

  const tabs: Array<{ id: TabType; label: string; icon: React.FC<{ className?: string }> }> = [
    { id: 'overview', label: '节点与拓扑总览', icon: Activity },
    { id: 'pipeline', label: '订阅与策略编排', icon: Layers },
    { id: 'vps', label: 'VPS 资产与自愈', icon: Wrench },
    { id: 'settings', label: '系统设置与日志', icon: Settings },
  ];

  return (
    <header className="bg-white border-b border-slate-200 sticky top-0 z-30 shadow-2xs">
      <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
        <div className="flex justify-between h-14 items-center gap-3">
          {/* Brand Logo */}
          <div className="flex items-center space-x-2.5 shrink-0">
            <div className="w-8 h-8 rounded-lg bg-blue-600 text-white flex items-center justify-center shadow-2xs">
              <Radio className="w-4 h-4" />
            </div>
            <div className="flex items-center space-x-2">
              <span className="font-bold text-slate-900 text-base tracking-tight">vpn-probe</span>
              {isInspecting ? (
                <span className="inline-flex items-center px-2 py-0.5 rounded-full text-[11px] font-medium bg-blue-50 text-blue-700 border border-blue-200">
                  <span className="w-1.5 h-1.5 mr-1.5 rounded-full bg-blue-500 animate-ping" />
                  巡检中
                </span>
              ) : (
                <span className="hidden sm:inline-flex items-center px-2 py-0.5 rounded-full text-[11px] font-medium bg-emerald-50 text-emerald-700 border border-emerald-200">
                  <span className="w-1.5 h-1.5 mr-1.5 rounded-full bg-emerald-500" />
                  监听中
                </span>
              )}
            </div>
          </div>

          {/* 4 Unified Navigation Workspaces */}
          <nav className="hidden md:flex items-center space-x-1">
            {tabs.map((tab) => {
              const Icon = tab.icon;
              const active = currentTab === tab.id;
              return (
                <button
                  key={tab.id}
                  type="button"
                  onClick={() => onTabChange(tab.id)}
                  className={`inline-flex items-center px-3 py-1.5 text-xs font-semibold rounded-lg transition-colors ${
                    active
                      ? 'bg-blue-50 text-blue-700 border border-blue-200 shadow-2xs'
                      : 'text-slate-600 hover:text-slate-900 hover:bg-slate-50 border border-transparent'
                  }`}
                >
                  <Icon className={`w-3.5 h-3.5 mr-1.5 ${active ? 'text-blue-600' : 'text-slate-400'}`} />
                  {tab.label}
                </button>
              );
            })}
          </nav>

          {/* Right Controls: Station Console Trigger + Admin Badge + Logout */}
          <div className="flex items-center space-x-2 shrink-0">
            <button
              type="button"
              onClick={() => setConsoleOpen((o) => !o)}
              title="展开/收起底部台位操作日志"
              className={`inline-flex items-center px-2.5 py-1 rounded-lg text-[11px] font-mono border transition ${
                consoleOpen
                  ? 'bg-slate-900 text-white border-slate-800'
                  : unreadErrors > 0
                  ? 'bg-rose-50 text-rose-700 border-rose-300'
                  : 'bg-slate-50 text-slate-600 border-slate-200 hover:bg-slate-100'
              }`}
            >
              <Terminal className="w-3.5 h-3.5 mr-1" />
              <span>台位日志</span>
              {unreadErrors > 0 ? (
                <span className="ml-1.5 px-1 rounded bg-rose-600 text-white text-[10px] font-bold">
                  {unreadErrors}
                </span>
              ) : logs.length > 0 ? (
                <span className="ml-1.5 text-[10px] opacity-70">{logs.length}</span>
              ) : null}
            </button>

            <div className="hidden lg:flex items-center space-x-1 text-[11px] text-slate-600 bg-slate-50 border border-slate-200 px-2 py-1 rounded-lg font-mono">
              <ShieldCheck className="w-3.5 h-3.5 text-emerald-600" />
              <span>Admin</span>
            </div>

            <button
              type="button"
              onClick={onLogout}
              title="退出登录并清除 Token"
              className="p-1.5 text-slate-400 hover:text-slate-700 hover:bg-slate-100 rounded-lg transition"
            >
              <LogOut className="w-4 h-4" />
            </button>
          </div>
        </div>

        {/* Mobile Nav */}
        <div className="md:hidden flex overflow-x-auto py-1.5 border-t border-slate-100 space-x-1">
          {tabs.map((tab) => {
            const Icon = tab.icon;
            const active = currentTab === tab.id;
            return (
              <button
                key={tab.id}
                type="button"
                onClick={() => onTabChange(tab.id)}
                className={`flex-shrink-0 inline-flex items-center px-2.5 py-1 text-xs font-medium rounded-md ${
                  active ? 'bg-blue-600 text-white' : 'text-slate-600 hover:bg-slate-100'
                }`}
              >
                <Icon className="w-3.5 h-3.5 mr-1" />
                {tab.label}
              </button>
            );
          })}
        </div>
      </div>
    </header>
  );
};
