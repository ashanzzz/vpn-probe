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
  Play, 
  Square, 
  Archive, 
  History, 
  RotateCcw,
  Activity,
  Check,
  X,
  Plus,
  Trash2,
  Globe,
  Network
} from 'lucide-react';
import { api } from '../api/client';
import { SoftwareInfo, BlockedAnalysisResult, BackupSnapshot, SafeUpgradeResponse, RegisterInstanceRequest } from '../types';

interface VPSCenterProps {
  onOpenHealingWizard: (nodeName: string) => void;
}

export const VPSCenter: React.FC<VPSCenterProps> = ({ onOpenHealingWizard }) => {
  const [instances, setInstances] = useState<SoftwareInfo[]>([]);
  const [blockedAnalysis, setBlockedAnalysis] = useState<BlockedAnalysisResult[]>([]);
  const [loading, setLoading] = useState(true);
  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const [expandedBackups, setExpandedBackups] = useState<Record<string, boolean>>({});
  const [backupsMap, setBackupsMap] = useState<Record<string, BackupSnapshot[]>>({});
  const [message, setMessage] = useState<{ type: 'success' | 'error' | 'warning'; text: string } | null>(null);

  // 注册新服务器 Modal 状态
  const [showRegisterModal, setShowRegisterModal] = useState(false);
  const [registerForm, setRegisterForm] = useState<RegisterInstanceRequest>({
    name: '',
    ip: '',
    ssh_port: 22,
    ssh_user: 'root',
    ssh_password: '',
    is_nat: false,
    nat_ports: [],
    region: '日本',
  });
  const [natPortsInput, setNatPortsInput] = useState('');

  const fetchData = async () => {
    setLoading(true);
    try {
      const [vpsData, analysisData] = await Promise.all([
        api.getVPSManagement().catch(() => ({ count: 0, instances: [] })),
        api.getBlockedAnalysis().catch(() => ({ count: 0, results: [] })),
      ]);
      setInstances(vpsData.instances || []);
      setBlockedAnalysis(analysisData.results || []);

      const bMap: Record<string, BackupSnapshot[]> = {};
      (vpsData.instances || []).forEach(inst => {
        if (inst.backups && inst.backups.length > 0) {
          bMap[inst.instance_id || inst.ip] = inst.backups;
        }
      });
      setBackupsMap(prev => ({ ...prev, ...bMap }));
    } catch (err: any) {
      setMessage({ type: 'error', text: err.message || '获取 VPS 运维数据失败' });
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchData();
  }, []);

  // 执行工业级安全升级（备份 -> 拉取新镜像 -> 重启 -> 沙箱冒烟验证 -> 失败自动回滚）
  const handleSafeUpgrade = async (inst: SoftwareInfo) => {
    const vpsId = inst.instance_id || inst.ip;
    setActionLoading(`upgrade-${vpsId}`);
    setMessage(null);
    try {
      const res = await api.safeUpgradeVPS(vpsId, inst.instance_name);
      const r = res.result;
      if (r.rolled_back) {
        setMessage({
          type: 'warning',
          text: `⚠️ 升级失败已自动回滚！原因: ${r.rollback_reason || r.message}。服务已安全恢复旧版，未中断运行。`,
        });
      } else if (r.success) {
        setMessage({
          type: 'success',
          text: `✅ ${inst.instance_name} (${inst.ip}) 升级成功！当前版本: ${r.current_version}。${r.smoke_test?.message || '冒烟测试全链路通过！'}`,
        });
      } else {
        setMessage({ type: 'error', text: r.message || '升级未完全成功' });
      }
      await fetchData();
    } catch (err: any) {
      setMessage({ type: 'error', text: `安全升级执行异常: ${err.message}` });
    } finally {
      setActionLoading(null);
    }
  };

  // 执行生命周期动作 (start, stop, restart, backup)
  const handleLifecycleAction = async (inst: SoftwareInfo, action: 'start' | 'stop' | 'restart' | 'backup') => {
    const vpsId = inst.instance_id || inst.ip;
    setActionLoading(`${action}-${vpsId}`);
    setMessage(null);
    try {
      const res = await api.executeVPSLifecycleAction(vpsId, action);
      const actionText = {
        start: '启动',
        stop: '关闭',
        restart: '重启',
        backup: '快照备份',
      }[action];
      setMessage({
        type: 'success',
        text: `实例 ${inst.instance_name} (${inst.ip}) ${actionText}完成！当前状态: ${res.instance?.status}`,
      });
      await fetchData();
      if (action === 'backup') {
        loadBackups(vpsId);
      }
    } catch (err: any) {
      setMessage({ type: 'error', text: `操作执行失败: ${err.message}` });
    } finally {
      setActionLoading(null);
    }
  };

  // 一键部署指定协议核心 (sing-box / 3x-ui / xray，全系统一 VLESS-Reality)
  const handleInstallProxy = async (inst: SoftwareInfo, software: string) => {
    const vpsId = inst.instance_id || inst.ip;
    setActionLoading(`install-${vpsId}`);
    setMessage(null);
    try {
      const res = await api.installVPSProxy(vpsId, software);
      setMessage({
        type: 'success',
        text: `✅ ${inst.instance_name} (${inst.ip}) ${software} 部署完成！${res.message || '已自动生成 Reality 规则并推送至 SubStore 订阅！'}`,
      });
      await fetchData();
    } catch (err: any) {
      setMessage({ type: 'error', text: `部署失败: ${err.message}` });
    } finally {
      setActionLoading(null);
    }
  };

  // 一键彻底卸载代理服务 (清理容器、清理配置、下架 SubStore)
  const handleUninstall = async (inst: SoftwareInfo) => {
    if (!window.confirm(`⚠️ 高危操作：确定要彻底卸载 ${inst.instance_name} (${inst.ip}) 上的所有代理软件容器并清理配置、从 SubStore 订阅下架该节点吗？`)) {
      return;
    }
    const vpsId = inst.instance_id || inst.ip;
    setActionLoading(`uninstall-${vpsId}`);
    setMessage(null);
    try {
      const res = await api.uninstallVPS(vpsId, true, true);
      setMessage({
        type: 'success',
        text: `✅ ${res.result?.message || '已完成卸载与清理'}`,
      });
      await fetchData();
    } catch (err: any) {
      setMessage({ type: 'error', text: `卸载失败: ${err.message}` });
    } finally {
      setActionLoading(null);
    }
  };

  // 展开或收起快照列表
  const toggleBackups = async (vpsId: string) => {
    const nextState = !expandedBackups[vpsId];
    setExpandedBackups(prev => ({ ...prev, [vpsId]: nextState }));
    if (nextState && !backupsMap[vpsId]) {
      await loadBackups(vpsId);
    }
  };

  const loadBackups = async (vpsId: string) => {
    try {
      const res = await api.getVPSBackups(vpsId);
      setBackupsMap(prev => ({ ...prev, [vpsId]: res.backups || [] }));
    } catch (err: any) {
      console.warn('Load backups error:', err);
    }
  };

  // 还原快照
  const handleRestoreBackup = async (inst: SoftwareInfo, snapshotId: string) => {
    if (!window.confirm(`确定要从快照 ${snapshotId} 还原配置并重启容器吗？`)) {
      return;
    }
    const vpsId = inst.instance_id || inst.ip;
    setActionLoading(`restore-${vpsId}`);
    setMessage(null);
    try {
      await api.restoreVPSBackup(vpsId, snapshotId);
      setMessage({
        type: 'success',
        text: `✅ 实例 ${inst.instance_name} 已成功从快照 ${snapshotId} 还原，容器已平滑重启！`,
      });
      await fetchData();
    } catch (err: any) {
      setMessage({ type: 'error', text: `快照还原失败: ${err.message}` });
    } finally {
      setActionLoading(null);
    }
  };

  // 提交注册全新 VPS / NAT VPS
  const handleRegisterSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!registerForm.name || !registerForm.ip) {
      alert('请填写名称与公网 IP');
      return;
    }
    let natPorts: number[] = [];
    if (registerForm.is_nat && natPortsInput) {
      natPorts = natPortsInput
        .split(/[,，\s]+/)
        .map(p => parseInt(p.trim(), 10))
        .filter(p => !isNaN(p) && p > 0 && p < 65536);
    }

    try {
      const res = await api.registerVPSInstance({
        ...registerForm,
        nat_ports: natPorts,
      });
      setMessage({
        type: 'success',
        text: `✅ 成功纳管新服务器: ${res.instance?.name || registerForm.name} (${registerForm.ip})`,
      });
      setShowRegisterModal(false);
      setRegisterForm({
        name: '',
        ip: '',
        ssh_port: 22,
        ssh_user: 'root',
        ssh_password: '',
        is_nat: false,
        nat_ports: [],
        region: '日本',
      });
      setNatPortsInput('');
      await fetchData();
    } catch (err: any) {
      alert(`注册失败: ${err.message}`);
    }
  };

  return (
    <div className="space-y-6">
      {/* Header bar */}
      <div className="bg-white border border-slate-200 rounded-xl p-4 shadow-2xs flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
        <div>
          <h2 className="text-base font-bold text-slate-900 flex items-center">
            <Cpu className="w-5 h-5 mr-2 text-blue-600" />
            全自动多核心部署与运维中心
          </h2>
          <p className="text-xs text-slate-500 mt-0.5">
            VPS / NAT VPS 多核心 VLESS-Reality 纳管 · 官方 GitHub 发版感知 · 深度墙况自愈闭环 · SubStore 4分组拓扑同步
          </p>
        </div>

        <div className="flex items-center space-x-2">
          <button
            onClick={() => setShowRegisterModal(true)}
            className="inline-flex items-center px-3 py-1.5 text-xs font-semibold rounded-lg text-white bg-blue-600 hover:bg-blue-700 transition shadow-2xs cursor-pointer"
          >
            <Plus className="w-3.5 h-3.5 mr-1" />
            纳管新服务器
          </button>

          <button
            onClick={fetchData}
            disabled={loading}
            className="inline-flex items-center px-3 py-1.5 text-xs font-medium rounded-lg text-slate-700 bg-white border border-slate-200 hover:bg-slate-50 disabled:opacity-50 transition shadow-2xs cursor-pointer"
          >
            <RotateCw className={`w-3.5 h-3.5 mr-1.5 ${loading ? 'animate-spin text-blue-600' : 'text-slate-500'}`} />
            刷新状态
          </button>
        </div>
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
          <div className="flex items-center space-x-2">
            {message.type === 'success' && <Check className="w-4 h-4 text-emerald-600" />}
            {message.type === 'warning' && <AlertTriangle className="w-4 h-4 text-amber-600" />}
            {message.type === 'error' && <X className="w-4 h-4 text-red-600" />}
            <span>{message.text}</span>
          </div>
          <button onClick={() => setMessage(null)} className="text-xs underline ml-2 cursor-pointer">
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
              墙况深度研判与自愈诊断矩阵 (GFW Diagnostic Matrix)
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
                            className="inline-flex items-center px-2.5 py-1 text-xs font-semibold rounded bg-red-600 hover:bg-red-700 text-white shadow-2xs transition cursor-pointer"
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
              VPS / NAT VPS 托管实例与多核心部署中心
            </h3>
          </div>
          <span className="text-2xs font-mono text-slate-400">
            VLESS-Reality Multi-Core Lifecycle
          </span>
        </div>

        <div className="divide-y divide-slate-200">
          {instances.length > 0 ? (
            instances.map((inst, idx) => {
              const vpsId = inst.instance_id || inst.ip;
              const isUpgrading = actionLoading === `upgrade-${vpsId}`;
              const isRestarting = actionLoading === `restart-${vpsId}`;
              const isStopping = actionLoading === `stop-${vpsId}`;
              const isStarting = actionLoading === `start-${vpsId}`;
              const isBackingUp = actionLoading === `backup-${vpsId}`;
              const isRestoring = actionLoading === `restore-${vpsId}`;
              const isInstalling = actionLoading === `install-${vpsId}`;
              const isUninstalling = actionLoading === `uninstall-${vpsId}`;
              const backups = backupsMap[vpsId] || [];
              const isBackupsExpanded = !!expandedBackups[vpsId];

              return (
                <div key={idx} className="p-4 hover:bg-slate-50/50 transition">
                  <div className="flex flex-col lg:flex-row lg:items-center justify-between gap-4">
                    {/* Basic Info */}
                    <div className="space-y-1.5 flex-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="font-bold text-slate-900 text-sm">
                          {inst.instance_name || inst.instance_id}
                        </span>

                        {inst.region && (
                          <span className="inline-flex items-center px-1.5 py-0.5 rounded text-2xs bg-slate-100 text-slate-600 font-medium">
                            <Globe className="w-3 h-3 mr-1 text-slate-400" />
                            {inst.region}
                          </span>
                        )}

                        {inst.is_nat && (
                          <span className="inline-flex items-center px-1.5 py-0.5 rounded text-2xs bg-purple-50 text-purple-700 border border-purple-200 font-semibold font-mono">
                            <Network className="w-3 h-3 mr-1 text-purple-500" />
                            NAT 机
                          </span>
                        )}

                        <span className="px-2 py-0.5 rounded bg-blue-50 text-blue-700 font-semibold border border-blue-200 text-2xs uppercase">
                          {inst.software_type}
                        </span>

                        <span className="text-xs font-mono text-slate-500 bg-slate-100 px-1.5 py-0.5 rounded">
                          {inst.ip}
                        </span>

                        <span className={`inline-flex items-center px-2 py-0.5 rounded text-2xs font-semibold ${
                          inst.status === 'running' 
                            ? 'bg-emerald-100 text-emerald-800' 
                            : 'bg-slate-100 text-slate-600'
                        }`}>
                          {inst.status === 'running' ? (
                            <CheckCircle2 className="w-3 h-3 mr-1 text-emerald-600" />
                          ) : (
                            <AlertTriangle className="w-3 h-3 mr-1 text-slate-500" />
                          )}
                          {inst.status || 'unknown'}
                        </span>
                      </div>

                      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-slate-600">
                        <div className="flex items-center space-x-1 font-mono">
                          <span className="text-slate-400">当前版本:</span>
                          <span className="font-semibold text-slate-800">
                            {inst.current_version || '未安装/未知'}
                          </span>
                          {inst.has_update && (
                            <span className="inline-flex items-center px-1.5 py-0.2 rounded bg-amber-100 text-amber-800 text-2xs font-sans font-medium">
                              <ArrowUpCircle className="w-3 h-3 mr-0.5 text-amber-600" />
                              可升级至: {inst.latest_version}
                            </span>
                          )}
                        </div>

                        <div className="flex items-center space-x-1 font-mono">
                          <span className="text-slate-400">活跃端口:</span>
                          <span>{inst.active_ports?.length ? inst.active_ports.join(', ') : '-'}</span>
                        </div>

                        {inst.is_nat && inst.nat_ports && inst.nat_ports.length > 0 && (
                          <div className="flex items-center space-x-1 font-mono text-2xs text-purple-700">
                            <span className="text-purple-400">NAT可用端口池:</span>
                            <span>[{inst.nat_ports.join(', ')}]</span>
                          </div>
                        )}

                        {inst.container_name && (
                          <div className="flex items-center space-x-1 font-mono text-slate-400 text-2xs">
                            <span>容器: {inst.container_name}</span>
                          </div>
                        )}
                      </div>

                      {/* Smoke test pill if available */}
                      {inst.last_smoke_test && (
                        <div className="mt-1 flex items-center space-x-2 text-2xs">
                          <span className="text-slate-400">最近出网冒烟测试:</span>
                          <span className={`inline-flex items-center px-1.5 py-0.5 rounded font-mono ${
                            inst.last_smoke_test.success 
                              ? 'bg-emerald-50 text-emerald-700 border border-emerald-200' 
                              : 'bg-red-50 text-red-700 border border-red-200'
                          }`}>
                            <Activity className="w-3 h-3 mr-1" />
                            {inst.last_smoke_test.success 
                              ? `通过 (${inst.last_smoke_test.latency_ms}ms, 出口: ${inst.last_smoke_test.exit_ip || 'OK'})` 
                              : `失败: ${inst.last_smoke_test.error || '握手异常'}`}
                          </span>
                        </div>
                      )}
                    </div>

                    {/* Action buttons */}
                    <div className="flex flex-wrap items-center gap-2">
                      {/* 一键部署多核心下拉选择 */}
                      <div className="inline-flex rounded-lg shadow-2xs">
                        <button
                          onClick={() => handleInstallProxy(inst, 'sing-box')}
                          disabled={!!actionLoading}
                          className="px-2.5 py-1.5 text-xs font-semibold rounded-l-lg bg-emerald-600 hover:bg-emerald-700 text-white disabled:opacity-50 transition cursor-pointer"
                          title="一键部署官方最新 sing-box VLESS-Reality 容器，自动放行端口并生成订阅"
                        >
                          {isInstalling ? <Loader2 className="w-3 h-3 animate-spin" /> : '装 sing-box'}
                        </button>
                        <button
                          onClick={() => handleInstallProxy(inst, '3x-ui')}
                          disabled={!!actionLoading}
                          className="px-2 py-1.5 text-xs font-medium bg-emerald-700 hover:bg-emerald-800 text-white border-l border-emerald-800 disabled:opacity-50 transition cursor-pointer"
                          title="一键部署 3x-ui 面板并自动初始化一条 VLESS-Reality 入站"
                        >
                          装 3x-ui
                        </button>
                        <button
                          onClick={() => handleInstallProxy(inst, 'xray')}
                          disabled={!!actionLoading}
                          className="px-2 py-1.5 text-xs font-medium rounded-r-lg bg-emerald-800 hover:bg-emerald-900 text-white border-l border-emerald-900 disabled:opacity-50 transition cursor-pointer"
                          title="一键部署 Xray-core VLESS-Reality 容器"
                        >
                          装 Xray
                        </button>
                      </div>

                      {/* 安全升级按钮 */}
                      <button
                        onClick={() => handleSafeUpgrade(inst)}
                        disabled={!!actionLoading}
                        className={`inline-flex items-center px-2.5 py-1.5 text-xs font-semibold rounded-lg shadow-2xs transition cursor-pointer ${
                          inst.has_update
                            ? 'bg-amber-600 hover:bg-amber-700 text-white'
                            : 'bg-blue-600 hover:bg-blue-700 text-white'
                        } disabled:opacity-50`}
                        title="执行安全升级：前置快照备份 -> 镜像拉取与启动 -> 内置沙箱冒烟功能检验 -> 失败自动原子回滚"
                      >
                        {isUpgrading ? (
                          <>
                            <Loader2 className="w-3.5 h-3.5 mr-1 animate-spin" />
                            升级验证中...
                          </>
                        ) : (
                          <>
                            <Download className="w-3.5 h-3.5 mr-1" />
                            {inst.has_update ? '升级' : '更新'}
                          </>
                        )}
                      </button>

                      {/* 平滑重启 */}
                      <button
                        onClick={() => handleLifecycleAction(inst, 'restart')}
                        disabled={!!actionLoading}
                        className="inline-flex items-center px-2.5 py-1.5 text-xs font-medium rounded-lg text-slate-700 bg-white border border-slate-200 hover:bg-slate-50 disabled:opacity-50 transition shadow-2xs cursor-pointer"
                        title="平滑重启容器进程"
                      >
                        {isRestarting ? (
                          <Loader2 className="w-3.5 h-3.5 mr-1 animate-spin text-blue-600" />
                        ) : (
                          <RotateCcw className="w-3.5 h-3.5 mr-1 text-slate-500" />
                        )}
                        重启
                      </button>

                      {/* 启停控制 */}
                      {inst.status === 'running' ? (
                        <button
                          onClick={() => handleLifecycleAction(inst, 'stop')}
                          disabled={!!actionLoading}
                          className="inline-flex items-center px-2.5 py-1.5 text-xs font-medium rounded-lg text-slate-700 bg-white border border-slate-200 hover:bg-slate-50 disabled:opacity-50 transition shadow-2xs cursor-pointer"
                          title="安全停止服务容器"
                        >
                          {isStopping ? (
                            <Loader2 className="w-3.5 h-3.5 mr-1 animate-spin" />
                          ) : (
                            <Square className="w-3.5 h-3.5 mr-1 text-slate-500" />
                          )}
                          停止
                        </button>
                      ) : (
                        <button
                          onClick={() => handleLifecycleAction(inst, 'start')}
                          disabled={!!actionLoading}
                          className="inline-flex items-center px-2.5 py-1.5 text-xs font-medium rounded-lg text-emerald-700 bg-emerald-50 border border-emerald-200 hover:bg-emerald-100 disabled:opacity-50 transition shadow-2xs cursor-pointer"
                          title="启动服务容器"
                        >
                          {isStarting ? (
                            <Loader2 className="w-3.5 h-3.5 mr-1 animate-spin" />
                          ) : (
                            <Play className="w-3.5 h-3.5 mr-1" />
                          )}
                          启动
                        </button>
                      )}

                      {/* 快照与还原展开 */}
                      <button
                        onClick={() => toggleBackups(vpsId)}
                        className={`inline-flex items-center px-2.5 py-1.5 text-xs font-medium rounded-lg border transition shadow-2xs cursor-pointer ${
                          isBackupsExpanded
                            ? 'bg-slate-100 text-slate-900 border-slate-300'
                            : 'bg-white text-slate-700 border-slate-200 hover:bg-slate-50'
                        }`}
                        title="查看与还原配置快照"
                      >
                        <Archive className="w-3.5 h-3.5 mr-1 text-slate-500" />
                        快照 ({backups.length})
                      </button>

                      {/* 彻底卸载 */}
                      <button
                        onClick={() => handleUninstall(inst)}
                        disabled={!!actionLoading}
                        className="inline-flex items-center px-2.5 py-1.5 text-xs font-medium rounded-lg text-red-700 bg-red-50 hover:bg-red-100 border border-red-200 disabled:opacity-50 transition shadow-2xs cursor-pointer"
                        title="彻底卸载代理容器、清理配置目录并从 SubStore 下架节点"
                      >
                        {isUninstalling ? (
                          <Loader2 className="w-3.5 h-3.5 animate-spin" />
                        ) : (
                          <Trash2 className="w-3.5 h-3.5" />
                        )}
                      </button>
                    </div>
                  </div>

                  {/* 展开的快照面板 */}
                  {isBackupsExpanded && (
                    <div className="mt-3 pt-3 border-t border-slate-100 bg-slate-50/70 p-3 rounded-lg space-y-2">
                      <div className="flex items-center justify-between">
                        <div className="flex items-center space-x-1.5 text-xs font-semibold text-slate-700">
                          <History className="w-3.5 h-3.5 text-slate-500" />
                          <span>历史轻量原子快照 (滚动保留最新 3 份)</span>
                        </div>
                        <button
                          onClick={() => handleLifecycleAction(inst, 'backup')}
                          disabled={!!actionLoading}
                          className="inline-flex items-center px-2 py-1 text-2xs font-semibold rounded bg-white border border-slate-200 hover:bg-slate-50 text-slate-700 transition shadow-2xs cursor-pointer"
                        >
                          {isBackingUp ? (
                            <Loader2 className="w-3 h-3 mr-1 animate-spin text-blue-600" />
                          ) : (
                            <Archive className="w-3 h-3 mr-1 text-slate-500" />
                          )}
                          立即创建快照
                        </button>
                      </div>

                      {backups.length > 0 ? (
                        <div className="space-y-1.5">
                          {backups.map((b, bIdx) => (
                            <div
                              key={bIdx}
                              className="flex items-center justify-between text-xs bg-white border border-slate-200 rounded px-2.5 py-1.5"
                            >
                              <div className="flex items-center space-x-2 font-mono text-2xs">
                                <span className="font-semibold text-slate-800">{b.file_name}</span>
                                <span className="text-slate-400">({(b.size_bytes / 1024).toFixed(1)} KB)</span>
                                <span className="text-slate-400">创建于: {b.created_at}</span>
                              </div>
                              <button
                                onClick={() => handleRestoreBackup(inst, b.id || b.file_name)}
                                disabled={!!actionLoading}
                                className="inline-flex items-center px-2 py-0.5 text-2xs font-medium text-amber-700 bg-amber-50 hover:bg-amber-100 border border-amber-200 rounded transition cursor-pointer"
                              >
                                {isRestoring ? (
                                  <Loader2 className="w-2.5 h-2.5 mr-1 animate-spin" />
                                ) : (
                                  <RotateCcw className="w-2.5 h-2.5 mr-1" />
                                )}
                                还原此快照
                              </button>
                            </div>
                          ))}
                        </div>
                      ) : (
                        <div className="text-2xs text-slate-400 py-1">
                          暂无历史快照，可在升级或换端口前自动生成，或点击上方“立即创建快照”。
                        </div>
                      )}
                    </div>
                  )}
                </div>
              );
            })
          ) : (
            <div className="p-8 text-center text-slate-400 text-xs">
              暂未配置 VPS 远程 SSH 实例，可点击右上角“纳管新服务器”直接录入！
            </div>
          )}
        </div>
      </div>

      {/* 纳管新服务器 Modal 弹窗 */}
      {showRegisterModal && (
        <div className="fixed inset-0 bg-slate-900/40 backdrop-blur-xs flex items-center justify-center p-4 z-50">
          <div className="bg-white rounded-xl shadow-xl border border-slate-200 max-w-lg w-full p-6 space-y-4">
            <div className="flex items-center justify-between border-b border-slate-100 pb-3">
              <h3 className="text-sm font-bold text-slate-900 flex items-center">
                <Plus className="w-4 h-4 mr-1.5 text-blue-600" />
                纳管全新独立 VPS / NAT VPS
              </h3>
              <button
                onClick={() => setShowRegisterModal(false)}
                className="text-slate-400 hover:text-slate-600 cursor-pointer"
              >
                <X className="w-4 h-4" />
              </button>
            </div>

            <form onSubmit={handleRegisterSubmit} className="space-y-3 text-xs">
              <div>
                <label className="block font-medium text-slate-700 mb-1">实例名称 *</label>
                <input
                  type="text"
                  required
                  placeholder="例如: JP-Tokyo-IIJ 或 HK-NAT-01"
                  value={registerForm.name}
                  onChange={e => setRegisterForm(f => ({ ...f, name: e.target.value }))}
                  className="w-full px-3 py-1.5 border border-slate-300 rounded-lg focus:outline-none focus:ring-1 focus:ring-blue-500"
                />
              </div>

              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="block font-medium text-slate-700 mb-1">公网 IP / 域名 *</label>
                  <input
                    type="text"
                    required
                    placeholder="1.2.3.4"
                    value={registerForm.ip}
                    onChange={e => setRegisterForm(f => ({ ...f, ip: e.target.value }))}
                    className="w-full px-3 py-1.5 border border-slate-300 rounded-lg focus:outline-none focus:ring-1 focus:ring-blue-500 font-mono"
                  />
                </div>
                <div>
                  <label className="block font-medium text-slate-700 mb-1">SSH 端口</label>
                  <input
                    type="number"
                    value={registerForm.ssh_port || 22}
                    onChange={e => setRegisterForm(f => ({ ...f, ssh_port: parseInt(e.target.value, 10) || 22 }))}
                    className="w-full px-3 py-1.5 border border-slate-300 rounded-lg focus:outline-none focus:ring-1 focus:ring-blue-500 font-mono"
                  />
                </div>
              </div>

              <div className="grid grid-cols-2 gap-3">
                <div>
                  <label className="block font-medium text-slate-700 mb-1">SSH 用户名</label>
                  <input
                    type="text"
                    value={registerForm.ssh_user || 'root'}
                    onChange={e => setRegisterForm(f => ({ ...f, ssh_user: e.target.value }))}
                    className="w-full px-3 py-1.5 border border-slate-300 rounded-lg focus:outline-none focus:ring-1 focus:ring-blue-500 font-mono"
                  />
                </div>
                <div>
                  <label className="block font-medium text-slate-700 mb-1">归属地区</label>
                  <input
                    type="text"
                    placeholder="日本 / 美国 / 香港"
                    value={registerForm.region || ''}
                    onChange={e => setRegisterForm(f => ({ ...f, region: e.target.value }))}
                    className="w-full px-3 py-1.5 border border-slate-300 rounded-lg focus:outline-none focus:ring-1 focus:ring-blue-500"
                  />
                </div>
              </div>

              <div>
                <label className="block font-medium text-slate-700 mb-1">root 登录密码</label>
                <input
                  type="password"
                  placeholder="SSH 认证密码"
                  value={registerForm.ssh_password || ''}
                  onChange={e => setRegisterForm(f => ({ ...f, ssh_password: e.target.value }))}
                  className="w-full px-3 py-1.5 border border-slate-300 rounded-lg focus:outline-none focus:ring-1 focus:ring-blue-500 font-mono"
                />
              </div>

              {/* NAT VPS 特性开关 */}
              <div className="bg-slate-50 p-3 rounded-lg border border-slate-200 space-y-2">
                <label className="flex items-center space-x-2 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={registerForm.is_nat || false}
                    onChange={e => setRegisterForm(f => ({ ...f, is_nat: e.target.checked }))}
                    className="rounded text-blue-600 focus:ring-blue-500"
                  />
                  <span className="font-semibold text-slate-800">这是 NAT VPS (非全端口独立公网机)</span>
                </label>

                {registerForm.is_nat && (
                  <div>
                    <label className="block text-2xs font-medium text-slate-600 mb-1">
                      分配的 NAT 外部映射端口列表（以逗号分隔，如: 30001, 30002, 30003）
                    </label>
                    <input
                      type="text"
                      placeholder="30001, 30002, 30003"
                      value={natPortsInput}
                      onChange={e => setNatPortsInput(e.target.value)}
                      className="w-full px-3 py-1.5 border border-slate-300 rounded-lg bg-white focus:outline-none focus:ring-1 focus:ring-purple-500 font-mono text-2xs"
                    />
                    <p className="text-3xs text-slate-400 mt-0.5">
                      系统部署代理及换端口自愈时，将严格受限并仅在此端口池内轮换。
                    </p>
                  </div>
                )}
              </div>

              <div className="flex items-center justify-end space-x-2 pt-2">
                <button
                  type="button"
                  onClick={() => setShowRegisterModal(false)}
                  className="px-3 py-1.5 text-xs text-slate-600 hover:bg-slate-100 rounded-lg transition cursor-pointer"
                >
                  取消
                </button>
                <button
                  type="submit"
                  className="px-4 py-1.5 text-xs font-semibold text-white bg-blue-600 hover:bg-blue-700 rounded-lg transition shadow-2xs cursor-pointer"
                >
                  确认纳管
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
};
