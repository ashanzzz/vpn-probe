import React, { useState, useEffect } from 'react';
import { 
  Server, 
  Wrench, 
  Download, 
  RotateCw, 
  CheckCircle2, 
  AlertTriangle, 
  Loader2, 
  ShieldAlert,
  ArrowUpCircle,
  Cpu,
  History,
  ShieldCheck,
  Undo2,
  HardDrive,
  X,
  Play
} from 'lucide-react';
import { api } from '../api/client';
import { SoftwareInfo, BlockedAnalysisResult, BackupSnapshot, SafeUpgradeResponse } from '../types';

interface VPSCenterProps {
  onOpenHealingWizard: (nodeName: string) => void;
}

export const VPSCenter: React.FC<VPSCenterProps> = ({ onOpenHealingWizard }) => {
  const [instances, setInstances] = useState<SoftwareInfo[]>([]);
  const [blockedAnalysis, setBlockedAnalysis] = useState<BlockedAnalysisResult[]>([]);
  const [loading, setLoading] = useState(true);
  const [updatingNode, setUpdatingNode] = useState<string | null>(null);
  const [operatingNode, setOperatingNode] = useState<string | null>(null);
  const [message, setMessage] = useState<{ type: 'success' | 'error' | 'warning'; text: string } | null>(null);

  // 安全升级冒烟结果展示卡片状态
  const [safeUpgradeResult, setSafeUpgradeResult] = useState<SafeUpgradeResponse | null>(null);

  // 配置快照模态框状态
  const [activeBackupVPS, setActiveBackupVPS] = useState<SoftwareInfo | null>(null);
  const [backups, setBackups] = useState<BackupSnapshot[]>([]);
  const [loadingBackups, setLoadingBackups] = useState(false);
  const [restoringSnapshotId, setRestoringSnapshotId] = useState<string | null>(null);
  const [creatingBackup, setCreatingBackup] = useState(false);

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

  // 1. 安全升级 (升级前快照 + 升级后真实出网冒烟测试 + 失败自动回滚)
  const handleSafeUpgrade = async (inst: SoftwareInfo) => {
    const targetKey = inst.ip || inst.instance_name;
    setUpdatingNode(targetKey);
    setMessage(null);
    setSafeUpgradeResult(null);

    try {
      const res = await api.safeUpgradeVPS(inst.ip, inst.instance_name);
      setSafeUpgradeResult(res);
      if (res.success) {
        setMessage({ 
          type: 'success', 
          text: `节点 ${inst.instance_name} (${inst.ip}) 安全升级成功！版本: ${res.previous_version} -> ${res.current_version}` 
        });
      } else {
        if (res.rolled_back) {
          setMessage({
            type: 'warning',
            text: `节点 ${inst.instance_name} 出网冒烟失败，已自动原子回滚至原版本！原因: ${res.rollback_reason || res.message}`
          });
        } else {
          setMessage({
            type: 'error',
            text: `节点 ${inst.instance_name} 升级失败: ${res.message}`
          });
        }
      }
      await fetchData();
    } catch (err: any) {
      setMessage({ type: 'error', text: `安全升级请求异常: ${err.message}` });
    } finally {
      setUpdatingNode(null);
    }
  };

  // 2. 服务生命周期操作 (restart, backup)
  const handleLifecycle = async (inst: SoftwareInfo, action: 'restart' | 'backup') => {
    const targetKey = `${inst.ip}:${action}`;
    setOperatingNode(targetKey);
    try {
      const res = await api.performVPSLifecycle(inst.ip, action);
      setMessage({ type: 'success', text: `操作成功: ${res.message || 'OK'}` });
      if (action === 'backup' && activeBackupVPS?.ip === inst.ip) {
        await loadBackups(inst.ip);
      }
      await fetchData();
    } catch (err: any) {
      setMessage({ type: 'error', text: `生命周期操作失败: ${err.message}` });
    } finally {
      setOperatingNode(null);
    }
  };

  // 3. 打开快照模态框并拉取列表
  const loadBackups = async (vpsId: string) => {
    setLoadingBackups(true);
    try {
      const res = await api.getVPSBackups(vpsId);
      setBackups(res.backups || []);
    } catch (err: any) {
      setMessage({ type: 'error', text: `读取快照列表失败: ${err.message}` });
    } finally {
      setLoadingBackups(false);
    }
  };

  const handleOpenBackups = async (inst: SoftwareInfo) => {
    setActiveBackupVPS(inst);
    await loadBackups(inst.ip);
  };

  // 4. 从历史快照还原配置
  const handleRestore = async (snapshotId: string) => {
    if (!activeBackupVPS) return;
    if (!window.confirm(`确定要从快照 ${snapshotId} 还原配置并平滑重启服务吗？`)) {
      return;
    }
    setRestoringSnapshotId(snapshotId);
    try {
      const res = await api.restoreVPSBackup(activeBackupVPS.ip, snapshotId);
      setMessage({ type: 'success', text: `快照还原成功: ${res.message || 'OK'}` });
      await loadBackups(activeBackupVPS.ip);
      await fetchData();
    } catch (err: any) {
      setMessage({ type: 'error', text: `快照还原失败: ${err.message}` });
    } finally {
      setRestoringSnapshotId(null);
    }
  };

  // 5. 立即创建当前快照
  const handleCreateSnapshot = async () => {
    if (!activeBackupVPS) return;
    setCreatingBackup(true);
    try {
      const res = await api.performVPSLifecycle(activeBackupVPS.ip, 'backup');
      setMessage({ type: 'success', text: `快照创建完成: ${res.message}` });
      await loadBackups(activeBackupVPS.ip);
    } catch (err: any) {
      setMessage({ type: 'error', text: `创建快照失败: ${err.message}` });
    } finally {
      setCreatingBackup(false);
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
            受控 SSH 执行引擎 · 官方 GitHub Upstream 版本自动感知 · 工业级安全升级（自动快照与冒烟自动回滚）· 深度墙况自愈
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
              : message.type === 'warning'
              ? 'bg-amber-50 text-amber-800 border-amber-200'
              : 'bg-red-50 text-red-800 border-red-200'
          }`}
        >
          <span>{message.text}</span>
          <button onClick={() => setMessage(null)} className="text-xs underline ml-2">
            关闭
          </button>
        </div>
      )}

      {/* 0. 安全升级出网冒烟测试结果卡片 (Safe Upgrade Smoke Result Drawer) */}
      {safeUpgradeResult && (
        <div className={`p-4 rounded-xl border ${safeUpgradeResult.success ? 'bg-emerald-50/60 border-emerald-200' : safeUpgradeResult.rolled_back ? 'bg-amber-50/80 border-amber-300' : 'bg-red-50/60 border-red-200'}`}>
          <div className="flex items-center justify-between pb-2 border-b border-slate-200/60">
            <div className="flex items-center space-x-2">
              {safeUpgradeResult.success ? (
                <ShieldCheck className="w-5 h-5 text-emerald-600" />
              ) : safeUpgradeResult.rolled_back ? (
                <Undo2 className="w-5 h-5 text-amber-600" />
              ) : (
                <AlertTriangle className="w-5 h-5 text-red-600" />
              )}
              <h3 className="text-xs font-bold uppercase tracking-wider text-slate-900">
                {safeUpgradeResult.success 
                  ? '安全升级与出网冒烟测试通过 (Safe Upgrade Succeeded)' 
                  : safeUpgradeResult.rolled_back 
                  ? '冒烟测试未通过 · 生产配置已触发原子安全回滚 (Auto Rollback Succeeded)' 
                  : '升级与冒烟执行异常'}
              </h3>
            </div>
            <button onClick={() => setSafeUpgradeResult(null)} className="text-slate-400 hover:text-slate-600">
              <X className="w-4 h-4" />
            </button>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-4 gap-3 mt-3 text-xs">
            <div className="bg-white/80 p-2.5 rounded-lg border border-slate-200/60">
              <span className="text-slate-500 block text-2xs">目标实例与软件</span>
              <span className="font-mono font-semibold text-slate-800">{safeUpgradeResult.ip} ({safeUpgradeResult.software_type})</span>
            </div>
            <div className="bg-white/80 p-2.5 rounded-lg border border-slate-200/60">
              <span className="text-slate-500 block text-2xs">版本变更状态</span>
              <span className="font-mono text-slate-800">
                {safeUpgradeResult.previous_version} ➔ <span className="font-bold">{safeUpgradeResult.current_version}</span>
              </span>
            </div>
            <div className="bg-white/80 p-2.5 rounded-lg border border-slate-200/60">
              <span className="text-slate-500 block text-2xs">预置原子备份</span>
              <span className="font-mono text-slate-700 truncate block" title={safeUpgradeResult.backup_created || '无'}>
                {safeUpgradeResult.backup_created || '未生成备份'}
              </span>
            </div>
            <div className="bg-white/80 p-2.5 rounded-lg border border-slate-200/60">
              <span className="text-slate-500 block text-2xs">出网冒烟测试 (Smoke Test)</span>
              {safeUpgradeResult.smoke_test ? (
                safeUpgradeResult.smoke_test.success ? (
                  <span className="text-emerald-700 font-semibold font-mono flex items-center">
                    <CheckCircle2 className="w-3.5 h-3.5 mr-1" />
                    🟢 通畅 ({safeUpgradeResult.smoke_test.latency_ms}ms, 出口: {safeUpgradeResult.smoke_test.exit_ip || '海外'})
                  </span>
                ) : (
                  <span className="text-red-700 font-semibold flex items-center">
                    <AlertTriangle className="w-3.5 h-3.5 mr-1" />
                    ❌ 握手失败 ({safeUpgradeResult.smoke_test.error || '超时'})
                  </span>
                )
              ) : (
                <span className="text-slate-400">无节点绑定关联</span>
              )}
            </div>
          </div>

          {safeUpgradeResult.rollback_reason && (
            <div className="mt-2.5 text-xs text-amber-900 bg-amber-100/80 p-2 rounded-lg border border-amber-200">
              <span className="font-semibold">回滚详情：</span>{safeUpgradeResult.rollback_reason}
            </div>
          )}
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
                <th scope="col" className="px-4 py-2.5 text-right">生命周期与安全升级</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-200 bg-white">
              {instances.length > 0 ? (
                instances.map((inst, idx) => {
                  const targetKey = inst.ip || inst.instance_name;
                  const isUpdating = updatingNode === targetKey;
                  const isRestarting = operatingNode === `${inst.ip}:restart`;
                  return (
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
                        <div className="inline-flex items-center space-x-2">
                          {/* 重启服务 */}
                          <button
                            onClick={() => handleLifecycle(inst, 'restart')}
                            disabled={isRestarting || isUpdating}
                            title="平滑重启该 VPS 上的代理服务"
                            className="inline-flex items-center px-2 py-1 text-2xs font-medium rounded text-slate-600 bg-white border border-slate-200 hover:bg-slate-50 disabled:opacity-50 transition shadow-2xs"
                          >
                            <RotateCw className={`w-3 h-3 mr-1 ${isRestarting ? 'animate-spin text-blue-600' : 'text-slate-500'}`} />
                            重启
                          </button>

                          {/* 配置快照历史与还原 */}
                          <button
                            onClick={() => handleOpenBackups(inst)}
                            title="查看历史配置快照或一键原子回滚"
                            className="inline-flex items-center px-2 py-1 text-2xs font-medium rounded text-slate-600 bg-white border border-slate-200 hover:bg-slate-50 transition shadow-2xs"
                          >
                            <History className="w-3 h-3 mr-1 text-slate-500" />
                            快照
                          </button>

                          {/* 安全升级（带快照与冒烟自动回滚） */}
                          <button
                            onClick={() => handleSafeUpgrade(inst)}
                            disabled={isUpdating}
                            className={`inline-flex items-center px-2.5 py-1 text-xs font-semibold rounded-lg transition shadow-2xs ${
                              inst.has_update
                                ? 'bg-blue-600 hover:bg-blue-700 text-white'
                                : 'bg-white border border-slate-200 hover:bg-slate-50 text-slate-700'
                            }`}
                          >
                            {isUpdating ? (
                              <>
                                <Loader2 className="w-3 h-3 mr-1 animate-spin text-blue-600" />
                                安全升级中...
                              </>
                            ) : (
                              <>
                                <ShieldCheck className="w-3 h-3 mr-1 text-emerald-500" />
                                安全升级
                              </>
                            )}
                          </button>
                        </div>
                      </td>
                    </tr>
                  );
                })
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

      {/* 3. 配置快照管理模态框 (Snapshot Management Modal) */}
      {activeBackupVPS && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/40 backdrop-blur-xs p-4">
          <div className="bg-white rounded-xl border border-slate-200 shadow-xl max-w-2xl w-full overflow-hidden flex flex-col max-h-[85vh]">
            <div className="px-5 py-4 border-b border-slate-200 bg-slate-50 flex items-center justify-between">
              <div className="flex items-center space-x-2">
                <HardDrive className="w-4 h-4 text-blue-600" />
                <h3 className="text-sm font-bold text-slate-900">
                  配置快照与原子还原 · {activeBackupVPS.instance_name || activeBackupVPS.ip}
                </h3>
              </div>
              <button
                onClick={() => setActiveBackupVPS(null)}
                className="text-slate-400 hover:text-slate-600 p-1 rounded-md"
              >
                <X className="w-4 h-4" />
              </button>
            </div>

            <div className="p-5 overflow-y-auto space-y-4 flex-1">
              <div className="flex items-center justify-between">
                <p className="text-xs text-slate-500">
                  当前托管节点 IP: <span className="font-mono font-semibold text-slate-800">{activeBackupVPS.ip}</span>（最多保留最近 3 份配置快照）
                </p>
                <button
                  onClick={handleCreateSnapshot}
                  disabled={creatingBackup}
                  className="inline-flex items-center px-2.5 py-1 text-xs font-semibold rounded bg-blue-600 hover:bg-blue-700 text-white shadow-2xs transition disabled:opacity-50"
                >
                  {creatingBackup ? (
                    <>
                      <Loader2 className="w-3.5 h-3.5 mr-1 animate-spin" />
                      生成快照中...
                    </>
                  ) : (
                    <>
                      <HardDrive className="w-3.5 h-3.5 mr-1" />
                      即刻创建快照
                    </>
                  )}
                </button>
              </div>

              {loadingBackups ? (
                <div className="py-12 text-center text-xs text-slate-400 flex flex-col items-center justify-center">
                  <Loader2 className="w-5 h-5 animate-spin text-blue-600 mb-2" />
                  正在读取远程 VPS 配置快照清单...
                </div>
              ) : backups.length > 0 ? (
                <div className="border border-slate-200 rounded-lg overflow-hidden divide-y divide-slate-200">
                  {backups.map((snap) => (
                    <div key={snap.id} className="p-3 bg-white hover:bg-slate-50 flex items-center justify-between text-xs">
                      <div className="space-y-0.5">
                        <div className="font-mono font-semibold text-slate-800 flex items-center">
                          {snap.file_name}
                        </div>
                        <div className="text-slate-400 font-mono text-2xs">
                          创建时间: {snap.created_at} · 文件大小: {snap.size_bytes} 字节
                        </div>
                      </div>

                      <button
                        onClick={() => handleRestore(snap.id)}
                        disabled={restoringSnapshotId === snap.id}
                        className="inline-flex items-center px-2.5 py-1 text-xs font-medium rounded text-amber-800 bg-amber-50 border border-amber-200 hover:bg-amber-100 disabled:opacity-50 transition shadow-2xs"
                      >
                        {restoringSnapshotId === snap.id ? (
                          <>
                            <Loader2 className="w-3 h-3 mr-1 animate-spin text-amber-600" />
                            还原中...
                          </>
                        ) : (
                          <>
                            <Undo2 className="w-3 h-3 mr-1 text-amber-600" />
                            一键还原
                          </>
                        )}
                      </button>
                    </div>
                  ))}
                </div>
              ) : (
                <div className="py-10 text-center text-xs text-slate-400 border border-dashed border-slate-200 rounded-lg">
                  当前节点暂无历史配置快照。可点击右上角「即刻创建快照」或在安全升级前由系统自动备份。
                </div>
              )}
            </div>

            <div className="px-5 py-3 border-t border-slate-200 bg-slate-50 flex justify-end">
              <button
                onClick={() => setActiveBackupVPS(null)}
                className="px-3 py-1.5 text-xs font-medium rounded-lg text-slate-700 bg-white border border-slate-200 hover:bg-slate-100 transition shadow-2xs"
              >
                关闭
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};