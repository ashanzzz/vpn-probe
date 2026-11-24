import React from 'react';
import { 
  Activity, 
  Wrench, 
  Share2, 
  Terminal, 
  Settings, 
  LogOut, 
  ShieldCheck,
  Radio,
  Layers,
  Network
} from 'lucide-react';

export type TabType = 'dashboard' | 'topology' | 'pipeline' | 'vps' | 'probe' | 'settings';

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
  const tabs = [
    { id: 'dashboard' as TabType, label: '节点监控大盘', icon: Activity },
    { id: 'topology' as TabType, label: '网络拓扑看板', icon: Network },
    { id: 'pipeline' as TabType, label: '订阅与编排枢纽', icon: Layers },
    { id: 'vps' as TabType, label: 'VPS 资产与自愈', icon: Wrench },
    { id: 'probe' as TabType, label: '沙箱即时探针', icon: Terminal },
    { id: 'settings' as TabType, label: '系统设置与审计', icon: Settings },
  ];

  return (
    <header className="bg-white border-b border-slate-200 sticky top-0 z-30 shadow-xs">
      <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
        <div className="flex justify-between h-16 items-center">
          {/* Brand Logo & Name */}
          <div className="flex items-center space-x-3">
            <div className="w-9 h-9 rounded-lg bg-blue-600 text-white flex items-center justify-center shadow-xs">
              <Radio className="w-5 h-5" />
            </div>
            <div>
              <div className="flex items-center space-x-2">
                <span className="font-bold text-slate-900 text-lg tracking-tight">vpn-probe</span>
                <span className="text-xs px-2 py-0.5 rounded-full bg-slate-100 text-slate-600 font-mono border border-slate-200">
                  v1.0
                </span>
                {isInspecting ? (
                  <span className="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-blue-50 text-blue-700 border border-blue-200 animate-pulse">
                    <span className="w-1.5 h-1.5 mr-1.5 rounded-full bg-blue-500 animate-ping"></span>
                    探针巡检中...
                  </span>
                ) : (
                  <span className="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-emerald-50 text-emerald-700 border border-emerald-200">
                    <span className="w-1.5 h-1.5 mr-1.5 rounded-full bg-emerald-500"></span>
                    在线监听
                  </span>
                )}
              </div>
            </div>
          </div>

          {/* Navigation Workspaces */}
          <nav className="hidden md:flex space-x-1">
            {tabs.map((tab) => {
              const Icon = tab.icon;
              const active = currentTab === tab.id;
              return (
                <button
                  key={tab.id}
                  onClick={() => onTabChange(tab.id)}
                  className={`inline-flex items-center px-3.5 py-2 text-sm font-medium rounded-lg transition-colors ${
                    active
                      ? 'bg-blue-50 text-blue-700 border border-blue-200 shadow-2xs'
                      : 'text-slate-600 hover:text-slate-900 hover:bg-slate-50'
                  }`}
                >
                  <Icon className={`w-4 h-4 mr-2 ${active ? 'text-blue-600' : 'text-slate-400'}`} />
                  {tab.label}
                </button>
              );
            })}
          </nav>

          {/* Right Admin Badge & Logout */}
          <div className="flex items-center space-x-3">
            <div className="flex items-center space-x-1.5 text-xs text-slate-600 bg-slate-50 border border-slate-200 px-2.5 py-1.5 rounded-lg font-mono">
              <ShieldCheck className="w-4 h-4 text-emerald-600" />
              <span>Admin 已认证</span>
            </div>
            <button
              onClick={onLogout}
              title="退出登录并清除 Token"
              className="p-2 text-slate-400 hover:text-slate-700 hover:bg-slate-100 rounded-lg transition"
            >
              <LogOut className="w-4 h-4" />
            </button>
          </div>
        </div>

        {/* Mobile Nav */}
        <div className="md:hidden flex overflow-x-auto py-2 border-t border-slate-100 space-x-1">
          {tabs.map((tab) => {
            const Icon = tab.icon;
            const active = currentTab === tab.id;
            return (
              <button
                key={tab.id}
                onClick={() => onTabChange(tab.id)}
                className={`flex-shrink-0 inline-flex items-center px-3 py-1.5 text-xs font-medium rounded-md ${
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
