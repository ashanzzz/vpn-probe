import React, { useState } from 'react';
import { 
  X, 
  Wrench, 
  ArrowRight, 
  CheckCircle2, 
  AlertTriangle, 
  Loader2, 
  Copy, 
  Check, 
  RefreshCw,
  Server,
  Terminal,
  ShieldCheck
} from 'lucide-react';
import { api } from '../api/client';
import { PortHealResponse } from '../types';

interface HealingWizardProps {
  initialNodeName?: string;
  availableNodes: string[];
  onClose: () => void;
  onSuccess: (res: PortHealResponse) => void;
}

export const HealingWizard: React.FC<HealingWizardProps> = ({
  initialNodeName,
  availableNodes,
  onClose,
  onSuccess,
}) => {
  const [selectedNode, setSelectedNode] = useState(initialNodeName || availableNodes[0] || '');
  const [customPort, setCustomPort] = useState(() => Math.floor(20000 + Math.random() * 40000));
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<PortHealResponse | null>(null);
  const [copied, setCopied] = useState(false);

  // Suggested random port generator
  const handleRandomizePort = () => {
    setCustomPort(Math.floor(20000 + Math.random() * 40000));
  };

  const handleStartHeal = async () => {
    if (!selectedNode) {
      setError('请选择需要换端口自愈的目标节点');
      return;
    }

    setLoading(true);
    setError(null);

    try {
      const resp = await api.healPort(selectedNode, customPort);
      setResult(resp);
      onSuccess(resp);
    } catch (err: any) {
      setError(`换端口自愈失败: ${err.message || 'SSH 执行超时或远端拒绝'}`);
    } finally {
      setLoading(false);
    }
  };

  const handleCopyNewURL = () => {
    if (result?.new_url) {
      navigator.clipboard.writeText(result.new_url);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/50 backdrop-blur-2xs p-4">
      <div className="bg-white rounded-2xl shadow-2xl max-w-lg w-full border border-slate-200 overflow-hidden flex flex-col animate-in zoom-in-95 duration-150">
        {/* Header */}
        <div className="px-6 py-4 border-b border-slate-200 flex items-center justify-between bg-slate-50">
          <div className="flex items-center space-x-2.5">
            <div className="w-8 h-8 rounded-lg bg-red-100 text-red-600 flex items-center justify-center">
              <Wrench className="w-4 h-4" />
            </div>
            <div>
              <h3 className="font-bold text-slate-900 text-sm">VPS 换端口自愈向导</h3>
              <p className="text-2xs text-slate-500">自动 SSH 改写远端配置 · 重启服务 · 同步 SubStore</p>
            </div>
          </div>
          <button
            onClick={onClose}
            className="p-1 text-slate-400 hover:text-slate-700 hover:bg-slate-200 rounded-lg transition"
          >
            <X className="w-4 h-4" />
          </button>
        </div>

        {/* Content Body */}
        <div className="p-6 space-y-5">
          {!result ? (
            <>
              {/* Step 1: Select Node */}
              <div>
                <label className="block text-xs font-semibold text-slate-700 mb-1.5">
                  1. 选择发生阻断的目标节点
                </label>
                <select
                  value={selectedNode}
                  onChange={(e) => setSelectedNode(e.target.value)}
                  disabled={loading}
                  className="block w-full px-3 py-2 border border-slate-300 rounded-lg text-sm bg-white focus:outline-none focus:ring-2 focus:ring-blue-600 focus:border-blue-600"
                >
                  {availableNodes.map((name) => (
                    <option key={name} value={name}>
                      {name}
                    </option>
                  ))}
                </select>
              </div>

              {/* Step 2: Target Port Selection */}
              <div>
                <div className="flex justify-between items-center mb-1.5">
                  <label className="block text-xs font-semibold text-slate-700">
                    2. 指定迁移后的高位目标端口
                  </label>
                  <button
                    type="button"
                    onClick={handleRandomizePort}
                    disabled={loading}
                    className="text-2xs text-blue-600 hover:text-blue-800 flex items-center"
                  >
                    <RefreshCw className="w-3 h-3 mr-1" />
                    重新随机生成
                  </button>
                </div>
                <div className="relative">
                  <input
                    type="number"
                    min="10000"
                    max="65535"
                    value={customPort}
                    onChange={(e) => setCustomPort(parseInt(e.target.value) || 0)}
                    disabled={loading}
                    className="block w-full px-3 py-2 border border-slate-300 rounded-lg text-sm font-mono focus:outline-none focus:ring-2 focus:ring-blue-600 focus:border-blue-600"
                  />
                  <div className="absolute inset-y-0 right-0 pr-3 flex items-center pointer-events-none text-2xs text-slate-400 font-mono">
                    安全高位端口 (10000-65535)
                  </div>
                </div>
              </div>

              {/* Step 3: Self-healing Workflow Outline */}
              <div className="p-3 bg-slate-50 border border-slate-200 rounded-xl space-y-2 text-2xs text-slate-600">
                <div className="font-semibold text-slate-700 flex items-center">
                  <Terminal className="w-3.5 h-3.5 mr-1.5 text-slate-500" />
                  自愈系统全自动化执行链：
                </div>
                <div className="space-y-1.5 pl-5 list-decimal">
                  <div>1. 建立受控安全 SSH 会话至目标 VPS 实例；</div>
                  <div>2. 原子热替换 sing-box / xray 监听 inbound port 为 {customPort}；</div>
                  <div>3. 重载并守护重启远端服务进程；</div>
                  <div>4. 本地启动临时沙箱探针秒级复测；</div>
                  <div>5. 回写本地 config.yaml 并自动同步推送至 Sub-Store 订阅源。</div>
                </div>
              </div>

              {error && (
                <div className="p-3 bg-red-50 border border-red-200 rounded-lg text-xs text-red-700 flex items-start space-x-2">
                  <AlertTriangle className="w-4 h-4 text-red-600 flex-shrink-0 mt-0.5" />
                  <span>{error}</span>
                </div>
              )}
            </>
          ) : (
            /* Success View */
            <div className="space-y-4">
              <div className="p-4 bg-emerald-50 border border-emerald-200 rounded-xl text-center space-y-2">
                <CheckCircle2 className="w-10 h-10 text-emerald-600 mx-auto" />
                <h4 className="text-sm font-bold text-emerald-900">换端口自愈操作圆满成功！</h4>
                <p className="text-xs text-emerald-700">{result.message}</p>
              </div>

              <div className="p-3 bg-slate-50 border border-slate-200 rounded-xl space-y-2 text-xs">
                <div className="flex justify-between items-center py-1 border-b border-slate-200">
                  <span className="text-slate-500">原阻断端口:</span>
                  <span className="font-mono text-red-600 line-through">{result.old_port}</span>
                </div>
                <div className="flex justify-between items-center py-1 border-b border-slate-200">
                  <span className="text-slate-500">新迁移端口:</span>
                  <span className="font-mono font-bold text-emerald-600">{result.new_port}</span>
                </div>
                <div className="flex justify-between items-center py-1 border-b border-slate-200">
                  <span className="text-slate-500">SubStore 订阅同步:</span>
                  <span className="font-medium text-emerald-700">
                    {result.substore_synced ? '✅ 已更新' : '跳过'}
                  </span>
                </div>
                <div className="flex justify-between items-center py-1 border-b border-slate-200">
                  <span className="text-slate-500">本地配置热更新:</span>
                  <span className="font-medium text-emerald-700">
                    {result.config_synced ? '✅ 已持久化' : '跳过'}
                  </span>
                </div>
              </div>

              {result.new_url && (
                <div>
                  <div className="text-xs font-semibold text-slate-700 mb-1">
                    新节点链接 (已热替换)：
                  </div>
                  <div className="p-2.5 bg-slate-100 rounded-lg text-2xs font-mono break-all text-slate-800 select-all border border-slate-200 flex items-center justify-between">
                    <span className="truncate pr-2">{result.new_url}</span>
                    <button
                      onClick={handleCopyNewURL}
                      className="flex-shrink-0 text-blue-600 hover:text-blue-800 text-xs font-medium"
                    >
                      {copied ? <Check className="w-4 h-4 text-emerald-600" /> : <Copy className="w-4 h-4" />}
                    </button>
                  </div>
                </div>
              )}
            </div>
          )}
        </div>

        {/* Footer Actions */}
        <div className="px-6 py-4 bg-slate-50 border-t border-slate-200 flex justify-end space-x-2">
          {!result ? (
            <>
              <button
                type="button"
                onClick={onClose}
                disabled={loading}
                className="px-4 py-2 text-xs font-medium text-slate-600 hover:bg-slate-200 rounded-lg transition"
              >
                取消
              </button>
              <button
                type="button"
                onClick={handleStartHeal}
                disabled={loading}
                className="px-4 py-2 text-xs font-medium text-white bg-red-600 hover:bg-red-700 rounded-lg shadow-2xs transition flex items-center disabled:opacity-50"
              >
                {loading ? (
                  <>
                    <Loader2 className="w-3.5 h-3.5 mr-1.5 animate-spin" />
                    正在远程 SSH 自愈中...
                  </>
                ) : (
                  <>
                    启动换端口自愈
                    <ArrowRight className="w-3.5 h-3.5 ml-1.5" />
                  </>
                )}
              </button>
            </>
          ) : (
            <button
              type="button"
              onClick={onClose}
              className="px-5 py-2 text-xs font-semibold text-white bg-blue-600 hover:bg-blue-700 rounded-lg shadow-2xs transition"
            >
              完成并关闭
            </button>
          )}
        </div>
      </div>
    </div>
  );
};
