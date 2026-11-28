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
import { ConfirmActionButton } from './ConfirmActionButton';
import { useFeedback } from '../context/FeedbackContext';
import {
  SoftwareInfo,
  BlockedAnalysisResult,
  BackupSnapshot,
  SafeUpgradeResponse,
  DeployVLESSResponse,
} from '../types';

interface VPSCenterProps {
  onOpenHealingWizard: (nodeName: string) => void;
}

export const VPSCenter: React.FC<VPSCenterProps> = ({ onOpenHealingWizard }) => {
  const { pushActivity } = useFeedback();
  const [instances, setInstances] = useState<SoftwareInfo[]>([]);
  const [blockedAnalysis, setBlockedAnalysis] = useState<BlockedAnalysisResult[]>([]);
  const [loading, setLoading] = useState(true);
  const [updatingNode, setUpdatingNode] = useState<string | null>(null);
  const [operatingNode, setOperatingNode] = useState<string | null>(null);
  const [message, setMessage] = useState<{ type: 'success' | 'error' | 'warning'; text: string } | null>(null);

  // 安全升级冒烟结果展示卡片状态
  const [safeUpgradeResult, setSafeUpgradeResult] = useState<SafeUpgradeResponse | null>(null);

  // 一键部署 VLESS 线路机模态框与结果状态
  const [deployTarget, setDeployTarget] = useState<SoftwareInfo | null>(null);
  const [deployNodeName, setDeployNodeName] = useState('');
  const [deployPort, setDeployPort] = useState('');
  const [deploySNI, setDeploySNI] = useState('aws.amazon.com');
  const [deploying, setDeploying] = useState(false);
  const [deployResult, setDeployResult] = useState<DeployVLESSResponse | null>(null);

  // 配置快照模态框状态
  const [activeBackupVPS, setActiveBackupVPS] = useState<SoftwareInfo | null>(null);
  const [activeBackupTarget, setActiveBackupTarget] = useState<{ software?: string; container_name?: string }>({});
  const [backups, setBackups] = useState<BackupSnapshot[]>([]);
  const [loadingBackups, setLoadingBackups] = useState(false);
  const [restoringSnapshotId, setRestoringSnapshotId] = useState<string | null>(null);
  const [creatingBackup, setCreatingBackup] = useState(false);

  const fetchData = async (forceRefresh: boolean = false) => {
    setLoading(true);
    try {
      const [vpsData, analysisData] = await Promise.all([
        api.getVPSManagement(forceRefresh).catch(() => ({ total: 0, instances: [] })),
        api.getBlockedAnalysis().catch(() => ({ analysis: [], results: [] })),
      ]);
      setInstances(vpsData.instances || []);
      setBlockedAnalysis(analysisData.analysis || analysisData.results || []);
    } catch (err: any) {
      setMessage({ type: 'error', text: err.message || '获取 VPS 运维数据失败' });
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchData();
  }, []);

  const appTargetKey = (inst: SoftwareInfo, software?: string, containerName?: string) => {
    const hostKey = inst.ip || inst.instance_name;
    if (!software) return hostKey;
    return `${hostKey}:${software}:${containerName || ''}`;
  };

  // 1. 安全升级 (升级前快照 + 升级后真实出网冒烟测试 + 失败自动回滚)
  const handleSafeUpgrade = async (inst: SoftwareInfo, software?: string, containerName?: string) => {
    const targetKey = appTargetKey(inst, software, containerName);
    const label = software ? `${inst.instance_name} [${software}${containerName ? `/${containerName}` : ''}]` : inst.instance_name;
    setUpdatingNode(targetKey);
    setMessage(null);
    setSafeUpgradeResult(null);

    try {
      const res = await api.safeUpgradeVPS(inst.ip, inst.instance_name, software, containerName);
      setSafeUpgradeResult(res);
      if (res.success) {
        setMessage({
          type: 'success',
          text: `节点 ${label} (${inst.ip}) 安全升级成功！版本: ${res.previous_version} -> ${res.current_version}`,
        });
      } else {
        if (res.rolled_back) {
          setMessage({
            type: 'warning',
            text: `节点 ${label} 冒烟测试失败，已自动原子回滚至原版本！原因: ${res.rollback_reason || res.message}`,
          });
        } else {
          setMessage({
            type: 'error',
            text: `节点 ${label} 升级失败: ${res.message}`,
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
  const handleLifecycle = async (
    inst: SoftwareInfo,
    action: 'restart' | 'backup',
    software?: string,
    containerName?: string,
  ) => {
    const targetKey = `${appTargetKey(inst, software, containerName)}:${action}`;
    setOperatingNode(targetKey);
    try {
      const res = await api.performVPSLifecycle(inst.ip, action, software, containerName);
      setMessage({ type: 'success', text: `操作成功: ${res.message || 'OK'}` });
      if (action === 'backup' && activeBackupVPS?.ip === inst.ip) {
        await loadBackups(inst.ip, software, containerName);
      }
      await fetchData();
    } catch (err: any) {
      setMessage({ type: 'error', text: `生命周期操作失败: ${err.message}` });
    } finally {
      setOperatingNode(null);
    }
  };

  // 3. 打开快照模态框并拉取列表
  const loadBackups = async (vpsId: string, software?: string, containerName?: string) => {
    setLoadingBackups(true);
    try {
      const res = await api.getVPSBackups(vpsId, software, containerName);
      setBackups(res.backups || []);
    } catch (err: any) {
      setMessage({ type: 'error', text: `读取快照列表失败: ${err.message}` });
    } finally {
      setLoadingBackups(false);
    }
  };

  const handleOpenBackups = async (inst: SoftwareInfo, software?: string, containerName?: string) => {
    setActiveBackupVPS(inst);
    setActiveBackupTarget({ software, container_name: containerName });
    await loadBackups(inst.ip, software, containerName);
  };

  // 4. 从历史快照还原配置
  const handleRestore = async (snapshotId: string) => {
    if (!activeBackupVPS) return;
    setRestoringSnapshotId(snapshotId);
    try {
      const res = await api.restoreVPSBackup(
        activeBackupVPS.ip,
        snapshotId,
        activeBackupTarget.software,
        activeBackupTarget.container_name,
      );
      setMessage({ type: 'success', text: `快照还原成功: ${res.message || 'OK'}` });
      pushActivity({ level: 'success', source: 'VPS快照', title: `${activeBackupVPS.ip} 快照还原成功`, detail: snapshotId });
      await loadBackups(activeBackupVPS.ip, activeBackupTarget.software, activeBackupTarget.container_name);
      await fetchData();
    } catch (err: any) {
      setMessage({ type: 'error', text: `快照还原失败: ${err.message}` });
      pushActivity({ level: 'error', source: 'VPS快照', title: `${activeBackupVPS.ip} 快照还原失败`, detail: err.message });
    } finally {
      setRestoringSnapshotId(null);
    }
  };

  // 5. 立即创建当前快照
  const handleCreateSnapshot = async () => {
    if (!activeBackupVPS) return;
    setCreatingBackup(true);
    try {
      const res = await api.performVPSLifecycle(
        activeBackupVPS.ip,
        'backup',
        activeBackupTarget.software,
        activeBackupTarget.container_name,
      );
      setMessage({ type: 'success', text: `快照创建完成: ${res.message}` });
      await loadBackups(activeBackupVPS.ip, activeBackupTarget.software, activeBackupTarget.container_name);
    } catch (err: any) {
      setMessage({ type: 'error', text: `创建快照失败: ${err.message}` });
    } finally {
      setCreatingBackup(false);
    }
  };

  // 6. 打开部署 VLESS 线路机弹窗
  const suggestSafeNodeName = (inst: SoftwareInfo): string => {
    const raw = (inst.instance_name || inst.instance_id || 'NODE')
      .replace(/0\.1折|全球家宽|固定出口|lazycat/gi, '')
      .replace(/[^a-zA-Z0-9\u4e00-\u9fa5]+/g, '-')
      .replace(/^-+|-+$/g, '')
      .slice(0, 18) || inst.instance_id;
    const upper = (inst.instance_name || '').toUpperCase();
    let cc = 'VPS';
    if (upper.includes('HK') || upper.includes('香港')) cc = 'HK';
    else if (upper.includes('JP') || upper.includes('日本') || upper.includes('东京')) cc = 'JP';
    else if (upper.includes('US') || upper.includes('美') || upper.includes('LAX')) cc = 'US';
    else if (upper.includes('SG') || upper.includes('新')) cc = 'SG';
    else if (upper.includes('TW') || upper.includes('台')) cc = 'TW';
    return `${cc}-${raw}${inst.is_nat ? '-NAT' : ''}`;
  };

  const handleOpenDeploy = (inst: SoftwareInfo) => {
    setDeployTarget(inst);
    setDeployNodeName(suggestSafeNodeName(inst));
    setDeployPort('');
    setDeploySNI('aws.amazon.com');
  };

  const handleConfirmDeploy = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!deployTarget) return;
    const trimmedName = deployNodeName.trim();
    const lower = trimmedName.toLowerCase();
    if (!trimmedName) {
      setMessage({ type: 'error', text: '请填写线路机节点名称' });
      return;
    }
    if (['lz-', '家宽', '动态', 'residential', 'resi', 'global'].some((kw) => lower.includes(kw))) {
      setMessage({ type: 'error', text: '线路机节点名禁止包含 LZ-/家宽/动态/global 等家宽关键字' });
      return;
    }

    const parsedPort = deployPort.trim() ? Number(deployPort.trim()) : 0;
    if (deployPort.trim() && (!Number.isInteger(parsedPort) || parsedPort <= 0 || parsedPort > 65535)) {
      setMessage({ type: 'error', text: '私网端口必须在 1-65535 之间，或留空自动分配' });
      return;
    }

    setDeploying(true);
    setMessage(null);
    setDeployResult(null);
    try {
      const res = await api.deployVLESS({
        vps_id: deployTarget.instance_id || deployTarget.ip,
        node_name: trimmedName,
        listen_port: parsedPort,
        sni: deploySNI.trim() || 'aws.amazon.com',
      });
      setDeployResult(res);
      setMessage({ type: 'success', text: res.message || `已部署并接入线路机 ${res.node_name}` });
      setDeployTarget(null);
      await fetchData(true);
    } catch (err: any) {
      setMessage({ type: 'error', text: `部署 VLESS 失败: ${err.message}` });
    } finally {
      setDeploying(false);
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
          onClick={() => fetchData(true)}
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

      {/* 0.1 一键部署 VLESS 线路机结果卡片 */}
      {deployResult && (
        <div className={`p-4 rounded-xl border ${deployResult.success ? 'bg-blue-50/60 border-blue-200' : 'bg-red-50/60 border-red-200'}`}>
          <div className="flex items-center justify-between pb-2 border-b border-slate-200/60">
            <div className="flex items-center space-x-2">
              <CheckCircle2 className="w-5 h-5 text-blue-600" />
              <h3 className="text-xs font-bold uppercase tracking-wider text-slate-900">
                VLESS Reality 线路机部署与入库结果 · {deployResult.node_name}
              </h3>
            </div>
            <button onClick={() => setDeployResult(null)} className="text-slate-400 hover:text-slate-600">
              <X className="w-4 h-4" />
            </button>
          </div>
          <div className="grid grid-cols-1 md:grid-cols-4 gap-3 mt-3 text-xs">
            <div className="bg-white/80 p-2.5 rounded-lg border border-slate-200/60">
              <span className="text-slate-500 block text-2xs">公网入口与模式</span>
              <span className="font-mono font-semibold text-slate-800">
                {deployResult.ip}:{deployResult.public_port} ({deployResult.deploy_mode})
              </span>
            </div>
            <div className="bg-white/80 p-2.5 rounded-lg border border-slate-200/60">
              <span className="text-slate-500 block text-2xs">NAT 端口映射</span>
              <span className="font-mono text-slate-800">
                私网 {deployResult.listen_port} ➔ 公网 {deployResult.public_port}
                {deployResult.retried_nat ? ' (已自动重试换端口)' : ''}
              </span>
            </div>
            <div className="bg-white/80 p-2.5 rounded-lg border border-slate-200/60">
              <span className="text-slate-500 block text-2xs">真机冒烟测试</span>
              <span className="font-mono font-semibold text-emerald-700">
                {deployResult.smoke_test?.success
                  ? `通畅 (${deployResult.smoke_test.latency_ms}ms · ${deployResult.smoke_test.exit_ip || 'OK'})`
                  : deployResult.smoke_test?.error || '已跳过'}
              </span>
            </div>
            <div className="bg-white/80 p-2.5 rounded-lg border border-slate-200/60">
              <span className="text-slate-500 block text-2xs">自动入库状态</span>
              <span className="font-mono text-slate-700">
                配置:{deployResult.config_synced ? '✅' : '—'} · 监控:{deployResult.monitor_synced ? '✅' : '—'} · 订阅:{deployResult.substore_synced ? '✅' : '—'}
              </span>
            </div>
          </div>
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
                  const isBlocked = item.fault_type === 'PORT_BLOCKED' || item.fault_type === 'port_blocked';
                  const isServerBlocked = item.fault_type === 'SERVER_BLOCKED' || item.fault_type === 'server_blocked' || item.fault_type === 'ip_blocked';
                  const isProtocolMismatch = item.fault_type === 'PROTOCOL_MISMATCH' || item.fault_type === 'protocol_mismatch';
                  const isCongestion = item.fault_type === 'NETWORK_CONGESTION' || item.fault_type === 'network_congestion';
                  const isDirectBlocked = item.fault_type === 'RESIDENTIAL_DIRECT_BLOCKED' || item.fault_type === 'residential_direct_blocked';
                  const isTunnelOffline = item.fault_type === 'RESIDENTIAL_TUNNEL_OFFLINE' || item.fault_type === 'residential_tunnel_offline';
                  const isRelayDown = item.fault_type === 'TRANSIT_RELAY_DOWN' || item.fault_type === 'transit_relay_down';
                  const isDown = item.fault_type === 'MACHINE_DOWN' || item.fault_type === 'machine_offline';
                  const isHealthy = item.fault_type === 'HEALTHY' || item.fault_type === 'none';
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
                        ) : isServerBlocked ? (
                          <span className="px-2 py-0.5 rounded text-2xs font-semibold bg-red-100 text-red-800 border border-red-200">
                            SERVER_BLOCKED (整机被墙)
                          </span>
                        ) : isProtocolMismatch ? (
                          <span className="px-2 py-0.5 rounded text-2xs font-semibold bg-amber-100 text-amber-800 border border-amber-200">
                            PROTOCOL_MISMATCH (配置错误)
                          </span>
                        ) : isCongestion ? (
                          <span className="px-2 py-0.5 rounded text-2xs font-semibold bg-amber-100 text-amber-800 border border-amber-200">
                            CONGESTION (网络拥塞)
                          </span>
                        ) : isDirectBlocked ? (
                          <span className="px-2 py-0.5 rounded text-2xs font-semibold bg-blue-100 text-blue-800 border border-blue-200">
                            DIRECT_BLOCKED (需链式中继)
                          </span>
                        ) : isTunnelOffline ? (
                          <span className="px-2 py-0.5 rounded text-2xs font-semibold bg-orange-100 text-orange-800 border border-orange-200">
                            TUNNEL_OFFLINE (家宽隧道掉线)
                          </span>
                        ) : isRelayDown ? (
                          <span className="px-2 py-0.5 rounded text-2xs font-semibold bg-purple-100 text-purple-800 border border-purple-200">
                            RELAY_DOWN (中转宿主宕机)
                          </span>
                        ) : isDown ? (
                          <span className="px-2 py-0.5 rounded text-2xs font-semibold bg-slate-100 text-slate-700 border border-slate-200">
                            MACHINE_DOWN (服务关机)
                          </span>
                        ) : isHealthy ? (
                          <span className="px-2 py-0.5 rounded text-2xs font-semibold bg-emerald-100 text-emerald-800 border border-emerald-200">
                            HEALTHY (正常)
                          </span>
                        ) : (
                          <span className="px-2 py-0.5 rounded text-2xs font-semibold bg-slate-100 text-slate-700 border border-slate-200">
                            UNKNOWN (未知异常)
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
                  const apps = inst.managed_apps && inst.managed_apps.length > 0 ? inst.managed_apps : [];
                  const primaryApp = apps.length === 1 ? apps[0] : undefined;
                  const targetKey = appTargetKey(inst, primaryApp?.app_type, primaryApp?.container_name);
                  const isUpdating = updatingNode === targetKey;
                  const isRestarting = operatingNode === `${targetKey}:restart`;
                  const isResidential =
                    inst.category === '全球家宽' ||
                    (inst.provider_id === 'lazycat' && inst.category !== '固定出口' && Boolean(inst.category)) ||
                    (inst.provider_id === 'lazycat' && /全球家宽|家宽|大杯|residential/i.test(inst.instance_name || ''));
                  const isLazyCatTransit = inst.provider_id === 'lazycat' && !isResidential;
                  const isUninstalled = (inst.software_type === 'none' || inst.status === 'not_installed') && apps.length === 0;
                  return (
                    <React.Fragment key={idx}>
                      <tr className="hover:bg-slate-50/80 transition">
                        <td className="px-4 py-3 font-semibold text-slate-900">
                          <div className="flex items-center gap-1.5">
                            <span>{inst.instance_name || inst.instance_id}</span>
                            {inst.instance_id && (
                              <span className="text-2xs font-mono font-normal text-slate-400">#{inst.instance_id}</span>
                            )}
                          </div>
                          <div className="flex flex-wrap items-center gap-1 mt-1">
                            {inst.is_nat && (
                              <span className="px-1.5 py-0.5 rounded bg-indigo-50 text-indigo-700 border border-indigo-200 text-2xs font-medium">
                                NAT{inst.port_limit ? ` ${inst.port_count || 0}/${inst.port_limit}` : ''}
                              </span>
                            )}
                            {inst.exec_mode === 'hub' && (
                              <span className="px-1.5 py-0.5 rounded bg-slate-100 text-slate-600 border border-slate-200 text-2xs font-mono">
                                Hub通道
                              </span>
                            )}
                            {isLazyCatTransit && (
                              <span className="px-1.5 py-0.5 rounded bg-emerald-50 text-emerald-700 border border-emerald-200 text-2xs font-medium">
                                固定出口·线路机
                              </span>
                            )}
                            {isResidential && (
                              <span className="px-1.5 py-0.5 rounded bg-amber-50 text-amber-800 border border-amber-200 text-2xs font-medium">
                                全球家宽·禁止部署线路机
                              </span>
                            )}
                            {inst.category && !isLazyCatTransit && !isResidential && (
                              <span className="px-1.5 py-0.5 rounded bg-slate-50 text-slate-600 border border-slate-200 text-2xs">
                                {inst.category}
                              </span>
                            )}
                            {apps.length > 1 && (
                              <span className="px-1.5 py-0.5 rounded bg-blue-50 text-blue-700 border border-blue-200 text-2xs font-medium">
                                共存 {apps.length} 组件
                              </span>
                            )}
                          </div>
                        </td>
                        <td className="px-4 py-3 font-mono text-slate-700">
                          {inst.ip}
                        </td>
                        <td className="px-4 py-3 font-mono">
                          <span className={`px-2 py-0.5 rounded font-semibold border text-2xs uppercase ${
                            isUninstalled
                              ? 'bg-slate-100 text-slate-500 border-slate-200'
                              : 'bg-blue-50 text-blue-700 border-blue-200'
                          }`}>
                            {inst.software_type}
                          </span>
                          {inst.container_name && (
                            <div className="text-2xs text-slate-400 mt-0.5">{inst.container_name}</div>
                          )}
                        </td>
                        <td className="px-4 py-3 text-slate-600">
                          {inst.deploy_mode || 'none'}
                        </td>
                        <td className="px-4 py-3 whitespace-nowrap">
                          <div className="flex items-center space-x-1.5 font-mono">
                            <span className="font-semibold text-slate-800">
                              {inst.current_version || '-'}
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
                          {inst.status === 'running' ? (
                            <span className="inline-flex items-center text-emerald-700">
                              <CheckCircle2 className="w-3.5 h-3.5 mr-1 text-emerald-500" />
                              running
                            </span>
                          ) : inst.status === 'unreachable' ? (
                            <div className="space-y-0.5 whitespace-normal max-w-xs">
                              <span className="inline-flex items-center text-red-700" title={inst.error_message}>
                                <AlertTriangle className="w-3.5 h-3.5 mr-1 text-red-500 shrink-0" />
                                unreachable
                              </span>
                              {inst.error_message && (
                                <div className="text-2xs text-red-600/90 leading-snug break-all">
                                  {inst.error_message}
                                </div>
                              )}
                            </div>
                          ) : (
                            <span className="inline-flex items-center text-amber-700">
                              <AlertTriangle className="w-3.5 h-3.5 mr-1 text-amber-500" />
                              {inst.status || 'stopped'}
                            </span>
                          )}
                        </td>
                        <td className="px-4 py-3 text-right whitespace-nowrap">
                          <div className="inline-flex items-center space-x-2">
                            {!isResidential && (
                              <button
                                onClick={() => handleOpenDeploy(inst)}
                                disabled={deploying}
                                title="一键部署 VLESS+Reality 并接入线路机监控与 Sub-Store"
                                className={`inline-flex items-center px-2.5 py-1 text-2xs font-semibold rounded transition shadow-2xs ${
                                  isUninstalled
                                    ? 'bg-blue-600 hover:bg-blue-700 text-white'
                                    : 'bg-white border border-blue-200 text-blue-700 hover:bg-blue-50'
                                }`}
                              >
                                <Play className="w-3 h-3 mr-1" />
                                {isUninstalled ? '部署 VLESS 线路机' : '重装/接入 VLESS'}
                              </button>
                            )}

                            {isResidential && (
                              <span
                                className="inline-flex items-center px-2 py-1 text-2xs rounded bg-amber-50 text-amber-800 border border-amber-200"
                                title="全球家宽网关仅通过 egress-subscriptions 提供家宽出口，禁止部署/运维为线路机"
                              >
                                家宽只读（非线路机）
                              </span>
                            )}

                            {!isResidential && !isUninstalled && apps.length <= 1 && (
                              <>
                                {/* 重启服务 */}
                                <button
                                  onClick={() => handleLifecycle(inst, 'restart', primaryApp?.app_type, primaryApp?.container_name)}
                                  disabled={isRestarting || isUpdating}
                                  title="平滑重启该 VPS 上的代理服务"
                                  className="inline-flex items-center px-2 py-1 text-2xs font-medium rounded text-slate-600 bg-white border border-slate-200 hover:bg-slate-50 disabled:opacity-50 transition shadow-2xs"
                                >
                                  <RotateCw className={`w-3 h-3 mr-1 ${isRestarting ? 'animate-spin text-blue-600' : 'text-slate-500'}`} />
                                  重启
                                </button>

                                {/* 配置快照历史与还原 */}
                                <button
                                  onClick={() => handleOpenBackups(inst, primaryApp?.app_type, primaryApp?.container_name)}
                                  title="查看历史配置快照或一键原子回滚"
                                  className="inline-flex items-center px-2 py-1 text-2xs font-medium rounded text-slate-600 bg-white border border-slate-200 hover:bg-slate-50 transition shadow-2xs"
                                >
                                  <History className="w-3 h-3 mr-1 text-slate-500" />
                                  快照
                                </button>

                                {/* 安全升级（带快照与冒烟自动回滚） */}
                                <button
                                  onClick={() => handleSafeUpgrade(inst, primaryApp?.app_type, primaryApp?.container_name)}
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
                              </>
                            )}
                          </div>
                        </td>
                      </tr>
                      {apps.length > 1 && apps.map((app, appIdx) => {
                        const appKey = appTargetKey(inst, app.app_type, app.container_name);
                        const appUpdating = updatingNode === appKey;
                        const appRestarting = operatingNode === `${appKey}:restart`;
                        const isSubStore = app.app_type === 'sub-store';
                        return (
                          <tr key={`${idx}-app-${appIdx}`} className="bg-slate-50/70 hover:bg-slate-100/70 transition text-2xs">
                            <td className="pl-8 pr-4 py-2 text-slate-500 font-mono">
                              └─ 组件 #{appIdx + 1}
                            </td>
                            <td className="px-4 py-2 font-mono text-slate-400">
                              {app.container_name || 'native'}
                            </td>
                            <td className="px-4 py-2 font-mono">
                              <span className={`px-1.5 py-0.5 rounded font-semibold border uppercase ${
                                isSubStore
                                  ? 'bg-purple-50 text-purple-700 border-purple-200'
                                  : 'bg-blue-50 text-blue-700 border-blue-200'
                              }`}>
                                {app.app_type}
                              </span>
                            </td>
                            <td className="px-4 py-2 text-slate-500">
                              {app.deploy_mode}
                            </td>
                            <td className="px-4 py-2 whitespace-nowrap font-mono">
                              <span className="font-semibold text-slate-700">{app.current_version || '-'}</span>
                              {app.has_update && (
                                <span className="ml-1.5 inline-flex items-center px-1.5 py-0.2 rounded bg-amber-100 text-amber-800 font-sans">
                                  <ArrowUpCircle className="w-3 h-3 mr-0.5 text-amber-600" />
                                  {app.latest_version}
                                </span>
                              )}
                            </td>
                            <td className="px-4 py-2 font-mono text-slate-400">
                              {app.config_path ? app.config_path : '-'}
                            </td>
                            <td className="px-4 py-2 whitespace-nowrap">
                              <span className={app.status === 'running' ? 'text-emerald-700' : 'text-amber-700'}>
                                {app.status || 'unknown'}
                              </span>
                            </td>
                            <td className="px-4 py-2 text-right whitespace-nowrap">
                              <div className="inline-flex items-center space-x-1.5">
                                <button
                                  onClick={() => handleLifecycle(inst, 'restart', app.app_type, app.container_name)}
                                  disabled={appRestarting || appUpdating}
                                  className="inline-flex items-center px-2 py-0.5 rounded text-slate-600 bg-white border border-slate-200 hover:bg-slate-50 disabled:opacity-50"
                                >
                                  <RotateCw className={`w-3 h-3 mr-1 ${appRestarting ? 'animate-spin text-blue-600' : 'text-slate-500'}`} />
                                  重启
                                </button>
                                <button
                                  onClick={() => handleOpenBackups(inst, app.app_type, app.container_name)}
                                  className="inline-flex items-center px-2 py-0.5 rounded text-slate-600 bg-white border border-slate-200 hover:bg-slate-50"
                                >
                                  <History className="w-3 h-3 mr-1 text-slate-500" />
                                  快照
                                </button>
                                <button
                                  onClick={() => handleSafeUpgrade(inst, app.app_type, app.container_name)}
                                  disabled={appUpdating}
                                  className={`inline-flex items-center px-2 py-0.5 font-semibold rounded transition ${
                                    app.has_update
                                      ? 'bg-blue-600 hover:bg-blue-700 text-white'
                                      : 'bg-white border border-slate-200 hover:bg-slate-50 text-slate-700'
                                  }`}
                                >
                                  {appUpdating ? (
                                    <>
                                      <Loader2 className="w-3 h-3 mr-1 animate-spin text-blue-600" />
                                      升级中
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
                      })}
                    </React.Fragment>
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
                  {activeBackupTarget.software ? ` · ${activeBackupTarget.software}${activeBackupTarget.container_name ? ` (${activeBackupTarget.container_name})` : ''}` : ''}
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

                      <ConfirmActionButton
                        onConfirm={() => handleRestore(snap.id)}
                        confirmLabel="确定还原?"
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
                      </ConfirmActionButton>
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
      {/* 4. 一键部署 VLESS 线路机模态框 */}
      {deployTarget && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/40 backdrop-blur-xs p-4">
          <div className="bg-white rounded-xl border border-slate-200 shadow-xl max-w-lg w-full overflow-hidden">
            <div className="px-5 py-4 border-b border-slate-200 bg-slate-50 flex items-center justify-between">
              <div className="flex items-center space-x-2">
                <Download className="w-4 h-4 text-blue-600" />
                <h3 className="text-sm font-bold text-slate-900">
                  部署 VLESS+Reality 线路机 · {deployTarget.instance_name || deployTarget.ip}
                </h3>
              </div>
              <button
                onClick={() => setDeployTarget(null)}
                disabled={deploying}
                className="text-slate-400 hover:text-slate-600 p-1 rounded-md"
              >
                <X className="w-4 h-4" />
              </button>
            </div>

            <form onSubmit={handleConfirmDeploy} className="p-5 space-y-4 text-xs">
              <div className="bg-slate-50 border border-slate-200 rounded-lg p-3 text-slate-600 space-y-1">
                <div>
                  目标主机：<span className="font-mono font-semibold text-slate-900">{deployTarget.ip}</span>
                  {deployTarget.is_nat ? '（懒猫云 NAT，自动复用/申请端口映射）' : '（普通流量 VPS，优先 Docker，无 Docker 自动安装原生 sing-box）'}
                </div>
                {deployTarget.port_limit ? (
                  <div>
                    当前 NAT 端口配额：<span className="font-mono font-semibold">{deployTarget.port_count || 0} / {deployTarget.port_limit}</span>
                  </div>
                ) : null}
              </div>

              <div>
                <label className="block font-semibold text-slate-700 mb-1">
                  线路机节点名称（禁用 LZ-/家宽 等字样）
                </label>
                <input
                  type="text"
                  value={deployNodeName}
                  onChange={(e) => setDeployNodeName(e.target.value)}
                  placeholder="例如 HK-HKG3-NAT"
                  className="w-full px-3 py-2 border border-slate-300 rounded-lg font-mono text-xs focus:outline-none focus:ring-2 focus:ring-blue-500"
                  required
                />
              </div>

              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="block font-semibold text-slate-700 mb-1">
                    私网监听端口（留空自动分配）
                  </label>
                  <input
                    type="number"
                    min={1}
                    max={65535}
                    value={deployPort}
                    onChange={(e) => setDeployPort(e.target.value)}
                    placeholder="自动复用或分配"
                    className="w-full px-3 py-2 border border-slate-300 rounded-lg font-mono text-xs focus:outline-none focus:ring-2 focus:ring-blue-500"
                  />
                </div>
                <div>
                  <label className="block font-semibold text-slate-700 mb-1">
                    Reality 伪装域名 (SNI)
                  </label>
                  <input
                    type="text"
                    value={deploySNI}
                    onChange={(e) => setDeploySNI(e.target.value)}
                    placeholder="aws.amazon.com"
                    className="w-full px-3 py-2 border border-slate-300 rounded-lg font-mono text-xs focus:outline-none focus:ring-2 focus:ring-blue-500"
                  />
                </div>
              </div>

              <div className="pt-2 flex justify-end space-x-2">
                <button
                  type="button"
                  onClick={() => setDeployTarget(null)}
                  disabled={deploying}
                  className="px-3 py-1.5 rounded-lg text-slate-700 bg-white border border-slate-200 hover:bg-slate-50"
                >
                  取消
                </button>
                <button
                  type="submit"
                  disabled={deploying}
                  className="inline-flex items-center px-4 py-1.5 rounded-lg font-semibold text-white bg-blue-600 hover:bg-blue-700 disabled:opacity-50"
                >
                  {deploying ? (
                    <>
                      <Loader2 className="w-3.5 h-3.5 mr-1.5 animate-spin" />
                      部署并冒烟验证中...
                    </>
                  ) : (
                    <>
                      <Play className="w-3.5 h-3.5 mr-1.5" />
                      一键部署并接入线路机
                    </>
                  )}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
};