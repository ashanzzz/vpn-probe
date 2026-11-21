import React, { useState, useEffect } from 'react';
import { 
  Share2, 
  RotateCw, 
  Copy, 
  Check, 
  Trash2, 
  Save, 
  Layers, 
  FileCode, 
  Network,
  Loader2,
  AlertCircle,
  Sliders,
  Zap,
  Shield,
  ShieldCheck,
  Plus,
  Globe,
  RefreshCw,
  Power
} from 'lucide-react';
import { api, getStoredToken } from '../api/client';
import { 
  SubStoreMode, 
  SubStoreModeInfo, 
  SubStoreManifest,
  ExternalSubsResponse,
  ExternalSubDetail,
  SnapshotStatusResponse,
  CategoriesSummary,
  CustomGroupDef,
  GroupFilter
} from '../types';

export const SubStoreHub: React.FC = () => {
  const [modeInfo, setModeInfo] = useState<SubStoreModeInfo | null>(null);
  const [manifest, setManifest] = useState<SubStoreManifest | null>(null);
  const [extSubs, setExtSubs] = useState<ExternalSubsResponse | null>(null);
  const [snapshots, setSnapshots] = useState<SnapshotStatusResponse | null>(null);
  const [, setLoading] = useState(true);
  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const [activeTab, setActiveTab] = useState<'orchestration' | 'nodes' | 'scripts'>('orchestration');
  const [categories, setCategories] = useState<CategoriesSummary | null>(null);
  const [customGroups, setCustomGroups] = useState<CustomGroupDef[]>([]);
  const [evalFilter, setEvalFilter] = useState<GroupFilter>({
    countries: ['US'],
    include_transit: true,
    include_residential: true,
    include_chain: true,
    exclude_china_leak: true,
  });
  const [evalResults, setEvalResults] = useState<string[]>([]);
  const [evalLoading, setEvalLoading] = useState(false);
  const [newGroupName, setNewGroupName] = useState('');
  const [newGroupDisplay, setNewGroupDisplay] = useState('');
  const [subNodesText, setSubNodesText] = useState('');
  const [subscriptName, setSubscriptName] = useState("ashan's vps");
  const [selectedScript, setSelectedScript] = useState<string>('');
  const [scriptContent, setScriptContent] = useState<string>('');
  const [statusMsg, setStatusMsg] = useState<{ type: 'success' | 'error'; text: string } | null>(null);
  const [copiedLink, setCopiedLink] = useState<string | null>(null);

  // Modal for Adding External Subscription
  const [showAddExtModal, setShowAddExtModal] = useState(false);
  const [newExtName, setNewExtName] = useState('');
  const [newExtDisplayName, setNewExtDisplayName] = useState('');
  const [newExtUrl, setNewExtUrl] = useState('');

  const fetchHubData = async () => {
    setLoading(true);
    try {
      const [mInfo, mfest, extData, snapData, catData, grpData] = await Promise.all([
        api.getSubStoreMode().catch(() => null),
        api.getSubStoreManifest().catch(() => null),
        api.getExternalSubs().catch(() => null),
        api.getSnapshotStatus().catch(() => null),
        api.getCategoriesSummary().catch(() => null),
        api.getCustomGroups().catch(() => null),
      ]);
      ]);
      if (mInfo) setModeInfo(mInfo);
      if (catData) setCategories(catData);
      if (grpData && grpData.groups) setCustomGroups(grpData.groups);
      if (mfest) {
        setManifest(mfest);
        if (mfest.scripts && mfest.scripts.length > 0 && !selectedScript) {
          setSelectedScript(mfest.scripts[0].name);
        }
      }
      if (extData) setExtSubs(extData);
      if (snapData) setSnapshots(snapData);

      // Load nodes for subscription
      const nodeData = await api.getSubStoreNodes("ashan's vps").catch(() => null);
      if (nodeData && nodeData.nodes) {
        setSubNodesText(nodeData.nodes.join('\n'));
      }
    } catch (err: any) {
      setStatusMsg({ type: 'error', text: err.message || '加载 SubStore 数据失败' });
    } finally {
      setLoading(false);
    }
  };

  const handleRunEvaluate = async () => {
    setEvalLoading(true);
    try {
      const res = await api.evaluateGroup(evalFilter);
      setEvalResults(res.nodes || []);
      setStatusMsg({ type: 'success', text: `公式试算成功：匹配到 ${res.count} 个节点连接` });
    } catch (err: any) {
      setStatusMsg({ type: 'error', text: err.message || '公式试算失败' });
    } finally {
      setEvalLoading(false);
    }
  };

  const handleSaveGroup = async () => {
    if (!newGroupName.trim()) {
      setStatusMsg({ type: 'error', text: '分组系统英文标识不能为空' });
      return;
    }
    setActionLoading('save-group');
    try {
      const def: CustomGroupDef = {
        name: newGroupName.trim(),
        display_name: newGroupDisplay.trim() || newGroupName.trim(),
        enabled: true,
        filter: evalFilter,
      };
      await api.saveCustomGroup(def, true);
      setStatusMsg({ type: 'success', text: `分组 '${def.display_name}' 创建并同步成功！` });
      setNewGroupName('');
      setNewGroupDisplay('');
      fetchHubData();
    } catch (err: any) {
      setStatusMsg({ type: 'error', text: err.message || '保存分组失败' });
    } finally {
      setActionLoading(null);
    }
  };

  const handleDeleteGroup = async (name: string) => {
    if (!confirm(`确定删除自定义分组 '${name}' 吗？`)) return;
    setActionLoading(`del-group-${name}`);
    try {
      await api.deleteCustomGroup(name, true);
      setStatusMsg({ type: 'success', text: `分组 '${name}' 已删除` });
      fetchHubData();
    } catch (err: any) {
      setStatusMsg({ type: 'error', text: err.message || '删除分组失败' });
    } finally {
      setActionLoading(null);
    }
  };

  const handleRebuildChains = async () => {
    setActionLoading('rebuild-chains');
    try {
      const res = await api.rebuildFullMeshChains();
      setStatusMsg({ type: 'success', text: `方案B：${res.message || '线路机全量区域网状池化中继重建并同步完成'}` });
      fetchHubData();
    } catch (err: any) {
      setStatusMsg({ type: 'error', text: err.message || '重建中继失败' });
    } finally {
      setActionLoading(null);
    }
  };

  const handleSyncFiveGroups = async () => {
    setActionLoading('sync-five');
    try {
      const res = await api.syncFiveGroups();
      setStatusMsg({ type: 'success', text: `自建五维策略组同步成功：${res.message}` });
      fetchHubData();
    } catch (err: any) {
      setStatusMsg({ type: 'error', text: err.message || '同步五维策略组失败' });
    } finally {
      setActionLoading(null);
    }
  };

  useEffect(() => {
    fetchHubData();
  }, []);

  // Switch SubStore Mode
  const handleSwitchMode = async (newMode: SubStoreMode) => {
    setActionLoading(`mode-${newMode}`);
    setStatusMsg(null);
    try {
      await api.setSubStoreMode(newMode);
      setStatusMsg({ type: 'success', text: `已成功切换为【${newMode}】模式并持久化！` });
      const [mInfo, extData] = await Promise.all([
        api.getSubStoreMode(),
        api.getExternalSubs().catch(() => null),
      ]);
      setModeInfo(mInfo);
      if (extData) setExtSubs(extData);
    } catch (err: any) {
      setStatusMsg({ type: 'error', text: `模式切换失败: ${err.message}` });
    } finally {
      setActionLoading(null);
    }
  };

  // Sync Three Tier
  const handleSyncThreeTier = async () => {
    setActionLoading('sync-threetier');
    setStatusMsg(null);
    try {
      const res = await api.syncThreeTier();
      setStatusMsg({ type: 'success', text: `三层拓扑自愈同步完成: ${res.message || 'OK'}` });
      await fetchHubData();
    } catch (err: any) {
      setStatusMsg({ type: 'error', text: `同步失败: ${err.message}` });
    } finally {
      setActionLoading(null);
    }
  };

  // Trigger Snapshot Warmup (PR #22 / Issue #21)
  const handleWarmupSnapshots = async () => {
    setActionLoading('warmup');
    setStatusMsg(null);
    try {
      await api.warmupSnapshot(['Q', 'ashan']);
      setStatusMsg({ type: 'success', text: '后台预烘焙任务已触发！稍后将生成最新毫秒级静态快照。' });
      setTimeout(async () => {
        const snap = await api.getSnapshotStatus().catch(() => null);
        if (snap) setSnapshots(snap);
      }, 2500);
    } catch (err: any) {
      setStatusMsg({ type: 'error', text: `触发快照预热失败: ${err.message}` });
    } finally {
      setActionLoading(null);
    }
  };

  // Add / Upsert External Subscription (PR #20 / Issue #19)
  const handleAddExternalSub = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!newExtName.trim() || !newExtUrl.trim()) {
      setStatusMsg({ type: 'error', text: '订阅标识与订阅 URL 为必填项' });
      return;
    }
    setActionLoading('add-ext');
    setStatusMsg(null);
    try {
      await api.upsertExternalSub({
        name: newExtName.trim(),
        display_name: newExtDisplayName.trim() || newExtName.trim(),
        url: newExtUrl.trim(),
        enabled: true,
        auto_mount: true,
      });
      setStatusMsg({ type: 'success', text: `外部机场订阅【${newExtName}】已成功挂载并联动 Sub-Store 同步！` });
      setShowAddExtModal(false);
      setNewExtName('');
      setNewExtDisplayName('');
      setNewExtUrl('');
      const extData = await api.getExternalSubs();
      setExtSubs(extData);
    } catch (err: any) {
      setStatusMsg({ type: 'error', text: `添加外部机场失败: ${err.message}` });
    } finally {
      setActionLoading(null);
    }
  };

  // Toggle External Source Enable/Disable
  const handleToggleExtSource = async (detail: ExternalSubDetail) => {
    setActionLoading(`toggle-${detail.name}`);
    setStatusMsg(null);
    try {
      await api.upsertExternalSub({
        name: detail.name,
        display_name: detail.display_name,
        url: detail.url || '',
        enabled: !detail.enabled,
      });
      setStatusMsg({ 
        type: 'success', 
        text: `外部源【${detail.display_name || detail.name}】已${detail.enabled ? '停用' : '启用'}！` 
      });
      const extData = await api.getExternalSubs();
      setExtSubs(extData);
    } catch (err: any) {
      setStatusMsg({ type: 'error', text: `操作失败: ${err.message}` });
    } finally {
      setActionLoading(null);
    }
  };

  // Delete External Source
  const handleDeleteExtSource = async (name: string) => {
    if (!window.confirm(`确定要解绑并从 Sub-Store 中物理清除外部订阅【${name}】吗？`)) {
      return;
    }
    setActionLoading(`del-${name}`);
    setStatusMsg(null);
    try {
      await api.deleteExternalSub(name, true);
      setStatusMsg({ type: 'success', text: `外部源【${name}】已成功物理卸载并清除！` });
      const extData = await api.getExternalSubs();
      setExtSubs(extData);
    } catch (err: any) {
      setStatusMsg({ type: 'error', text: `删除失败: ${err.message}` });
    } finally {
      setActionLoading(null);
    }
  };

  // Save Nodes Text
  const handleSaveNodes = async () => {
    setActionLoading('save-nodes');
    setStatusMsg(null);
    try {
      const lines = subNodesText
        .split('\n')
        .map((s) => s.trim())
        .filter(Boolean);
      await api.updateSubStoreNodes(subscriptName, lines);
      setStatusMsg({ type: 'success', text: `已成功保存并同步 ${lines.length} 个节点至 SubStore 订阅【${subscriptName}】` });
    } catch (err: any) {
      setStatusMsg({ type: 'error', text: `保存节点失败: ${err.message}` });
    } finally {
      setActionLoading(null);
    }
  };

  // Load Script
  const handleLoadScript = async (name: string) => {
    setSelectedScript(name);
    try {
      const code = await api.getScriptContent(name);
      setScriptContent(code);
    } catch (err: any) {
      setStatusMsg({ type: 'error', text: `读取脚本源码失败: ${err.message}` });
    }
  };

  useEffect(() => {
    if (selectedScript) {
      handleLoadScript(selectedScript);
    }
  }, [selectedScript]);

  // Save Script
  const handleSaveScript = async () => {
    if (!selectedScript) return;
    setActionLoading('save-script');
    setStatusMsg(null);
    try {
      await api.updateScript(selectedScript, scriptContent);
      setStatusMsg({ type: 'success', text: `脚本【${selectedScript}】已热更新部署并在 SubStore 生效！` });
    } catch (err: any) {
      setStatusMsg({ type: 'error', text: `热更新脚本失败: ${err.message}` });
    } finally {
      setActionLoading(null);
    }
  };

  const handleCopy = (text: string, id: string) => {
    navigator.clipboard.writeText(text);
    setCopiedLink(id);
    setTimeout(() => setCopiedLink(null), 2000);
  };

  // 生成当前环境下的 1.3ms 快照直出地址
  const currentToken = getStoredToken() || 'Woo@@@204317';
  const getInstantSnapshotUrl = (fileName: string) => {
    const origin = window.location.origin;
    return `${origin}/api/sub/file/${fileName}?token=${encodeURIComponent(currentToken)}`;
  };

  return (
    <div className="space-y-6">
      {/* Status banner */}
      {statusMsg && (
        <div
          className={`p-3 rounded-lg text-xs flex items-center justify-between border ${
            statusMsg.type === 'success'
              ? 'bg-emerald-50 text-emerald-800 border-emerald-200'
              : 'bg-red-50 text-red-800 border-red-200'
          }`}
        >
          <span>{statusMsg.text}</span>
          <button onClick={() => setStatusMsg(null)} className="text-xs underline ml-2">
            关闭
          </button>
        </div>
      )}

      {/* 1. Top Mode Switch Card */}
      <div className="bg-white border border-slate-200 rounded-xl p-5 shadow-2xs">
        <div className="flex flex-col md:flex-row md:items-center md:justify-between gap-4 pb-4 border-b border-slate-100">
          <div>
            <div className="flex items-center space-x-2">
              <Layers className="w-5 h-5 text-blue-600" />
              <h2 className="text-base font-bold text-slate-900">Sub-Store 组合订阅三模式中枢</h2>
            </div>
            <p className="text-xs text-slate-500 mt-0.5">
              后台全自动解决拓扑构建，前端一键切换模式与触发拓扑自愈
            </p>
          </div>

          <div className="flex flex-wrap items-center gap-2">
            <button
              onClick={handleSyncThreeTier}
              disabled={!!actionLoading}
              className="inline-flex items-center px-3 py-1.5 text-xs font-semibold rounded-lg text-white bg-blue-600 hover:bg-blue-700 disabled:opacity-50 transition shadow-2xs"
            >
              {actionLoading === 'sync-threetier' ? (
                <Loader2 className="w-3.5 h-3.5 mr-1.5 animate-spin" />
              ) : (
                <RotateCw className="w-3.5 h-3.5 mr-1.5" />
              )}
              一键三层拓扑自愈同步
            </button>
          </div>
        </div>

        {/* Mode Selector Buttons */}
        <div className="grid grid-cols-1 md:grid-cols-3 gap-3 mt-4">
          {[
            {
              id: 'external+self' as SubStoreMode,
              title: 'external+self (机场 + 自有)',
              desc: '主流混合模式：包含外部商业机场节点与自建线路机/家宽机，兼顾大带宽与隐私落地',
              activeColor: 'border-blue-600 bg-blue-50/50 ring-1 ring-blue-600',
            },
            {
              id: 'self' as SubStoreMode,
              title: 'self (纯自有专线 - V2Ray强保密)',
              desc: '自建专线纯净模式：全量输出自建 ACCK、IIJ、软银等 V2Ray 强保密节点，绝对干净安全',
              activeColor: 'border-emerald-600 bg-emerald-50/50 ring-1 ring-emerald-600',
            },
            {
              id: 'external' as SubStoreMode,
              title: 'external (纯机场节点)',
              desc: '纯外部资源模式：仅分发第三方商业订阅节点',
              activeColor: 'border-indigo-600 bg-indigo-50/50 ring-1 ring-indigo-600',
            },
          ].map((item) => {
            const isCurrent = modeInfo?.current_mode === item.id;
            return (
              <div
                key={item.id}
                onClick={() => handleSwitchMode(item.id)}
                className={`p-4 rounded-xl border transition cursor-pointer relative ${
                  isCurrent ? item.activeColor : 'border-slate-200 hover:border-slate-300 bg-white'
                }`}
              >
                <div className="flex items-center justify-between mb-1.5">
                  <span className="font-bold text-xs text-slate-900">{item.title}</span>
                  {isCurrent && (
                    <span className="inline-flex items-center px-2 py-0.5 rounded text-2xs font-semibold bg-blue-600 text-white">
                      当前生效
                    </span>
                  )}
                </div>
                <p className="text-2xs text-slate-500 leading-relaxed">{item.desc}</p>
              </div>
            );
          })}
        </div>
      </div>

      {/* 2. External Subscriptions Management Card (PR #20 / Issue #19 新增展现) */}
      <div className="bg-white border border-slate-200 rounded-xl p-5 shadow-2xs">
        <div className="flex items-center justify-between pb-3 border-b border-slate-100">
          <div>
            <div className="flex items-center space-x-2">
              <Globe className="w-5 h-5 text-indigo-600" />
              <h3 className="text-sm font-bold text-slate-900">外部商业订阅与第三方机场纳管</h3>
            </div>
            <p className="text-xs text-slate-500 mt-0.5">
              直接传入机场订阅 URL 联动 Sub-Store 自动挂载，支持全动态增删与状态开关
            </p>
          </div>

          <button
            onClick={() => setShowAddExtModal(true)}
            className="inline-flex items-center px-3 py-1.5 text-xs font-semibold rounded-lg text-white bg-indigo-600 hover:bg-indigo-700 transition shadow-2xs"
          >
            <Plus className="w-3.5 h-3.5 mr-1" />
            添加外部订阅
          </button>
        </div>

        {/* External Subs List */}
        <div className="mt-4 space-y-3">
          {extSubs?.details && extSubs.details.length > 0 ? (
            <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
              {extSubs.details.map((sub) => (
                <div
                  key={sub.name}
                  className={`p-3.5 rounded-xl border transition flex items-center justify-between ${
                    sub.enabled ? 'bg-slate-50/70 border-slate-200' : 'bg-slate-100/50 border-slate-200 opacity-60'
                  }`}
                >
                  <div className="space-y-1 pr-3 truncate">
                    <div className="flex items-center space-x-2">
                      <span className="font-bold text-xs text-slate-900">{sub.display_name || sub.name}</span>
                      <span className="font-mono text-2xs px-1.5 py-0.5 rounded bg-slate-200 text-slate-700">
                        {sub.name}
                      </span>
                      {sub.enabled ? (
                        <span className="text-2xs px-1.5 py-0.5 rounded bg-emerald-100 text-emerald-800 font-semibold">
                          已启用
                        </span>
                      ) : (
                        <span className="text-2xs px-1.5 py-0.5 rounded bg-slate-200 text-slate-600 font-semibold">
                          已停用
                        </span>
                      )}
                    </div>
                    {sub.url && (
                      <p className="text-2xs font-mono text-slate-500 truncate" title={sub.url}>
                        URL: {sub.url}
                      </p>
                    )}
                  </div>

                  <div className="flex items-center space-x-1.5 flex-shrink-0">
                    <button
                      onClick={() => handleToggleExtSource(sub)}
                      disabled={actionLoading === `toggle-${sub.name}`}
                      title={sub.enabled ? '停用此外部源' : '启用此外部源'}
                      className={`p-1.5 rounded-lg border transition ${
                        sub.enabled 
                          ? 'text-emerald-700 border-emerald-200 bg-emerald-50 hover:bg-emerald-100' 
                          : 'text-slate-500 border-slate-200 bg-white hover:bg-slate-50'
                      }`}
                    >
                      <Power className="w-3.5 h-3.5" />
                    </button>
                    <button
                      onClick={() => handleDeleteExtSource(sub.name)}
                      disabled={actionLoading === `del-${sub.name}`}
                      title="解绑并从 Sub-Store 物理删除"
                      className="p-1.5 rounded-lg border border-red-200 text-red-600 bg-red-50 hover:bg-red-100 transition"
                    >
                      <Trash2 className="w-3.5 h-3.5" />
                    </button>
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <div className="text-center text-slate-400 py-6 text-xs bg-slate-50 rounded-xl border border-dashed border-slate-200">
              当前尚未纳管外部机场，点击右上角「添加外部订阅」直接填入 URL 一键纳管！
            </div>
          )}
        </div>
      </div>

      {/* 3. Pre-rendered Snapshot & Instant Delivery (PR #22 / Issue #21 毫秒级直出展现) */}
      <div className="bg-white border border-slate-200 rounded-xl p-5 shadow-2xs space-y-4">
        <div className="flex flex-col md:flex-row md:items-center md:justify-between gap-4 pb-3 border-b border-slate-100">
          <div>
            <div className="flex items-center space-x-2">
              <Zap className="w-5 h-5 text-amber-500" />
              <h3 className="text-sm font-bold text-slate-900">预烘焙快照极速直出大盘 (1.3ms 零计算风暴)</h3>
              <span className="px-2 py-0.5 rounded-full text-2xs font-semibold bg-emerald-100 text-emerald-800">
                推荐客户端生产使用
              </span>
            </div>
            <p className="text-xs text-slate-500 mt-0.5">
              彻底告别 30s 现场即时计算与 300% CPU 假死，后台静默烘焙，客户端请求直接读纯静态流秒级返回
            </p>
          </div>

          <button
            onClick={handleWarmupSnapshots}
            disabled={actionLoading === 'warmup'}
            className="inline-flex items-center px-3 py-1.5 text-xs font-semibold rounded-lg text-slate-700 bg-white border border-slate-200 hover:bg-slate-50 disabled:opacity-50 transition shadow-2xs"
          >
            {actionLoading === 'warmup' ? (
              <Loader2 className="w-3.5 h-3.5 mr-1.5 animate-spin text-amber-600" />
            ) : (
              <RefreshCw className="w-3.5 h-3.5 mr-1.5 text-amber-600" />
            )}
            立即重新预热烘焙
          </button>
        </div>

        {/* Snapshot Cards */}
        {snapshots?.snapshots && Object.keys(snapshots.snapshots).length > 0 ? (
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            {Object.entries(snapshots.snapshots).map(([name, meta]: [string, any]) => {
              const instantUrl = getInstantSnapshotUrl(name);
              return (
                <div key={name} className="p-4 rounded-xl border border-slate-200 bg-slate-50/60 space-y-3">
                  <div className="flex items-center justify-between">
                    <div className="flex items-center space-x-2">
                      <span className="font-bold text-sm text-slate-900 font-mono">{name}</span>
                      <span className="text-2xs font-semibold px-2 py-0.5 rounded bg-amber-100 text-amber-800 flex items-center">
                        <Zap className="w-3 h-3 mr-1" />
                        直出 1.3ms
                      </span>
                    </div>
                    <span className="text-2xs font-mono text-slate-400">
                      节点数: <strong className="text-slate-800">{meta.proxy_count}</strong>
                    </span>
                  </div>

                  <div className="grid grid-cols-3 gap-2 text-2xs font-mono bg-white p-2.5 rounded-lg border border-slate-200">
                    <div>
                      <span className="text-slate-400 block">快照大小</span>
                      <span className="font-semibold text-slate-700">{meta.size_human}</span>
                    </div>
                    <div>
                      <span className="text-slate-400 block">烘焙耗时</span>
                      <span className="font-semibold text-slate-700">{meta.duration_ms}ms</span>
                    </div>
                    <div>
                      <span className="text-slate-400 block">生成时间</span>
                      <span className="font-semibold text-slate-700">{meta.updated_at_str.split(' ')[1] || meta.updated_at_str}</span>
                    </div>
                  </div>

                  {/* Instant Delivery URL with Copy */}
                  <div className="space-y-1">
                    <label className="text-2xs font-semibold text-slate-600 flex items-center">
                      <ShieldCheck className="w-3 h-3 mr-1 text-emerald-600" />
                      毫秒直出分享链接 (含 ETag 304 极速协商):
                    </label>
                    <div className="p-2 bg-white rounded border border-slate-200 font-mono text-2xs text-slate-600 break-all select-all flex items-center justify-between">
                      <span className="truncate pr-2">{instantUrl}</span>
                      <button
                        onClick={() => handleCopy(instantUrl, `instant-${name}`)}
                        className="text-amber-600 hover:text-amber-800 text-xs font-medium ml-1 flex-shrink-0"
                      >
                        {copiedLink === `instant-${name}` ? (
                          <Check className="w-4 h-4 text-emerald-600" />
                        ) : (
                          <Copy className="w-4 h-4" />
                        )}
                      </button>
                    </div>
                  </div>
                </div>
              );
            })}
          </div>
        ) : (
          <div className="text-center text-slate-400 py-6 text-xs bg-slate-50 rounded-xl border border-dashed border-slate-200">
            暂无落盘快照，正在等待后台定时烘焙或点击上方「立即预热」立即生成！
          </div>
        )}
      </div>

      {/* 4. Sub-Store Origin Live Sharing Views */}
      <div className="bg-white border border-slate-200 rounded-xl shadow-2xs overflow-hidden">
        <div className="px-5 py-3.5 border-b border-slate-200 bg-slate-50 flex items-center justify-between">
          <div className="flex items-center space-x-2">
            <Share2 className="w-4 h-4 text-blue-600" />
            <h3 className="text-xs font-bold text-slate-900 uppercase tracking-wider">
              Sub-Store 源站动态计算订阅链接 (用于算子调试)
            </h3>
          </div>
          <span className="text-2xs font-mono text-slate-500">
            仅作为测试参考，生产拉取请使用上方极速直出链接
          </span>
        </div>

        <div className="p-5 space-y-4">
          <div className="grid grid-cols-1 md:grid-cols-2 gap-3 text-xs">
            {manifest?.share_links && manifest.share_links.length > 0 ? (
              manifest.share_links.map((share, idx) => (
                <div key={idx} className="p-3.5 bg-slate-50 border border-slate-200 rounded-xl space-y-2">
                  <div className="flex items-center justify-between">
                    <span className="font-bold text-slate-900 font-mono text-xs">{share.name}</span>
                    <span className="text-2xs px-2 py-0.5 rounded bg-blue-100 text-blue-800 font-mono">
                      {share.type || 'file'}
                    </span>
                  </div>
                  <div className="p-2 bg-white rounded border border-slate-200 font-mono text-2xs text-slate-600 break-all select-all flex items-center justify-between">
                    <span className="truncate pr-2">{share.full_url || share.url}</span>
                    <button
                      onClick={() => handleCopy(share.full_url || share.url, `share-${idx}`)}
                      className="text-blue-600 hover:text-blue-800 text-xs font-medium ml-1 flex-shrink-0"
                    >
                      {copiedLink === `share-${idx}` ? (
                        <Check className="w-4 h-4 text-emerald-600" />
                      ) : (
                        <Copy className="w-4 h-4" />
                      )}
                    </button>
                  </div>
                </div>
              ))
            ) : (
              <div className="col-span-2 text-center text-slate-400 py-4 text-xs">
                正在从 SubStore 远端拉取订阅分享链接...
              </div>
            )}
          </div>
        </div>
      </div>

      {/* 5. Bottom Management Workbench */}
      <div className="bg-white border border-slate-200 rounded-xl shadow-2xs overflow-hidden">
        {/* Workspace Tab Nav */}
        <div className="flex border-b border-slate-200 bg-slate-50 px-4">
          <button
            onClick={() => setActiveTab('orchestration')}
            className={`py-3 px-4 text-xs font-bold border-b-2 transition ${
              activeTab === 'orchestration'
                ? 'border-blue-600 text-blue-600 bg-white'
                : 'border-transparent text-slate-500 hover:text-slate-800'
            }`}
          >
            🎯 动态规则编排与五维策略 (Plan B)
          </button>
          <button
            onClick={() => setActiveTab('nodes')}
            className={`py-3 px-4 text-xs font-bold border-b-2 transition ${
              activeTab === 'nodes'
                ? 'border-blue-600 text-blue-600 bg-white'
                : 'border-transparent text-slate-500 hover:text-slate-800'
            }`}
          >
            自建专线节点在线维护 (V2Ray强保密)
          </button>
          <button
            onClick={() => setActiveTab('scripts')}
            className={`py-3 px-4 text-xs font-bold border-b-2 transition ${
              activeTab === 'scripts'
                ? 'border-blue-600 text-blue-600 bg-white'
                : 'border-transparent text-slate-500 hover:text-slate-800'
            }`}
          >
            Sub-Store 算子脚本热补丁编辑器
          </button>
        </div>

        {/* Tab 0: Dynamic Orchestration & Plan B */}
        {activeTab === 'orchestration' && (
          <div className="p-5 space-y-6">
            {/* Action Bar */}
            <div className="flex flex-wrap items-center justify-between gap-3 bg-slate-50 p-4 rounded-xl border border-slate-200">
              <div className="space-y-1">
                <div className="text-xs font-bold text-slate-800 flex items-center space-x-2">
                  <Zap className="w-4 h-4 text-amber-500" />
                  <span>方案B：线路机全网状池化中继 & 自建五维策略</span>
                </div>
                <div className="text-2xs text-slate-500">
                  各区域线路专线常驻预热中继至对应家宽，毫秒秒切；出口送中强隔离；原子 API 灵活组合与动态规则持久化。
                </div>
              </div>
              <div className="flex items-center space-x-2">
                <button
                  onClick={handleRebuildChains}
                  disabled={actionLoading !== null}
                  className="px-3 py-1.5 bg-blue-600 hover:bg-blue-700 text-white rounded text-xs font-bold flex items-center space-x-1.5 shadow-2xs transition disabled:opacity-50"
                >
                  {actionLoading === 'rebuild-chains' ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <RotateCw className="w-3.5 h-3.5" />}
                  <span>重建线路机全网状池</span>
                </button>
                <button
                  onClick={handleSyncFiveGroups}
                  disabled={actionLoading !== null}
                  className="px-3 py-1.5 bg-indigo-600 hover:bg-indigo-700 text-white rounded text-xs font-bold flex items-center space-x-1.5 shadow-2xs transition disabled:opacity-50"
                >
                  {actionLoading === 'sync-five' ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Layers className="w-3.5 h-3.5" />}
                  <span>同步五维策略组</span>
                </button>
              </div>
            </div>

            {/* Five-Group Summary Cards */}
            {categories && (
              <div className="grid grid-cols-1 md:grid-cols-4 gap-3">
                <div className="p-3 bg-blue-50/50 border border-blue-200 rounded-lg">
                  <div className="text-2xs font-bold text-blue-800">🇺🇸 美国与美国家宽 (ashan-us-resi)</div>
                  <div className="text-lg font-black text-blue-900 mt-1">
                    {categories.categories.us?.total_clean_count || 0}
                    <span className="text-2xs font-normal text-blue-700 ml-1">干净节点</span>
                  </div>
                  <div className="text-2xs text-blue-600 mt-1 space-x-1">
                    <span>线路: {categories.categories.us?.transit_count || 0}</span>
                    <span>• 家宽: {categories.categories.us?.residential_count || 0}</span>
                    <span>• 链式: {categories.categories.us?.chain_count || 0}</span>
                  </div>
                </div>

                <div className="p-3 bg-emerald-50/50 border border-emerald-200 rounded-lg">
                  <div className="text-2xs font-bold text-emerald-800">🇯🇵 日本与日本家宽 (ashan-jp-resi)</div>
                  <div className="text-lg font-black text-emerald-900 mt-1">
                    {categories.categories.jp?.total_clean_count || 0}
                    <span className="text-2xs font-normal text-emerald-700 ml-1">干净节点</span>
                  </div>
                  <div className="text-2xs text-emerald-600 mt-1 space-x-1">
                    <span>线路: {categories.categories.jp?.transit_count || 0}</span>
                    <span>• 家宽: {categories.categories.jp?.residential_count || 0}</span>
                    <span>• 链式: {categories.categories.jp?.chain_count || 0}</span>
                  </div>
                </div>

                <div className="p-3 bg-purple-50/50 border border-purple-200 rounded-lg">
                  <div className="text-2xs font-bold text-purple-800">⚡ 自建高速专线 (ashan-transit)</div>
                  <div className="text-lg font-black text-purple-900 mt-1">
                    {((categories.categories.us?.transit_count || 0) + (categories.categories.jp?.transit_count || 0) + (categories.categories.other?.transit_count || 0))}
                    <span className="text-2xs font-normal text-purple-700 ml-1">条直连专线</span>
                  </div>
                  <div className="text-2xs text-purple-600 mt-1">
                    纯 VLESS Reality / CF-WS 待命自动隐藏
                  </div>
                </div>

                <div className="p-3 bg-rose-50/50 border border-rose-200 rounded-lg">
                  <div className="text-2xs font-bold text-rose-800 flex items-center space-x-1">
                    <Shield className="w-3.5 h-3.5 text-rose-600" />
                    <span>🛡️ 出口送中隔离 (ashan-china-leak)</span>
                  </div>
                  <div className="text-lg font-black text-rose-900 mt-1">
                    {categories.categories.china_leaks?.length || 0}
                    <span className="text-2xs font-normal text-rose-700 ml-1">个送中节点</span>
                  </div>
                  <div className="text-2xs text-rose-600 mt-1">
                    强隔离运行，永不污染干净组
                  </div>
                </div>
              </div>
            )}

            {/* Formula Evaluator & Group Creator */}
            <div className="bg-slate-50/70 border border-slate-200 rounded-xl p-4 space-y-4">
              <div className="flex items-center justify-between">
                <div className="flex items-center space-x-2">
                  <Sliders className="w-4 h-4 text-blue-600" />
                  <span className="text-xs font-bold text-slate-800">动态组合公式求值器 (连接 = 美国家宽 + 美国线路机 + 线路家宽机)</span>
                </div>
                <div className="text-2xs text-slate-500">
                  支持按需组合原子节点并实时试算预览
                </div>
              </div>

              {/* Filters Form */}
              <div className="grid grid-cols-1 md:grid-cols-4 gap-3 text-xs">
                <div>
                  <label className="text-2xs font-bold text-slate-600 block mb-1">目标国家/地区</label>
                  <select
                    value={evalFilter.countries?.[0] || 'ALL'}
                    onChange={(e) => {
                      const val = e.target.value;
                      setEvalFilter({ ...evalFilter, countries: val === 'ALL' ? undefined : [val] });
                    }}
                    className="w-full bg-white border border-slate-300 rounded px-2.5 py-1.5 text-xs text-slate-700"
                  >
                    <option value="ALL">全部地区 (All Regions)</option>
                    <option value="US">🇺🇸 美国 (US)</option>
                    <option value="JP">🇯🇵 日本 (JP)</option>
                  </select>
                </div>

                <div className="flex flex-col justify-end space-y-1.5">
                  <label className="flex items-center space-x-1.5 text-xs text-slate-700 cursor-pointer">
                    <input
                      type="checkbox"
                      checked={evalFilter.include_transit}
                      onChange={(e) => setEvalFilter({ ...evalFilter, include_transit: e.target.checked })}
                      className="rounded border-slate-300 text-blue-600"
                    />
                    <span>包含线路机 (Transit)</span>
                  </label>
                  <label className="flex items-center space-x-1.5 text-xs text-slate-700 cursor-pointer">
                    <input
                      type="checkbox"
                      checked={evalFilter.include_residential}
                      onChange={(e) => setEvalFilter({ ...evalFilter, include_residential: e.target.checked })}
                      className="rounded border-slate-300 text-blue-600"
                    />
                    <span>包含直连家宽 (Residential)</span>
                  </label>
                </div>

                <div className="flex flex-col justify-end space-y-1.5">
                  <label className="flex items-center space-x-1.5 text-xs text-slate-700 cursor-pointer">
                    <input
                      type="checkbox"
                      checked={evalFilter.include_chain}
                      onChange={(e) => setEvalFilter({ ...evalFilter, include_chain: e.target.checked })}
                      className="rounded border-slate-300 text-blue-600"
                    />
                    <span>包含链式中继 (线路机拉家宽)</span>
                  </label>
                  <label className="flex items-center space-x-1.5 text-xs text-slate-700 cursor-pointer">
                    <input
                      type="checkbox"
                      checked={evalFilter.exclude_china_leak}
                      onChange={(e) => setEvalFilter({ ...evalFilter, exclude_china_leak: e.target.checked })}
                      className="rounded border-slate-300 text-blue-600"
                    />
                    <span>强排除送中 (Clean Only)</span>
                  </label>
                </div>

                <div className="flex items-end space-x-2">
                  <button
                    onClick={handleRunEvaluate}
                    disabled={evalLoading}
                    className="w-full py-1.5 bg-slate-800 hover:bg-slate-900 text-white rounded text-xs font-bold flex items-center justify-center space-x-1 transition disabled:opacity-50"
                  >
                    {evalLoading ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <RotateCw className="w-3.5 h-3.5" />}
                    <span>实时试算连接</span>
                  </button>
                </div>
              </div>

              {/* Evaluate Results View */}
              {evalResults.length > 0 && (
                <div className="bg-white border border-slate-200 rounded-lg p-3 space-y-2">
                  <div className="text-2xs font-bold text-slate-600 flex items-center justify-between">
                    <span>试算匹配结果 (共 {evalResults.length} 条连接):</span>
                    <button onClick={() => setEvalResults([])} className="text-slate-400 hover:text-slate-600">收起</button>
                  </div>
                  <div className="max-h-36 overflow-y-auto space-y-1 font-mono text-2xs text-slate-600 bg-slate-50 p-2 rounded">
                    {evalResults.map((u, i) => (
                      <div key={i} className="truncate select-all">{u}</div>
                    ))}
                  </div>

                  {/* Save to Group Form */}
                  <div className="pt-2 border-t border-slate-100 flex flex-wrap items-center gap-2">
                    <input
                      type="text"
                      placeholder="系统英文标识 (如 ashan-us-bundle)"
                      value={newGroupName}
                      onChange={(e) => setNewGroupName(e.target.value)}
                      className="bg-white border border-slate-300 rounded px-2 py-1 text-xs text-slate-700 w-52"
                    />
                    <input
                      type="text"
                      placeholder="显示名称 (如 自建-美区全家桶)"
                      value={newGroupDisplay}
                      onChange={(e) => setNewGroupDisplay(e.target.value)}
                      className="bg-white border border-slate-300 rounded px-2 py-1 text-xs text-slate-700 w-52"
                    />
                    <button
                      onClick={handleSaveGroup}
                      disabled={actionLoading === 'save-group'}
                      className="px-3 py-1 bg-emerald-600 hover:bg-emerald-700 text-white rounded text-xs font-bold flex items-center space-x-1 shadow-2xs transition disabled:opacity-50"
                    >
                      {actionLoading === 'save-group' ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Plus className="w-3.5 h-3.5" />}
                      <span>保存为自定义分组</span>
                    </button>
                  </div>
                </div>
              )}
            </div>

            {/* Custom Groups Table */}
            <div className="space-y-3">
              <div className="text-xs font-bold text-slate-700 flex items-center justify-between">
                <span>已保存的自定义动态分组 (Sub-Store 独立订阅清单)</span>
                <span className="text-2xs font-normal text-slate-400">数量: {customGroups.length}</span>
              </div>
              {customGroups.length === 0 ? (
                <div className="text-center py-6 text-slate-400 text-xs bg-slate-50 rounded-lg border border-slate-200">
                  暂未创建自定义分组，上方试算后可一键保存并自动挂载至 Sub-Store。
                </div>
              ) : (
                <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                  {customGroups.map((g) => (
                    <div key={g.name} className="p-3 bg-white border border-slate-200 rounded-lg shadow-2xs space-y-2">
                      <div className="flex items-center justify-between">
                        <div className="font-bold text-xs text-slate-800">
                          {g.display_name}
                          <span className="text-2xs font-mono text-slate-400 font-normal ml-1.5">({g.name})</span>
                        </div>
                        <button
                          onClick={() => handleDeleteGroup(g.name)}
                          disabled={actionLoading !== null}
                          className="text-slate-400 hover:text-rose-600 transition"
                          title="删除此分组"
                        >
                          <Trash2 className="w-3.5 h-3.5" />
                        </button>
                      </div>
                      <div className="text-2xs text-slate-500 space-x-2">
                        <span>地区: {g.filter?.countries?.join(',') || '全部'}</span>
                        <span>• 线路机: {g.filter?.include_transit ? '✅' : '❌'}</span>
                        <span>• 家宽: {g.filter?.include_residential ? '✅' : '❌'}</span>
                        <span>• 链式中继: {g.filter?.include_chain ? '✅' : '❌'}</span>
                      </div>
                      <div className="text-2xs text-slate-400 pt-1 border-t border-slate-100 flex items-center justify-between">
                        <span>更新: {g.updated_at || '刚刚'}</span>
                        <span className="text-blue-600 font-mono">已挂载 Sub-Store</span>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>
          </div>
        )}

        {/* Tab 1: Nodes Editor */}
        {activeTab === 'nodes' && (
          <div className="p-5 space-y-4">
            <div className="flex items-center justify-between">
              <div className="flex items-center space-x-2">
                <label className="text-xs font-medium text-slate-600">目标订阅:</label>
                <input
                  type="text"
                  value={subscriptName}
                  onChange={(e) => setSubscriptName(e.target.value)}
                  className="px-2.5 py-1 border border-slate-300 rounded text-xs font-mono font-semibold"
                />
              </div>

              <button
                onClick={handleSaveNodes}
                disabled={actionLoading === 'save-nodes'}
                className="inline-flex items-center px-4 py-1.5 text-xs font-semibold rounded-lg text-white bg-blue-600 hover:bg-blue-700 disabled:opacity-50 transition shadow-2xs"
              >
                {actionLoading === 'save-nodes' ? (
                  <Loader2 className="w-3.5 h-3.5 mr-1.5 animate-spin" />
                ) : (
                  <Save className="w-3.5 h-3.5 mr-1.5" />
                )}
                保存并推送至 SubStore
              </button>
            </div>

            <textarea
              rows={12}
              value={subNodesText}
              onChange={(e) => setSubNodesText(e.target.value)}
              placeholder="每行输入一条完整的自建代理协议链接 (vless://, trojan://, ss://, vmess://) —— 提示：自建对外直连全量要求 V2Ray 高保密协议"
              className="w-full p-3 font-mono text-xs border border-slate-300 rounded-xl focus:outline-none focus:ring-2 focus:ring-blue-600 focus:border-blue-600 bg-slate-50/50 leading-relaxed"
            />
            <div className="text-2xs text-slate-400">
              安全规范提示：所有自建对外节点必须使用 V2Ray / Xray 生态协议（VLESS Reality / Vision / WS TLS）；懒猫云到家宽内部链路允许使用 SS。
            </div>
          </div>
        )}

        {/* Tab 2: Script Hot Editor */}
        {activeTab === 'scripts' && (
          <div className="p-5 space-y-4">
            <div className="flex items-center justify-between">
              <div className="flex items-center space-x-2">
                <FileCode className="w-4 h-4 text-blue-600" />
                <label className="text-xs font-medium text-slate-600">选择脚本:</label>
                <select
                  value={selectedScript}
                  onChange={(e) => setSelectedScript(e.target.value)}
                  className="px-2.5 py-1 border border-slate-300 rounded text-xs font-mono"
                >
                  {manifest?.scripts?.map((s) => (
                    <option key={s.name} value={s.name}>
                      {s.name} ({s.size} bytes)
                    </option>
                  ))}
                </select>
              </div>

              <button
                onClick={handleSaveScript}
                disabled={actionLoading === 'save-script'}
                className="inline-flex items-center px-4 py-1.5 text-xs font-semibold rounded-lg text-white bg-blue-600 hover:bg-blue-700 disabled:opacity-50 transition shadow-2xs"
              >
                {actionLoading === 'save-script' ? (
                  <Loader2 className="w-3.5 h-3.5 mr-1.5 animate-spin" />
                ) : (
                  <Save className="w-3.5 h-3.5 mr-1.5" />
                )}
                热更新部署脚本
              </button>
            </div>

            <textarea
              rows={14}
              value={scriptContent}
              onChange={(e) => setScriptContent(e.target.value)}
              className="w-full p-3 font-mono text-2xs bg-slate-900 text-slate-100 rounded-xl focus:outline-none focus:ring-2 focus:ring-blue-600 border border-slate-800 leading-relaxed"
            />
          </div>
        )}
      </div>

      {/* Modal: Add External Subscription */}
      {showAddExtModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-xs p-4">
          <div className="bg-white rounded-2xl max-w-lg w-full p-6 shadow-xl border border-slate-200 space-y-4">
            <div className="flex items-center justify-between border-b border-slate-100 pb-3">
              <h3 className="text-base font-bold text-slate-900 flex items-center">
                <Plus className="w-5 h-5 mr-1.5 text-indigo-600" />
                添加外部商业机场订阅 (一键联动 Sub-Store)
              </h3>
              <button
                onClick={() => setShowAddExtModal(false)}
                className="text-slate-400 hover:text-slate-600 text-sm font-semibold"
              >
                ✕
              </button>
            </div>

            <form onSubmit={handleAddExternalSub} className="space-y-4 text-xs">
              <div>
                <label className="block font-semibold text-slate-700 mb-1">
                  订阅唯一标识 (Name) <span className="text-red-500">*</span>
                </label>
                <input
                  type="text"
                  required
                  placeholder="例如: fwcloud, airport_a"
                  value={newExtName}
                  onChange={(e) => setNewExtName(e.target.value)}
                  className="w-full px-3 py-2 border border-slate-300 rounded-lg font-mono focus:ring-2 focus:ring-indigo-600 focus:outline-none"
                />
                <span className="text-2xs text-slate-400 mt-1 block">用于 Sub-Store 内部存储与 URL 路由标识</span>
              </div>

              <div>
                <label className="block font-semibold text-slate-700 mb-1">
                  显示别名 (Display Name)
                </label>
                <input
                  type="text"
                  placeholder="例如: 蜂窝云 (香港/新加坡BGP)"
                  value={newExtDisplayName}
                  onChange={(e) => setNewExtDisplayName(e.target.value)}
                  className="w-full px-3 py-2 border border-slate-300 rounded-lg focus:ring-2 focus:ring-indigo-600 focus:outline-none"
                />
              </div>

              <div>
                <label className="block font-semibold text-slate-700 mb-1">
                  第三方机场订阅 URL <span className="text-red-500">*</span>
                </label>
                <textarea
                  required
                  rows={3}
                  placeholder="https://api.example.com/link/token?clash=1"
                  value={newExtUrl}
                  onChange={(e) => setNewExtUrl(e.target.value)}
                  className="w-full px-3 py-2 border border-slate-300 rounded-lg font-mono focus:ring-2 focus:ring-indigo-600 focus:outline-none leading-relaxed"
                />
                <span className="text-2xs text-slate-400 mt-1 block">
                  直接粘贴第三方机场的原生 Clash/V2Ray 链接，系统将自动向 Sub-Store 创建并完成挂载。
                </span>
              </div>

              <div className="flex items-center justify-end space-x-2 pt-3 border-t border-slate-100">
                <button
                  type="button"
                  onClick={() => setShowAddExtModal(false)}
                  className="px-4 py-2 border border-slate-300 rounded-lg text-slate-700 hover:bg-slate-50 font-medium"
                >
                  取消
                </button>
                <button
                  type="submit"
                  disabled={actionLoading === 'add-ext'}
                  className="px-4 py-2 bg-indigo-600 hover:bg-indigo-700 text-white rounded-lg font-semibold flex items-center shadow-2xs disabled:opacity-50"
                >
                  {actionLoading === 'add-ext' ? (
                    <Loader2 className="w-3.5 h-3.5 mr-1.5 animate-spin" />
                  ) : (
                    <Check className="w-3.5 h-3.5 mr-1.5" />
                  )}
                  保存并同步挂载
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
};
