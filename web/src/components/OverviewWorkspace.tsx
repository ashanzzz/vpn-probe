import React, { useState } from 'react';
import {
  Activity,
  Network,
  Gauge,
  Globe,
  RotateCw,
  Play,
  ShieldAlert,
  Terminal,
  ArrowRight,
  Pause,
} from 'lucide-react';
import { MonitorStatusMap, NodeProbeResult } from '../types';
import { MonitorDashboard } from './MonitorDashboard';
import { TopologyView } from './TopologyView';
import { SandboxProbeDrawer } from './SandboxProbeDrawer';
import { useFeedback } from '../context/FeedbackContext';

interface BlockedNodeInfo {
  name: string;
  blocked_port: number;
  status_text?: string;
  target_ip?: string;
}

interface OverviewWorkspaceProps {
  statusMap: MonitorStatusMap;
  blockedNodes: BlockedNodeInfo[];
  speedTestCount: number;
  lastSpeedTestTime: string | null;
  splitCheckCount: number;
  lastSplitCheckTime: string | null;
  pollingInterval: number;
  onPollingIntervalChange: (sec: number) => void;
  onTriggerInspection: (split: boolean) => Promise<void>;
  onTriggerSpeedTest: () => Promise<void>;
  isSpeedTesting: boolean;
  isInspecting: boolean;
  onOpenReport: (node: NodeProbeResult, initialTab?: 'overview' | 'ip_intelligence' | 'history' | 'logs') => void;
  onRetestNode: (node: NodeProbeResult) => Promise<void>;
  onSpeedTestNode: (node: NodeProbeResult) => Promise<void>;
  onOpenHealingWizard: (nodeName?: string) => void;
}

export const OverviewWorkspace: React.FC<OverviewWorkspaceProps> = ({
  statusMap,
  blockedNodes,
  speedTestCount,
  lastSpeedTestTime,
  splitCheckCount,
  lastSplitCheckTime,
  pollingInterval,
  onPollingIntervalChange,
  onTriggerInspection,
  onTriggerSpeedTest,
  isSpeedTesting,
  isInspecting,
  onOpenReport,
  onRetestNode,
  onSpeedTestNode,
  onOpenHealingWizard,
}) => {
  const { nodeErrors, navigateTo } = useFeedback();
  const [subView, setSubView] = useState<'nodes' | 'topology'>('nodes');
  const [sandboxOpen, setSandboxOpen] = useState(false);

  const errorList = Object.entries(nodeErrors);

  return (
    <div className="space-y-4">
      {/* Compact Consolidated Control & Quota Strip (replaces stacked global banners) */}
      <div className="bg-white border border-slate-200 rounded-xl p-3 shadow-2xs flex flex-col gap-3">
        <div className="flex flex-wrap items-center justify-between gap-3">
          {/* Left: Sub-view switcher + Quota pills */}
          <div className="flex flex-wrap items-center gap-2.5">
            <div className="inline-flex rounded-lg border border-slate-200 bg-slate-50 p-0.5 text-xs">
              <button
                type="button"
                onClick={() => setSubView('nodes')}
                className={`inline-flex items-center px-3 py-1 rounded-md font-semibold transition ${
                  subView === 'nodes'
                    ? 'bg-white text-blue-700 shadow-2xs'
                    : 'text-slate-600 hover:text-slate-900'
                }`}
              >
                <Activity className="w-3.5 h-3.5 mr-1.5" />
                节点明细与家宽池
              </button>
              <button
                type="button"
                onClick={() => setSubView('topology')}
                className={`inline-flex items-center px-3 py-1 rounded-md font-semibold transition ${
                  subView === 'topology'
                    ? 'bg-white text-blue-700 shadow-2xs'
                    : 'text-slate-600 hover:text-slate-900'
                }`}
              >
                <Network className="w-3.5 h-3.5 mr-1.5" />
                四阶网络拓扑图
              </button>
            </div>

            <div className="flex items-center space-x-1.5 bg-slate-50 border border-slate-200 px-2.5 py-1 rounded-lg text-[11px]">
              <Gauge className="w-3.5 h-3.5 text-blue-600" />
              <span className="text-slate-500">10M 测速:</span>
              <span className="font-mono font-semibold text-slate-800">{speedTestCount}/2</span>
              {lastSpeedTestTime && <span className="text-slate-400">({lastSpeedTestTime})</span>}
            </div>

            <div className="flex items-center space-x-1.5 bg-slate-50 border border-slate-200 px-2.5 py-1 rounded-lg text-[11px]">
              <Globe className="w-3.5 h-3.5 text-indigo-600" />
              <span className="text-slate-500">分流探测:</span>
              <span className="font-mono font-semibold text-slate-800">{splitCheckCount}/1</span>
              {lastSplitCheckTime && <span className="text-slate-400">({lastSplitCheckTime})</span>}
            </div>
          </div>

          {/* Right: Polling & Action Buttons */}
          <div className="flex flex-wrap items-center gap-2">
            <div className="flex items-center space-x-0.5 bg-slate-50 border border-slate-200 p-0.5 rounded-lg text-[11px]">
              <span className="px-1.5 text-slate-400">刷新:</span>
              {[2, 10, 30].map((sec) => (
                <button
                  key={sec}
                  type="button"
                  onClick={() => onPollingIntervalChange(sec)}
                  className={`px-2 py-0.5 rounded font-mono ${
                    pollingInterval === sec
                      ? 'bg-blue-600 text-white'
                      : 'text-slate-600 hover:bg-slate-200'
                  }`}
                >
                  {sec}s
                </button>
              ))}
              <button
                type="button"
                onClick={() => onPollingIntervalChange(0)}
                className={`px-1.5 py-0.5 rounded inline-flex items-center ${
                  pollingInterval === 0
                    ? 'bg-amber-600 text-white'
                    : 'text-slate-600 hover:bg-slate-200'
                }`}
              >
                <Pause className="w-3 h-3" />
              </button>
            </div>

            <button
              type="button"
              onClick={() => onTriggerInspection(false)}
              disabled={isInspecting}
              className="inline-flex items-center px-2.5 py-1.5 text-xs font-medium rounded-lg text-slate-700 bg-white border border-slate-200 hover:bg-slate-50 disabled:opacity-50 transition"
            >
              <RotateCw className={`w-3.5 h-3.5 mr-1 ${isInspecting ? 'animate-spin text-blue-600' : 'text-slate-500'}`} />
              立即巡检
            </button>

            <button
              type="button"
              onClick={() => onTriggerInspection(true)}
              disabled={isInspecting}
              className="inline-flex items-center px-2.5 py-1.5 text-xs font-medium rounded-lg text-indigo-700 bg-indigo-50 border border-indigo-200 hover:bg-indigo-100 disabled:opacity-50 transition"
            >
              <Play className="w-3 h-3 mr-1 fill-current" />
              含分流巡检
            </button>

            <button
              type="button"
              onClick={onTriggerSpeedTest}
              disabled={isSpeedTesting}
              className="inline-flex items-center px-2.5 py-1.5 text-xs font-semibold rounded-lg text-white bg-blue-600 hover:bg-blue-700 disabled:opacity-50 transition shadow-2xs"
            >
              <Gauge className={`w-3.5 h-3.5 mr-1 ${isSpeedTesting ? 'animate-pulse' : ''}`} />
              {isSpeedTesting ? '测速队列中...' : '全量 10M 测速'}
            </button>

            <button
              type="button"
              onClick={() => setSandboxOpen(true)}
              className="inline-flex items-center px-2.5 py-1.5 text-xs font-medium rounded-lg text-slate-700 bg-slate-100 hover:bg-slate-200 border border-slate-200 transition"
            >
              <Terminal className="w-3.5 h-3.5 mr-1 text-slate-600" />
              沙箱测链接
            </button>
          </div>
        </div>

        {/* Inline Port-Blocked Alert Strip (only shown inside Overview when active) */}
        {blockedNodes.length > 0 && (
          <div className="flex flex-wrap items-center justify-between gap-2 bg-rose-50 border border-rose-200 rounded-lg px-3 py-2 text-xs">
            <div className="flex items-center gap-2 text-rose-800">
              <ShieldAlert className="w-4 h-4 text-rose-600 shrink-0" />
              <span className="font-semibold">
                {blockedNodes.length} 个节点端口疑似遭阻断：
              </span>
              <span className="font-mono">
                {blockedNodes.map((n) => `${n.name}:${n.blocked_port}`).join(' · ')}
              </span>
            </div>
            <button
              type="button"
              onClick={() => onOpenHealingWizard(blockedNodes[0]?.name)}
              className="inline-flex items-center px-2.5 py-1 rounded bg-rose-600 hover:bg-rose-700 text-white font-semibold"
            >
              启动自愈向导
              <ArrowRight className="w-3 h-3 ml-1" />
            </button>
          </div>
        )}

        {/* Inline Active Node Action Errors (e.g. Speedtest URL missing on specific node) */}
        {errorList.length > 0 && (
          <div className="bg-amber-50/90 border border-amber-200 rounded-lg px-3 py-2 text-xs space-y-1">
            {errorList.slice(0, 3).map(([nodeName, errInfo]) => (
              <div key={nodeName} className="flex flex-wrap items-center justify-between gap-2">
                <div className="font-mono text-amber-950 truncate">
                  <span className="font-bold">[{nodeName}]</span>{' '}
                  <span>{errInfo.message}</span>
                </div>
                {errInfo.action && (
                  <button
                    type="button"
                    onClick={() => navigateTo(errInfo.action!.tab, errInfo.action!.focusSection)}
                    className="inline-flex items-center text-blue-700 hover:text-blue-900 font-semibold shrink-0"
                  >
                    {errInfo.action.label}
                    <ArrowRight className="w-3 h-3 ml-0.5" />
                  </button>
                )}
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Sub-view Render */}
      {subView === 'nodes' ? (
        <MonitorDashboard
          statusMap={statusMap}
          onOpenReport={onOpenReport}
          onRetestNode={onRetestNode}
          onSpeedTestNode={onSpeedTestNode}
          onOpenHealingWizard={onOpenHealingWizard}
          isInspecting={isInspecting}
        />
      ) : (
        <TopologyView onOpenReport={(node) => onOpenReport(node)} />
      )}

      <SandboxProbeDrawer open={sandboxOpen} onClose={() => setSandboxOpen(false)} />
    </div>
  );
};
