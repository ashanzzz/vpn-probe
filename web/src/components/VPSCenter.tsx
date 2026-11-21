import React, { useState, useEffect } from 'react';
import { 
  Server, 
  Wrench, 
  Download, 
  RotateCw, 
  CheckCircle2, 
  AlertTriangle, 
  Loader2, 
  Terminal,
  ShieldAlert,
  ArrowUpCircle,
  ExternalLink,
  Cpu
} from 'lucide-react';
import { api } from '../api/client';
import { SoftwareInfo, BlockedAnalysisResult } from '../types';

interface VPSCenterProps {
  onOpenHealingWizard: (nodeName: string) => void;
}

export const VPSCenter: React.FC<VPSCenterProps> = ({ onOpenHealingWizard }) => {
  const [instances, setInstances] = useState<SoftwareInfo[]>([]);
  const [blockedAnalysis, setBlockedAnalysis] = useState<BlockedAnalysisResult[]>([]);
  const [loading, setLoading] = useState(true);
  const [updatingNode, setUpdatingNode] = useState<string | null>(null);
  const [message, setMessage] = useState<{ type: 'success' | 'error'; text: string } | null>(null);

  const fetchData = async () => {
    setLoading(true);
    try {
      const [vpsData, analysisData] = await Promise.all([
        api.getVPSManagement().catch(() => ({ count: 0, instances: [] })),
        api.getBlockedAnalysis().catch(() => ({ count: 0, results: [] })),
      ]);
      setInstances(vpsData.instances || []);
      setBlockedAnalysis(analysisData.results || []);
    } catch (err: any) {
      setMessage({ type: 'error', text: err.message || '获取 VPS 运维数据失败' });
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchData();
  }, []);

  const handleUpdate = async (name: string) => {
    setUpdatingNode(name);
    setMessage(null);
    try {
      const res = await api.updateVPSProxy(name);
      setMessage({ type: 'success', text: `节点 ${name} 软件升级完成: ${res.message || 'OK'}` });
      await fetchData();
    } catch (err: any) {
      setMessage({ type: 'error', text: `升级失败: ${err.message}` });
    } finally {
      setUpdatingNode(null);
    }
  };

  return (
    <div className="space-y-6">
      {/* Header bar */}
      <div className="bg-white border border-slate-200 rounded-xl p-4 shadow-2xs flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
        <div>
          <h2 className="text-base font-bold text-slate-900 flex items-center">
            <Cpu className="w-5 h-5 mr-2 text-blue-600" />
            VPS 资产与软件生命周期中心
          </h2>
          <p className="text-xs text-slate-500 mt-0.5">
            受控 SSH 执行引擎 · 官方 GitHub Upstream 版本自动感知 · 深度墙况研判与换端口自愈
          </p>
        </div>

        <button
          onClick={fetchData}
          disabled={loading}
          className="inline-flex items-center px-3 py-1.5 text-xs font-medium rounded-lg text-slate-700 bg-white border border-slate-200 hover:bg-slate-50 disabled:opacity-50 transition shadow-2xs"
        >
          <RotateCw className={`w-3.5 h-3.5 mr-1.5 ${loading ? 'animate-spin text-blue-600' : 'text-slate-500'}`} />
          刷新 VPS 状态
        </button>
      </div>

      {message && (
        <div
          className={`p-3 rounded-lg text-xs flex items-center justify-between border ${
            message.type === 'success'
              ? 'bg-emerald-50 text-emerald-800 border-emerald-200'
              : 'bg-red-50 text-red-800 border-red-200'
          }`}
        >
          <span>{message.text}</span>
          <button onClick={() => setMessage(null)} className="text-xs underline ml-2">
            关闭
          </button>
        </div>
      )}

      {/* 1. Deep Blocked Analysis Table */}
      <div className="bg-white border border-slate-200 rounded-xl shadow-2xs overflow-hidden">
        <div className="px-4 py-3 border-b border-slate-200 bg-slate-50 flex items-center justify-between">
          <div className="flex items-center space-x-2">
            <ShieldAlert className="w-4 h-4 text-red-600" />
            <h3 className="text-xs font-bold text-slate-900 uppercase tracking-wider">
              墙况深度研判与自愈诊断矩阵 (GFW Diagnostic Analysis)
            </h3>
          </div>
          <span className="text-2xs font-mono text-slate-400">
            国内/海外双向比对
          </span>
        </div>

        <div className="overflow-x-auto">
          <table className="min-w-full divide-y divide-slate-200 text-left text-xs">
            <thead className="bg-slate-50 text-slate-500 uppercase tracking-wider font-semibold">
              <tr>
                <th scope="col" className="px-4 py-2.5">节点名称</th>
                <th scope="col" className="px-4 py-2.5">IP 与端口</th>
                <th scope="col" className="px-4 py-2.5">国内 TCP</th>
                <th scope="col" className="px-4 py-2.5">海外拨号</th>
                <th scope="col" className="px-4 py-2.5">故障分类研判</th>
                <th scope="col" className="px-4 py-2.5">自愈建议 / 动作</th>
                <th scope="col" className="px-4 py-2.5 text-right">自愈操作</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-200 bg-white">
              {blockedAnalysis.length > 0 ? (
                blockedAnalysis.map((item, idx) => {
                  const isBlocked = item.fault_type === 'PORT_BLOCKED';
                  const isDown = item.fault_type === 'MACHINE_DOWN';
                  return (
                    <tr key={idx} className="hover:bg-slate-50/80 transition">
                      <td className="px-4 py-3 font-semibold text-slate-900">
                        {item.node_name}
                      </td>
                      <td className="px-4 py-3 font-mono text-slate-600">
                        {item.server_ip}:{item.server_port}
                      </td>
                      <td className="px-4 py-3 whitespace-nowrap">
                        {item.domestic_tcp ? (
                          <span className="inline-flex items-center text-emerald-700 font-medium">
                            <CheckCircle2 className="w-3.5 h-3.5 mr-1 text-emerald-500" />
                            连通
                          </span>
                        ) : (
                          <span className="inline-flex items-center text-red-700 font-medium">
                            <AlertTriangle className="w-3.5 h-3.5 mr-1 text-red-500" />
                            阻断
                          </span>
                        )}
                      </td>
                      <td className="px-4 py-3 whitespace-nowrap">
                        {item.abroad_tcp ? (
                          <span className="inline-flex items-center text-emerald-700 font-mono">
                            <CheckCircle2 className="w-3.5 h-3.5 mr-1 text-emerald-500" />
                            {item.abroad_rtt_ms}ms ({item.abroad_source || 'OK'})
                          </span>
                        ) : (
                          <span className="text-slate-400">失败</span>
                        )}
                      </td>
                      <td className="px-4 py-3 whitespace-nowrap">
                        {isBlocked ? (
                          <span className="px-2 py-0.5 rounded text-2xs font-semibold bg-red-100 text-red-800 border border-red-200">
                            PORT_BLOCKED (端口被墙)
                          </span>
                        ) : isDown ? (
                          <span className="px-2 py-0.5 rounded text-2xs font-semibold bg-slate-100 text-slate-700">
                            MACHINE_DOWN (服务关机)
                          </span>
                        ) : (
                          <span className="px-2 py-0.5 rounded text-2xs font-semibold bg-emerald-100 text-emerald-800">
                            HEALTHY (正常)
                          </span>
                        )}
                      </td>
                      <td className="px-4 py-3 text-slate-600 max-w-xs truncate" title={item.diagnostic_message}>
                        {item.recommended_action || item.diagnostic_message}
                      </td>
                      <td className="px-4 py-3 text-right">
                        {item.can_auto_heal_port ? (
                          <button
                            onClick={() => onOpenHealingWizard(item.node_name)}
                            className="inline-flex items-center px-2.5 py-1 text-xs font-semibold rounded bg-red-600 hover:bg-red-700 text-white shadow-2xs transition"
                          >
                            <Wrench className="w-3.5 h-3.5 mr-1" />
                            换端口自愈
                          </button>
                        ) : (
                          <span className="text-slate-400 text-2xs">正常无须自愈</span>
                        )}
                      </td>
                    </tr>
                  );
                })
              ) : (
                <tr>
                  <td colSpan={7} className="px-4 py-8 text-center text-slate-400">
                    暂无阻断分析数据（所有节点连通良好或探针巡检采集中）
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>

      {/* 2. VPS Instances & Software Lifecycle */}
      <div className="bg-white border border-slate-200 rounded-xl shadow-2xs overflow-hidden">
        <div className="px-4 py-3 border-b border-slate-200 bg-slate-50 flex items-center justify-between">
          <div className="flex items-center space-x-2">
            <Server className="w-4 h-4 text-blue-600" />
            <h3 className="text-xs font-bold text-slate-900 uppercase tracking-wider">
              VPS 托管实例与代理软件版本
            </h3>
          </div>
          <span className="text-2xs font-mono text-slate-400">
            SSH Lifecycle Manager
          </span>
        </div>

        <div className="overflow-x-auto">
          <table className="min-w-full divide-y divide-slate-200 text-left text-xs">
            <thead className="bg-slate-50 text-slate-500 uppercase tracking-wider font-semibold">
              <tr>
                <th scope="col" className="px-4 py-2.5">主机实例</th>
                <th scope="col" className="px-4 py-2.5">公网 IP</th>
                <th scope="col" className="px-4 py-2.5">代理软件</th>
                <th scope="col" className="px-4 py-2.5">部署模式</th>
                <th scope="col" className="px-4 py-2.5">当前版本 / 上游最新</th>
                <th scope="col" className="px-4 py-2.5">活跃端口</th>
                <th scope="col" className="px-4 py-2.5">状态</th>
                <th scope="col" className="px-4 py-2.5 text-right">生命周期操作</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-200 bg-white">
              {instances.length > 0 ? (
                instances.map((inst, idx) => (
                  <tr key={idx} className="hover:bg-slate-50/80 transition">
                    <td className="px-4 py-3 font-semibold text-slate-900">
                      {inst.instance_name || inst.instance_id}
                    </td>
                    <td className="px-4 py-3 font-mono text-slate-700">
                      {inst.ip}
                    </td>
                    <td className="px-4 py-3 font-mono">
                      <span className="px-2 py-0.5 rounded bg-blue-50 text-blue-700 font-semibold border border-blue-200 text-2xs uppercase">
                        {inst.software_type}
                      </span>
                    </td>
                    <td className="px-4 py-3 text-slate-600">
                      {inst.deploy_mode || 'native'}
                    </td>
                    <td className="px-4 py-3 whitespace-nowrap">
                      <div className="flex items-center space-x-1.5 font-mono">
                        <span className="font-semibold text-slate-800">
                          {inst.current_version || 'v1.10.x'}
                        </span>
                        {inst.has_update && (
                          <span className="inline-flex items-center px-1.5 py-0.2 rounded bg-amber-100 text-amber-800 text-2xs font-sans">
                            <ArrowUpCircle className="w-3 h-3 mr-0.5 text-amber-600" />
                            可更新至 {inst.latest_version}
                          </span>
                        )}
                      </div>
                    </td>
                    <td className="px-4 py-3 font-mono text-2xs text-slate-600">
                      {inst.active_ports && inst.active_ports.length > 0
                        ? inst.active_ports.join(', ')
                        : '-'}
                    </td>
                    <td className="px-4 py-3 whitespace-nowrap">
                      <span className="inline-flex items-center text-emerald-700">
                        <CheckCircle2 className="w-3.5 h-3.5 mr-1 text-emerald-500" />
                        {inst.status || 'running'}
                      </span>
                    </td>
                    <td className="px-4 py-3 text-right whitespace-nowrap">
                      <button
                        onClick={() => handleUpdate(inst.instance_name)}
                        disabled={updatingNode === inst.instance_name}
                        className="inline-flex items-center px-2.5 py-1 text-xs font-medium rounded-lg text-slate-700 bg-white border border-slate-200 hover:bg-slate-50 disabled:opacity-50 transition shadow-2xs"
                      >
                        {updatingNode === inst.instance_name ? (
                          <>
                            <Loader2 className="w-3 h-3 mr-1 animate-spin text-blue-600" />
                            升级中...
                          </>
                        ) : (
                          <>
                            <Download className="w-3 h-3 mr-1 text-slate-500" />
                            一键升级
                          </>
                        )}
                      </button>
                    </td>
                  </tr>
                ))
              ) : (
                <tr>
                  <td colSpan={8} className="px-4 py-8 text-center text-slate-400">
                    暂未配置 VPS 远程 SSH 实例（可在 config.yaml 的 vps_instances 中添加）
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
};
