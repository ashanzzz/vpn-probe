import React, { useState } from 'react';
import { 
  Gauge, 
  Globe, 
  Play, 
  RotateCw, 
  Pause, 
  CheckCircle2, 
  AlertTriangle,
  Zap,
  Clock
} from 'lucide-react';

interface QuotaBarProps {
  speedTestCount: number;
  maxSpeedTestCount: number;
  lastSpeedTestTime: string | null;
  splitCheckCount: number;
  maxSplitCheckCount: number;
  lastSplitCheckTime: string | null;
  pollingInterval: number; // in seconds (0 = paused)
  onPollingIntervalChange: (sec: number) => void;
  onTriggerInspection: (split: boolean) => Promise<void>;
  onTriggerSpeedTest: () => Promise<void>;
  isSpeedTesting: boolean;
  isInspecting: boolean;
}

export const QuotaBar: React.FC<QuotaBarProps> = ({
  speedTestCount,
  maxSpeedTestCount = 2,
  lastSpeedTestTime,
  splitCheckCount,
  maxSplitCheckCount = 1,
  lastSplitCheckTime,
  pollingInterval,
  onPollingIntervalChange,
  onTriggerInspection,
  onTriggerSpeedTest,
  isSpeedTesting,
  isInspecting,
}) => {
  const [showSpeedConfirm, setShowSpeedConfirm] = useState(false);

  return (
    <div className="bg-white border-b border-slate-200 px-4 py-3 sm:px-6 lg:px-8">
      <div className="max-w-7xl mx-auto flex flex-col lg:flex-row lg:items-center lg:justify-between gap-3">
        {/* Left: Schedule Quotas */}
        <div className="flex flex-wrap items-center gap-4 text-xs">
          {/* Speedtest Quota Widget */}
          <div className="flex items-center space-x-2 bg-slate-50 border border-slate-200 px-3 py-1.5 rounded-lg">
            <Gauge className="w-4 h-4 text-blue-600 flex-shrink-0" />
            <div className="flex items-center space-x-1.5">
              <span className="text-slate-500">今日自动测速 (自研 30MB):</span>
              <span className="font-mono font-semibold text-slate-800">
                {speedTestCount} / {maxSpeedTestCount} 次
              </span>
              {speedTestCount >= maxSpeedTestCount ? (
                <span className="text-amber-600 font-medium">(配额已满)</span>
              ) : (
                <CheckCircle2 className="w-3.5 h-3.5 text-emerald-500" />
              )}
            </div>
            {lastSpeedTestTime && (
              <span className="text-slate-400 border-l border-slate-200 pl-2">
                上次: {lastSpeedTestTime}
              </span>
            )}
          </div>

          {/* Split Check Quota Widget */}
          <div className="flex items-center space-x-2 bg-slate-50 border border-slate-200 px-3 py-1.5 rounded-lg">
            <Globe className="w-4 h-4 text-indigo-600 flex-shrink-0" />
            <div className="flex items-center space-x-1.5">
              <span className="text-slate-500">今日自动分流探测:</span>
              <span className="font-mono font-semibold text-slate-800">
                {splitCheckCount} / {maxSplitCheckCount} 次
              </span>
              {splitCheckCount >= maxSplitCheckCount ? (
                <span className="text-amber-600 font-medium">(配额已满)</span>
              ) : (
                <CheckCircle2 className="w-3.5 h-3.5 text-emerald-500" />
              )}
            </div>
            {lastSplitCheckTime && (
              <span className="text-slate-400 border-l border-slate-200 pl-2">
                上次: {lastSplitCheckTime}
              </span>
            )}
          </div>
        </div>

        {/* Right: Manual Trigger Actions & Polling Controller */}
        <div className="flex flex-wrap items-center gap-2">
          {/* Polling Interval Selector */}
          <div className="flex items-center space-x-1 bg-slate-50 border border-slate-200 p-1 rounded-lg text-xs">
            <Clock className="w-3.5 h-3.5 text-slate-400 ml-1.5 mr-0.5" />
            <span className="text-slate-500 text-2xs mr-1">轮询:</span>
            {[
              { label: '10s', value: 10 },
              { label: '30s', value: 30 },
              { label: '暂停', value: 0 },
            ].map((item) => (
              <button
                key={item.value}
                onClick={() => onPollingIntervalChange(item.value)}
                className={`px-2 py-0.5 rounded text-xs transition ${
                  pollingInterval === item.value
                    ? 'bg-white font-medium text-blue-700 shadow-2xs border border-slate-200'
                    : 'text-slate-500 hover:text-slate-800'
                }`}
              >
                {item.value === 0 ? <Pause className="w-2.5 h-2.5 inline mr-0.5" /> : null}
                {item.label}
              </button>
            ))}
          </div>

          {/* Trigger Quick Inspection */}
          <button
            onClick={() => onTriggerInspection(false)}
            disabled={isInspecting}
            className="inline-flex items-center px-3 py-1.5 text-xs font-medium rounded-lg text-slate-700 bg-white border border-slate-200 hover:bg-slate-50 hover:border-slate-300 disabled:opacity-50 transition shadow-2xs"
            title="立即触发连通性与送中快速轮询"
          >
            <RotateCw className={`w-3.5 h-3.5 mr-1.5 ${isInspecting ? 'animate-spin text-blue-600' : 'text-slate-500'}`} />
            {isInspecting ? '巡检中...' : '立即巡检'}
          </button>

          {/* Trigger Full Split Inspection */}
          <button
            onClick={() => onTriggerInspection(true)}
            disabled={isInspecting}
            className="inline-flex items-center px-3 py-1.5 text-xs font-medium rounded-lg text-slate-700 bg-white border border-slate-200 hover:bg-slate-50 hover:border-slate-300 disabled:opacity-50 transition shadow-2xs"
            title="执行包含 ChatGPT / Claude / YouTube / 百度 的全部分流连通测试"
          >
            <Globe className="w-3.5 h-3.5 mr-1.5 text-indigo-500" />
            分流探测(人工)
          </button>

          {/* Trigger Custom 30MB Speedtest */}
          <button
            onClick={() => setShowSpeedConfirm(true)}
            disabled={isSpeedTesting}
            className="inline-flex items-center px-3 py-1.5 text-xs font-medium rounded-lg text-white bg-blue-600 hover:bg-blue-700 disabled:opacity-50 transition shadow-2xs"
            title="针对 VPS 自建 30MB 真实测试文件进行无风控吞吐量测速（严禁使用开源第三方测速库）"
          >
            <Zap className={`w-3.5 h-3.5 mr-1.5 ${isSpeedTesting ? 'animate-pulse text-amber-300' : ''}`} />
            {isSpeedTesting ? '30M 测速中...' : '30M 吞吐测速(手工)'}
          </button>
        </div>
      </div>

      {/* Speed Test Confirmation Modal */}
      {showSpeedConfirm && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/40 backdrop-blur-2xs p-4">
          <div className="bg-white rounded-xl shadow-xl max-w-md w-full border border-slate-200 p-6">
            <div className="flex items-center space-x-3 text-amber-600 mb-3">
              <AlertTriangle className="w-6 h-6 flex-shrink-0" />
              <h3 className="font-semibold text-slate-900 text-base">启动自研 30MB 吞吐量测速？</h3>
            </div>
            <p className="text-xs text-slate-600 leading-relaxed mb-4">
              测速将通过沙箱代理针对自建 VPS 30MB 无风控测试文件（<code className="text-2xs bg-slate-100 p-1 rounded font-mono">speedtest-30m.bin</code>）进行真实带宽采样与排名。该操作将消耗每个节点约 30MB 流量。
            </p>
            <div className="flex justify-end space-x-2">
              <button
                onClick={() => setShowSpeedConfirm(false)}
                className="px-3.5 py-1.5 text-xs font-medium text-slate-600 hover:bg-slate-100 rounded-lg transition"
              >
                取消
              </button>
              <button
                onClick={() => {
                  setShowSpeedConfirm(false);
                  onTriggerSpeedTest();
                }}
                className="px-4 py-1.5 text-xs font-medium text-white bg-blue-600 hover:bg-blue-700 rounded-lg shadow-2xs transition flex items-center"
              >
                <Play className="w-3.5 h-3.5 mr-1.5 fill-current" />
                立即执行 30M 测速
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};
