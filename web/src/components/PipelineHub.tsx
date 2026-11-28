import React, { useState, useEffect, useCallback, useMemo } from 'react';
import { 
  Layers, 
  RotateCw, 
  CheckCircle2, 
  AlertTriangle, 
  ShieldCheck, 
  Zap, 
  Globe, 
  Home, 
  Server, 
  Link2, 
  Trash2, 
  ExternalLink,
  ChevronDown,
  ChevronUp,
  Sliders,
  Radio,
  X,
  Play,
  Save,
  Info,
  Lock,
  Plus,
  RefreshCw,
  FileCode,
  Check,
  Copy,
  Terminal,
  Activity,
  Flame,
  ArrowRight
} from 'lucide-react';
import { api, getStoredToken } from '../api/client';
import { PolicyGroupStudio } from './PolicyGroupStudio';
import { SubscriptionTopologyDrawer } from './SubscriptionTopologyDrawer';
import { ConfirmActionButton } from './ConfirmActionButton';
import { useFeedback } from '../context/FeedbackContext';
import type { 
  SubStoreMode,
  SubStoreModeInfo,
  ExternalSubsResponse,
  ExternalSubDetail,
  SnapshotStatusResponse,
  CategoriesSummary,
  SubStoreManifest,
  AtomNode,
  CustomChainBind,
  CustomGroupDef
} from '../types';

interface PipelineHubProps {
  onRefreshGlobal?: () => void;
}

export const PipelineHub: React.FC<PipelineHubProps> = ({ onRefreshGlobal }) => {
  // Global Data States
  const [loading, setLoading] = useState(true);
  const [notice, setNotice] = useState<{ type: 'success' | 'error'; text: string } | null>(null);
  const [actionLoading, setActionLoading] = useState<string | null>(null);

  // Stage 1: Input Pools & Mode States
  const [modeInfo, setModeInfo] = useState<SubStoreModeInfo | null>(null);
  const [externalSubs, setExternalSubs] = useState<ExternalSubDetail[]>([]);
  const [atoms, setAtoms] = useState<AtomNode[]>([]);
  const [customChains, setCustomChains] = useState<CustomChainBind[]>([]);

  // Stage 3: Snapshot & Deliverable States
  const [snapshotStatus, setSnapshotStatus] = useState<SnapshotStatusResponse | null>(null);
  const [drawerSubName, setDrawerSubName] = useState<string | null>(null);

  // Stage 4 / Bottom: Advanced Scripts & Manifest
  const [manifest, setManifest] = useState<SubStoreManifest | null>(null);
  const [showAdvancedOps, setShowAdvancedOps] = useState(false);
  const [selectedScript, setSelectedScript] = useState<string>('custom_patch.js');
  const [scriptContent, setScriptContent] = useState<string>('');
  const [savingScript, setSavingScript] = useState(false);

  // Add External Sub Modal State
  const [showAddExtModal, setShowAddExtModal] = useState(false);
  const [extSubName, setExtSubName] = useState('');
  const [extSubURL, setExtSubURL] = useState('');
  const [extSubEnabled, setExtSubEnabled] = useState(true);

  // Subscription Copy & Direct Download URL helpers
  const [copiedSub, setCopiedSub] = useState<string | null>(null);

  const handleCopySub = (subName: string) => {
    const token = getStoredToken();
    const origin = window.location.origin;
    const url = token
      ? `${origin}/api/sub/file/${subName}?token=${encodeURIComponent(token)}`
      : `${origin}/api/sub/file/${subName}`;
    navigator.clipboard.writeText(url);
    setCopiedSub(subName);
    setTimeout(() => setCopiedSub(null), 2000);
  };

  const getDownloadUrl = (subName: string) => {
    const token = getStoredToken();
    return token
      ? `/api/sub/file/${subName}?token=${encodeURIComponent(token)}`
      : `/api/sub/file/${subName}`;
  };

  // Fetch all pipeline core data
  const loadPipelineData = useCallback(async () => {
    setLoading(true);
    try {
      const [mMode, extData, atomsRes, chainsRes, snapRes, maniRes] = await Promise.all([
        api.getSubStoreMode().catch(() => null),
        api.getExternalSubs().catch(() => null),
        api.getAtoms({}).catch(() => ({ atoms: [], count: 0, status: '' })),
        api.getCustomChains().catch(() => ({ chains: [], count: 0, status: '' })),
        api.getSnapshotStatus().catch(() => null),
        api.getSubStoreManifest().catch(() => null),
      ]);

      if (mMode) setModeInfo(mMode);
      if (extData && extData.details) setExternalSubs(extData.details);
      if (atomsRes) setAtoms(atomsRes.atoms || []);
      if (chainsRes) setCustomChains(chainsRes.chains || []);
      if (snapRes) setSnapshotStatus(snapRes);
      if (maniRes) setManifest(maniRes);
    } catch (err: any) {
      setNotice({ type: 'error', text: `加载编排数据失败: ${err.message}` });
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadPipelineData();
  }, [loadPipelineData]);

  // Load script content when selected
  useEffect(() => {
    if (selectedScript && showAdvancedOps) {
      api.getScriptContent(selectedScript)
        .then(setScriptContent)
        .catch((err: any) => setScriptContent(`// 无法加载脚本内容: ${err?.message || '请求失败'}`));
    }
  }, [selectedScript, showAdvancedOps]);

  // Mode Switch
  const handleSwitchMode = async (newMode: SubStoreMode) => {
    setActionLoading('switch_mode');
    try {
      await api.setSubStoreMode(newMode);
      setNotice({ type: 'success', text: `订阅合成模式已原子切换至: ${newMode}` });
      await loadPipelineData();
      onRefreshGlobal?.();
    } catch (err: any) {
      setNotice({ type: 'error', text: `切换模式失败: ${err.message}` });
    } finally {
      setActionLoading(null);
    }
  };

  // Warmup Snapshots
  const handleWarmup = async (files = ['Q', 'ashan', 'business']) => {
    setActionLoading('warmup');
    try {
      await api.warmupSnapshot(files);
      setNotice({ type: 'success', text: `已触发 ${files.join(' & ')} 快照异步预热！` });
      setTimeout(async () => {
        const snap = await api.getSnapshotStatus().catch(() => null);
        if (snap) setSnapshotStatus(snap);
      }, 2500);
    } catch (err: any) {
      setNotice({ type: 'error', text: `预热快照失败: ${err.message}` });
    } finally {
      setActionLoading(null);
    }
  };

  // Rebuild Full Mesh Chains
  const handleRebuildMesh = async () => {
    setActionLoading('rebuild_mesh');
    try {
      const res = await api.rebuildFullMeshChains();
      setNotice({ type: 'success', text: res.message || '全网网状中继重建完毕！' });
      await loadPipelineData();
    } catch (err: any) {
      setNotice({ type: 'error', text: `重建失败: ${err.message}` });
    } finally {
      setActionLoading(null);
    }
  };

  // Sync Five Groups
  const handleSyncFiveGroups = async () => {
    setActionLoading('sync_five');
    try {
      const res = await api.syncFiveGroups();
      setNotice({ type: 'success', text: res.message || '五维策略组全量同步成功！' });
      await loadPipelineData();
    } catch (err: any) {
      setNotice({ type: 'error', text: `同步失败: ${err.message}` });
    } finally {
      setActionLoading(null);
    }
  };

  // Upsert External Sub
  const handleSaveExtSub = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!extSubName.trim() || !extSubURL.trim()) return;

    setActionLoading('save_ext');
    try {
      await api.upsertExternalSub({
        name: extSubName.trim(),
        display_name: extSubName.trim(),
        url: extSubURL.trim(),
        enabled: extSubEnabled,
        auto_mount: true,
      });
      setNotice({ type: 'success', text: `已成功保存并挂载外部订阅: ${extSubName}` });
      setShowAddExtModal(false);
      setExtSubName('');
      setExtSubURL('');
      await loadPipelineData();
    } catch (err: any) {
      setNotice({ type: 'error', text: `保存外部订阅失败: ${err.message}` });
    } finally {
      setActionLoading(null);
    }
  };

  // Delete External Sub
  const handleDeleteExtSub = async (name: string) => {
    try {
      await api.deleteExternalSub(name, true);
      setNotice({ type: 'success', text: `已移除外部订阅: ${name}` });
      await loadPipelineData();
    } catch (err: any) {
      setNotice({ type: 'error', text: `删除失败: ${err.message}` });
    }
  };

  // Toggle External Sub Active
  const handleToggleExtSub = async (sub: ExternalSubDetail) => {
    try {
      await api.upsertExternalSub({
        name: sub.name,
        display_name: sub.display_name,
        url: sub.url,
        enabled: !sub.enabled,
      });
      await loadPipelineData();
    } catch (err: any) {
      setNotice({ type: 'error', text: `更新状态失败: ${err.message}` });
    }
  };

  // Update Script
  const handleUpdateScript = async () => {
    setSavingScript(true);
    try {
      await api.updateScript(selectedScript, scriptContent);
      setNotice({ type: 'success', text: `脚本 ${selectedScript} 已热更新并部署上线！` });
    } catch (err: any) {
      setNotice({ type: 'error', text: `更新脚本失败: ${err.message}` });
    } finally {
      setSavingScript(false);
    }
  };

  // Node pool statistics
  const selfTransitCount = useMemo(() => atoms.filter(a => a.kind === 'transit').length, [atoms]);
  const selfResiCount = useMemo(() => atoms.filter(a => a.kind === 'residential').length, [atoms]);
  const chainCount = useMemo(() => customChains.length, [customChains]);

  return (
    <div className="space-y-8">
      {/* Notice Banner */}
      {notice && (
        <div className={`p-4 rounded-xl border flex items-center justify-between text-xs transition ${
          notice.type === 'success' 
            ? 'bg-emerald-50 border-emerald-200 text-emerald-800' 
            : 'bg-red-50 border-red-200 text-red-800'
        }`}>
          <div className="flex items-center space-x-2">
            {notice.type === 'success' ? <CheckCircle2 className="w-4 h-4 text-emerald-600" /> : <AlertTriangle className="w-4 h-4 text-red-600" />}
            <span className="font-semibold">{notice.text}</span>
          </div>
          <button onClick={() => setNotice(null)} className="p-1 hover:opacity-75">
            <X className="w-3.5 h-3.5" />
          </button>
        </div>
      )}

      {/* TOP HEADER: PIPELINE ARCHITECTURE HERO & MODE SELECTOR */}
      <div className="bg-white border border-slate-200 rounded-2xl p-6 shadow-2xs">
        <div className="flex flex-col md:flex-row md:items-center justify-between gap-4">
          <div>
            <div className="flex items-center space-x-2.5">
              <div className="w-8 h-8 rounded-lg bg-blue-600 text-white flex items-center justify-center font-bold">
                <Layers className="w-4 h-4" />
              </div>
              <h1 className="text-base font-bold text-slate-900 tracking-tight">
                订阅与策略编排枢纽 (Pipeline Hub)
              </h1>
            </div>
            <p className="text-xs text-slate-500 mt-1 max-w-2xl">
              三段式全景管道：从【节点输入池】到【外部/自建策略组编排】，直达【Q / ashan 双轨成型与全量连通性验证】。
            </p>
          </div>

          {/* 3-Mode Pill Switcher */}
          <div className="bg-slate-100 p-1 rounded-xl flex items-center space-x-1 border border-slate-200 self-start md:self-auto">
            <button
              onClick={() => handleSwitchMode('external+self')}
              disabled={actionLoading === 'switch_mode'}
              className={`px-3 py-1.5 text-xs font-semibold rounded-lg transition ${
                modeInfo?.current_mode === 'external+self'
                  ? 'bg-blue-600 text-white shadow-2xs'
                  : 'text-slate-600 hover:text-slate-900'
              }`}
            >
              融合双轨 (自建+商业)
            </button>
            <button
              onClick={() => handleSwitchMode('self')}
              disabled={actionLoading === 'switch_mode'}
              className={`px-3 py-1.5 text-xs font-semibold rounded-lg transition ${
                modeInfo?.current_mode === 'self'
                  ? 'bg-blue-600 text-white shadow-2xs'
                  : 'text-slate-600 hover:text-slate-900'
              }`}
            >
              仅自建节点
            </button>
            <button
              onClick={() => handleSwitchMode('external')}
              disabled={actionLoading === 'switch_mode'}
              className={`px-3 py-1.5 text-xs font-semibold rounded-lg transition ${
                modeInfo?.current_mode === 'external'
                  ? 'bg-blue-600 text-white shadow-2xs'
                  : 'text-slate-600 hover:text-slate-900'
              }`}
            >
              仅外部商业
            </button>
          </div>
        </div>
      </div>

      {/* ============================================================== */}
      {/* 阶段一：自建与外部节点输入源 (INPUT SOURCES & ATOMS) */}
      {/* ============================================================== */}
      <section className="space-y-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center space-x-2">
            <span className="w-5 h-5 rounded-full bg-blue-100 text-blue-700 text-2xs font-bold flex items-center justify-center">
              1
            </span>
            <h2 className="text-sm font-bold text-slate-900 tracking-tight">
              输入源节点池 (自建基础设施 + 外部商业机场源)
            </h2>
          </div>
          <span className="text-2xs text-slate-400">
            全部自建原子经沙箱实时巡检 · 外部机场仅按需引入不测速
          </span>
        </div>

        <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
          {/* Card 1: Self-Hosted Transit Backbone */}
          <div className="bg-white border border-slate-200 rounded-xl p-4 shadow-2xs space-y-2">
            <div className="flex items-center justify-between">
              <div className="flex items-center space-x-2">
                <Radio className="w-4 h-4 text-blue-600" />
                <span className="font-bold text-xs text-slate-900">自建骨干专线池</span>
              </div>
              <span className="text-2xs font-mono px-2 py-0.5 rounded bg-blue-50 text-blue-700 font-semibold border border-blue-200">
                Reality Vision
              </span>
            </div>
            <div className="flex items-baseline space-x-2">
              <span className="text-2xl font-bold font-mono text-slate-900">{selfTransitCount}</span>
              <span className="text-2xs text-slate-400">条骨干直连</span>
            </div>
            <p className="text-2xs text-slate-500">
              美西 4837、东京 IIJ、东京软银等全线 VLESS Reality 专线，支持 CF-WS 443 自动救援。
            </p>
          </div>

          {/* Card 2: Self-Hosted Residential & Chains */}
          <div className="bg-white border border-slate-200 rounded-xl p-4 shadow-2xs space-y-2">
            <div className="flex items-center justify-between">
              <div className="flex items-center space-x-2">
                <Home className="w-4 h-4 text-purple-600" />
                <span className="font-bold text-xs text-slate-900">原生家宽与链式池</span>
              </div>
              <span className="text-2xs font-mono px-2 py-0.5 rounded bg-purple-50 text-purple-700 font-semibold border border-purple-200">
                Mesh Chaining
              </span>
            </div>
            <div className="flex items-baseline space-x-2">
              <span className="text-2xl font-bold font-mono text-purple-700">{selfResiCount}</span>
              <span className="text-2xs text-slate-400">家宽原子 · {chainCount} 条网状绑定</span>
            </div>
            <p className="text-2xs text-slate-500">
              美拉美、日拉日原生家宽 Socks5，网状智能中继优化 RTT 降幅 60%+，解锁全量 AI。
            </p>
          </div>

          {/* Card 3: External Commercial Subscriptions */}
          <div className="bg-white border border-slate-200 rounded-xl p-4 shadow-2xs space-y-2">
            <div className="flex items-center justify-between">
              <div className="flex items-center space-x-2">
                <Globe className="w-4 h-4 text-emerald-600" />
                <span className="font-bold text-xs text-slate-900">外部商业机场池</span>
              </div>
              <button
                onClick={() => setShowAddExtModal(true)}
                className="text-2xs font-semibold px-2 py-0.5 rounded bg-emerald-50 text-emerald-700 hover:bg-emerald-100 border border-emerald-200 transition flex items-center"
              >
                <Plus className="w-3 h-3 mr-0.5" />
                添加机场
              </button>
            </div>
            <div className="flex items-baseline space-x-2">
              <span className="text-2xl font-bold font-mono text-emerald-700">{externalSubs.length}</span>
              <span className="text-2xs text-slate-400">个订阅源生效中</span>
            </div>
            <p className="text-2xs text-slate-500">
              商业订阅与自建策略物理隔离，作为海量备选池保障全天候可用性。
            </p>
          </div>
        </div>

        {/* External Subscriptions Management Strip */}
        {externalSubs.length > 0 && (
          <div className="bg-white border border-slate-200 rounded-xl p-4 shadow-2xs space-y-3">
            <div className="flex items-center justify-between border-b border-slate-100 pb-2">
              <span className="font-bold text-xs text-slate-800">已纳管外部商业机场清单</span>
              <span className="text-2xs text-slate-400">支持独立启用/停用与物理删除</span>
            </div>
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3">
              {externalSubs.map((sub) => (
                <div
                  key={sub.name}
                  className={`p-3 rounded-lg border flex items-center justify-between transition ${
                    sub.enabled 
                      ? 'bg-slate-50 border-slate-200' 
                      : 'bg-slate-100/60 border-slate-200 opacity-60'
                  }`}
                >
                  <div className="min-w-0 flex-1 pr-2">
                    <div className="flex items-center space-x-2">
                      <span className="font-bold text-xs text-slate-900 truncate">{sub.display_name || sub.name}</span>
                      <span className={`text-3xs px-1.5 py-0.2 rounded font-mono ${
                        sub.enabled ? 'bg-emerald-100 text-emerald-800 font-semibold' : 'bg-slate-200 text-slate-600'
                      }`}>
                        {sub.enabled ? '启用' : '禁用'}
                      </span>
                    </div>
                    <div className="text-3xs text-slate-400 font-mono truncate mt-0.5" title={sub.url}>
                      {sub.url}
                    </div>
                  </div>

                  <div className="flex items-center space-x-1.5">
                    <button
                      onClick={() => handleToggleExtSub(sub)}
                      className="px-2 py-1 text-2xs font-semibold rounded bg-white border border-slate-200 hover:bg-slate-50 text-slate-700 transition"
                      title={sub.enabled ? '禁用该源' : '启用该源'}
                    >
                      {sub.enabled ? '禁用' : '启用'}
                    </button>
                    <ConfirmActionButton
                      onConfirm={() => handleDeleteExtSub(sub.name)}
                      confirmLabel="确认删除?"
                      className="p-1 text-slate-400 hover:text-red-600 rounded transition"
                      title="彻底删除"
                    >
                      <Trash2 className="w-3.5 h-3.5" />
                    </ConfirmActionButton>
                  </div>
                </div>
              ))}
            </div>
          </div>
        )}
      </section>

      {/* ============================================================== */}
      {/* 阶段二：策略组与标签编排 (POLICY GROUPS & TAG STUDIO) */}
      {/* ============================================================== */}
      <section className="space-y-4 pt-4 border-t border-slate-200">
        <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
          <div className="flex items-center space-x-2">
            <span className="w-5 h-5 rounded-full bg-purple-100 text-purple-700 text-2xs font-bold flex items-center justify-center">
              2
            </span>
            <div>
              <h2 className="text-sm font-bold text-slate-900 tracking-tight">
                策略组与标签编排 (Policy Group & Tag Studio)
              </h2>
              <p className="text-xs text-slate-500 mt-0.5">
                清晰区分只读外部策略组与自建策略组 · 所见即所得编辑 · 一键网状重建与五组同步
              </p>
            </div>
          </div>

          <div className="flex items-center space-x-2">
            <ConfirmActionButton
              onConfirm={handleRebuildMesh}
              confirmLabel="全网重建?"
              disabled={actionLoading === 'rebuild_mesh'}
              className="inline-flex items-center px-3 py-1.5 text-xs font-semibold rounded-lg bg-purple-50 text-purple-700 border border-purple-200 hover:bg-purple-100 transition shadow-2xs"
            >
              <RotateCw className={`w-3.5 h-3.5 mr-1.5 ${actionLoading === 'rebuild_mesh' ? 'animate-spin' : ''}`} />
              一键全网网状重建
            </ConfirmActionButton>
            <button
              onClick={handleSyncFiveGroups}
              disabled={actionLoading === 'sync_five'}
              className="inline-flex items-center px-3 py-1.5 text-xs font-semibold rounded-lg bg-blue-600 text-white hover:bg-blue-700 transition shadow-2xs"
            >
              <CheckCircle2 className="w-3.5 h-3.5 mr-1.5" />
              五维策略组全量同步
            </button>
          </div>
        </div>

        {/* Embedded PolicyGroupStudio (WYSIWYG custom policy groups) */}
        <PolicyGroupStudio />
      </section>

      {/* ============================================================== */}
      {/* 阶段三：双轨核心订阅最终成型看板 (FINAL CONCRETE DELIVERABLES) */}
      {/* ============================================================== */}
      <section className="space-y-4 pt-4 border-t border-slate-200">
        <div className="flex items-center justify-between">
          <div className="flex items-center space-x-2">
            <span className="w-5 h-5 rounded-full bg-emerald-100 text-emerald-700 text-2xs font-bold flex items-center justify-center">
              3
            </span>
            <div>
              <h2 className="text-sm font-bold text-slate-900 tracking-tight">
                三轨核心订阅最终成型看板 (Q 极速骨干 vs ashan 全家桶 vs business 纯自建商务)
              </h2>
              <p className="text-xs text-slate-500 mt-0.5">
                最终出炉的生产级订阅交付物 · 直观对比策略组构成、节点分布、双域名状态与快照直出
              </p>
            </div>
          </div>

          <button
            onClick={() => handleWarmup(['Q', 'ashan', 'business'])}
            disabled={actionLoading === 'warmup'}
            className="inline-flex items-center px-3 py-1.5 text-xs font-semibold rounded-lg text-emerald-700 bg-emerald-50 hover:bg-emerald-100 border border-emerald-200 transition shadow-2xs"
          >
            <Flame className={`w-3.5 h-3.5 mr-1.5 text-emerald-600 ${actionLoading === 'warmup' ? 'animate-pulse' : ''}`} />
            一键异步快照预热 (Warmup)
          </button>
        </div>

        {/* Three-Track Deliverables Comparison Cards */}
        <div className="grid grid-cols-1 lg:grid-cols-3 gap-5">
          {/* Track 1: Q (Lightweight Fast Backbone) */}
          <div className="bg-white border-2 border-blue-500/80 rounded-2xl p-5 shadow-xs flex flex-col justify-between space-y-4">
            <div className="space-y-3">
              <div className="flex items-center justify-between">
                <div className="flex items-center space-x-2.5">
                  <span className="w-8 h-8 rounded-xl bg-blue-600 text-white font-bold flex items-center justify-center text-sm shadow-2xs">
                    Q
                  </span>
                  <div>
                    <h3 className="font-bold text-sm text-slate-900">Q 极速轻量骨干版</h3>
                    <p className="text-2xs text-slate-500">面向手机、平板与移动设备 · 极速低延迟</p>
                  </div>
                </div>
                <span className="text-2xs font-mono px-2 py-0.5 rounded bg-blue-50 text-blue-700 font-semibold border border-blue-200">
                  {snapshotStatus?.snapshots?.['Q']?.proxy_count || snapshotStatus?.snapshots?.['q']?.proxy_count
                    ? `${snapshotStatus?.snapshots?.['Q']?.proxy_count || snapshotStatus?.snapshots?.['q']?.proxy_count} 节点 · 0 家宽`
                    : `${atoms.filter(a => a.kind === 'transit').length} 骨干 · 0 家宽`}
                </span>
              </div>

              {/* Composition Breakdown */}
              <div className="grid grid-cols-3 gap-2 text-center py-2 bg-slate-50 rounded-xl border border-slate-200 font-mono text-2xs">
                <div>
                  <div className="text-slate-400">自建专线</div>
                  <div className="font-bold text-slate-900 text-sm mt-0.5">{atoms.filter(a => a.kind === 'transit').length} 条骨干</div>
                </div>
                <div>
                  <div className="text-slate-400">外部商业</div>
                  <div className="font-bold text-emerald-700 text-sm mt-0.5">{externalSubs.filter(s => s.enabled).length} 机场池</div>
                </div>
                <div>
                  <div className="text-slate-400">住宅家宽</div>
                  <div className="font-bold text-slate-400 text-sm mt-0.5">0 住宅 (硬隔离)</div>
                </div>
              </div>

              {/* Domain & Fast Snapshot Badges */}
              <div className="flex flex-wrap items-center gap-2 pt-1 text-2xs font-mono">
                <span className="inline-flex items-center px-2 py-0.5 rounded bg-slate-100 text-slate-700 border border-slate-200">
                  {manifest?.domains?.length ? `${manifest.domains[0]} · 需点击校验测活` : '已挂载 SubStore 分发'}
                </span>
                {(snapshotStatus?.snapshots?.['Q'] || snapshotStatus?.snapshots?.['q']) && (() => {
                  const s = snapshotStatus.snapshots['Q'] || snapshotStatus.snapshots['q'];
                  return (
                    <span className="inline-flex items-center px-2 py-0.5 rounded bg-blue-50 text-blue-700 border border-blue-200">
                      快照: {s.sha256?.substring(0, 7)} ({s.duration_ms}ms)
                    </span>
                  );
                })()}
              </div>
            </div>

            {/* Actions: View Topology & Download */}
            <div className="pt-3 border-t border-slate-100 flex flex-wrap items-center justify-between gap-2">
              <div className="flex items-center space-x-2">
                <a
                  href={getDownloadUrl('Q')}
                  target="_blank"
                  rel="noreferrer"
                  download="Q.yaml"
                  className="text-xs font-semibold text-slate-700 hover:text-slate-900 flex items-center bg-slate-100 hover:bg-slate-200 px-3 py-1.5 rounded-lg border border-slate-200 transition"
                  title="在浏览器中直接下载已清洗编排的 Clash YAML 文件"
                >
                  <span>直接下载 Clash YAML</span>
                  <ExternalLink className="w-3 h-3 ml-1" />
                </a>

                <button
                  type="button"
                  onClick={() => handleCopySub('Q')}
                  className="text-xs font-semibold text-slate-700 hover:text-slate-900 flex items-center bg-slate-100 hover:bg-slate-200 px-3 py-1.5 rounded-lg border border-slate-200 transition"
                  title="复制含凭证的完整订阅 URL，可直接粘贴进 Clash / Shadowrocket / Loon"
                >
                  {copiedSub === 'Q' ? (
                    <>
                      <Check className="w-3.5 h-3.5 text-emerald-600 mr-1" />
                      <span className="text-emerald-700 font-bold">已复制链接!</span>
                    </>
                  ) : (
                    <>
                      <Copy className="w-3.5 h-3.5 text-slate-500 mr-1" />
                      <span>复制订阅 URL</span>
                    </>
                  )}
                </button>
              </div>

              <button
                onClick={() => setDrawerSubName('Q')}
                className="inline-flex items-center px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white rounded-xl text-xs font-bold shadow-2xs transition group"
              >
                <span>🔍 查看全量拓扑与双域名检验报告</span>
                <ArrowRight className="w-3.5 h-3.5 ml-1.5 group-hover:translate-x-0.5 transition-transform" />
              </button>
            </div>
          </div>

          {/* Track 2: ashan (Full-Stack Flagship) */}
          <div className="bg-white border-2 border-purple-500/80 rounded-2xl p-5 shadow-xs flex flex-col justify-between space-y-4">
            <div className="space-y-3">
              <div className="flex items-center justify-between">
                <div className="flex items-center space-x-2.5">
                  <span className="w-8 h-8 rounded-xl bg-purple-600 text-white font-bold flex items-center justify-center text-sm shadow-2xs">
                    A
                  </span>
                  <div>
                    <h3 className="font-bold text-sm text-slate-900">ashan 全栈旗舰版</h3>
                    <p className="text-2xs text-slate-500">面向桌面、TV与高防 AI 工作负载 · 100% 解锁全境</p>
                  </div>
                </div>
                <span className="text-2xs font-mono px-2 py-0.5 rounded bg-purple-50 text-purple-700 font-semibold border border-purple-200">
                  {snapshotStatus?.snapshots?.['ashan']?.proxy_count || snapshotStatus?.snapshots?.['Ashan']?.proxy_count
                    ? `${snapshotStatus?.snapshots?.['ashan']?.proxy_count || snapshotStatus?.snapshots?.['Ashan']?.proxy_count} 节点 · ${atoms.filter(a => a.kind === 'residential' || a.kind === 'chain').length} 家宽/链`
                    : `${atoms.length} 自建原子`}
                </span>
              </div>

              {/* Composition Breakdown */}
              <div className="grid grid-cols-3 gap-2 text-center py-2 bg-slate-50 rounded-xl border border-slate-200 font-mono text-2xs">
                <div>
                  <div className="text-slate-400">自建专线</div>
                  <div className="font-bold text-slate-900 text-sm mt-0.5">{atoms.filter(a => a.kind === 'transit').length} 条骨干</div>
                </div>
                <div>
                  <div className="text-slate-400">原生家宽</div>
                  <div className="font-bold text-purple-700 text-sm mt-0.5">{atoms.filter(a => a.kind === 'residential').length} 家宽</div>
                </div>
                <div>
                  <div className="text-slate-400">网状中继</div>
                  <div className="font-bold text-emerald-700 text-sm mt-0.5">{customChains.length} 条链路</div>
                </div>
              </div>

              {/* Domain & Fast Snapshot Badges */}
              <div className="flex flex-wrap items-center gap-2 pt-1 text-2xs font-mono">
                <span className="inline-flex items-center px-2 py-0.5 rounded bg-slate-100 text-slate-700 border border-slate-200">
                  {manifest?.domains?.length ? `${manifest.domains[manifest.domains.length - 1]} · 需点击校验测活` : '已挂载 SubStore 分发'}
                </span>
                {(snapshotStatus?.snapshots?.['ashan'] || snapshotStatus?.snapshots?.['Ashan']) && (() => {
                  const s = snapshotStatus.snapshots['ashan'] || snapshotStatus.snapshots['Ashan'];
                  return (
                    <span className="inline-flex items-center px-2 py-0.5 rounded bg-purple-50 text-purple-700 border border-purple-200">
                      快照: {s.sha256?.substring(0, 7)} ({s.duration_ms}ms)
                    </span>
                  );
                })()}
              </div>
            </div>

            {/* Actions: View Topology & Download */}
            <div className="pt-3 border-t border-slate-100 flex flex-wrap items-center justify-between gap-2">
              <div className="flex items-center space-x-2">
                <a
                  href={getDownloadUrl('ashan')}
                  target="_blank"
                  rel="noreferrer"
                  download="ashan.yaml"
                  className="text-xs font-semibold text-slate-700 hover:text-slate-900 flex items-center bg-slate-100 hover:bg-slate-200 px-3 py-1.5 rounded-lg border border-slate-200 transition"
                  title="在浏览器中直接下载已清洗编排的 Clash YAML 文件"
                >
                  <span>直接下载 Clash YAML</span>
                  <ExternalLink className="w-3 h-3 ml-1" />
                </a>

                <button
                  type="button"
                  onClick={() => handleCopySub('ashan')}
                  className="text-xs font-semibold text-slate-700 hover:text-slate-900 flex items-center bg-slate-100 hover:bg-slate-200 px-3 py-1.5 rounded-lg border border-slate-200 transition"
                  title="复制含凭证的完整订阅 URL，可直接粘贴进 Clash / Shadowrocket / Loon"
                >
                  {copiedSub === 'ashan' ? (
                    <>
                      <Check className="w-3.5 h-3.5 text-emerald-600 mr-1" />
                      <span className="text-emerald-700 font-bold">已复制链接!</span>
                    </>
                  ) : (
                    <>
                      <Copy className="w-3.5 h-3.5 text-slate-500 mr-1" />
                      <span>复制订阅 URL</span>
                    </>
                  )}
                </button>
              </div>

              <button
                onClick={() => setDrawerSubName('ashan')}
                className="inline-flex items-center px-4 py-2 bg-purple-600 hover:bg-purple-700 text-white rounded-xl text-xs font-bold shadow-2xs transition group"
              >
                <span>🔍 查看全量拓扑与双域名检验报告</span>
                <ArrowRight className="w-3.5 h-3.5 ml-1.5 group-hover:translate-x-0.5 transition-transform" />
              </button>
            </div>
          </div>

          {/* Track 3: business (Pure Self-Hosted Business) */}
          <div className="bg-white border-2 border-emerald-500/80 rounded-2xl p-5 shadow-xs flex flex-col justify-between space-y-4">
            <div className="space-y-3">
              <div className="flex items-center justify-between">
                <div className="flex items-center space-x-2.5">
                  <span className="w-8 h-8 rounded-xl bg-emerald-600 text-white font-bold flex items-center justify-center text-sm shadow-2xs">
                    B
                  </span>
                  <div>
                    <h3 className="font-bold text-sm text-slate-900">business 纯自建商务版</h3>
                    <p className="text-2xs text-slate-500">面向跨境电商、纯净业务与安全办公 · 0 外部机场</p>
                  </div>
                </div>
                <span className="text-2xs font-mono px-2 py-0.5 rounded bg-emerald-50 text-emerald-700 font-semibold border border-emerald-200">
                  {snapshotStatus?.snapshots?.['business']?.proxy_count || snapshotStatus?.snapshots?.['Business']?.proxy_count
                    ? `${snapshotStatus?.snapshots?.['business']?.proxy_count || snapshotStatus?.snapshots?.['Business']?.proxy_count} 纯自建节点`
                    : `纯自建 · ${atoms.length} 原子`}
                </span>
              </div>

              {/* Composition Breakdown */}
              <div className="grid grid-cols-3 gap-2 text-center py-2 bg-slate-50 rounded-xl border border-slate-200 font-mono text-2xs">
                <div>
                  <div className="text-slate-400">自建专线</div>
                  <div className="font-bold text-slate-900 text-sm mt-0.5">{atoms.filter(a => a.kind === 'transit').length} 条骨干</div>
                </div>
                <div>
                  <div className="text-slate-400">外部商业</div>
                  <div className="font-bold text-slate-400 text-sm mt-0.5">0 外部机场</div>
                </div>
                <div>
                  <div className="text-slate-400">原生家宽+链</div>
                  <div className="font-bold text-emerald-700 text-sm mt-0.5">{atoms.filter(a => a.kind === 'residential' || a.kind === 'chain').length} 个节点</div>
                </div>
              </div>

              {/* Domain & Fast Snapshot Badges */}
              <div className="flex flex-wrap items-center gap-2 pt-1 text-2xs font-mono">
                <span className="inline-flex items-center px-2 py-0.5 rounded bg-slate-100 text-slate-700 border border-slate-200">
                  {manifest?.domains?.length ? `${manifest.domains[0]} · 需点击校验测活` : '已挂载 SubStore 分发'}
                </span>
                {(snapshotStatus?.snapshots?.['business'] || snapshotStatus?.snapshots?.['Business']) && (() => {
                  const s = snapshotStatus.snapshots['business'] || snapshotStatus.snapshots['Business'];
                  return (
                    <span className="inline-flex items-center px-2 py-0.5 rounded bg-emerald-50 text-emerald-700 border border-emerald-200">
                      快照: {s.sha256?.substring(0, 7)} ({s.duration_ms}ms)
                    </span>
                  );
                })()}
              </div>
            </div>

            {/* Actions: View Topology & Download */}
            <div className="pt-3 border-t border-slate-100 flex flex-wrap items-center justify-between gap-2">
              <div className="flex items-center space-x-2">
                <a
                  href={getDownloadUrl('business')}
                  target="_blank"
                  rel="noreferrer"
                  download="business.yaml"
                  className="text-xs font-semibold text-slate-700 hover:text-slate-900 flex items-center bg-slate-100 hover:bg-slate-200 px-3 py-1.5 rounded-lg border border-slate-200 transition"
                  title="在浏览器中直接下载已清洗编排的 Clash YAML 文件"
                >
                  <span>直接下载 Clash YAML</span>
                  <ExternalLink className="w-3 h-3 ml-1" />
                </a>

                <button
                  type="button"
                  onClick={() => handleCopySub('business')}
                  className="text-xs font-semibold text-slate-700 hover:text-slate-900 flex items-center bg-slate-100 hover:bg-slate-200 px-3 py-1.5 rounded-lg border border-slate-200 transition"
                  title="复制含凭证的完整订阅 URL，可直接粘贴进 Clash / Shadowrocket / Loon"
                >
                  {copiedSub === 'business' ? (
                    <>
                      <Check className="w-3.5 h-3.5 text-emerald-600 mr-1" />
                      <span className="text-emerald-700 font-bold">已复制链接!</span>
                    </>
                  ) : (
                    <>
                      <Copy className="w-3.5 h-3.5 text-slate-500 mr-1" />
                      <span>复制订阅 URL</span>
                    </>
                  )}
                </button>
              </div>

              <button
                onClick={() => setDrawerSubName('business')}
                className="inline-flex items-center px-4 py-2 bg-emerald-600 hover:bg-emerald-700 text-white rounded-xl text-xs font-bold shadow-2xs transition group"
              >
                <span>🔍 查看全量拓扑与双域名检验报告</span>
                <ArrowRight className="w-3.5 h-3.5 ml-1.5 group-hover:translate-x-0.5 transition-transform" />
              </button>
            </div>
          </div>
        </div>
      </section>

      {/* ============================================================== */}
      {/* 底部折叠面板：高级运维与脚本管理 (ADVANCED SCRIPTS & OPERATORS) */}
      {/* ============================================================== */}
      <section className="bg-white border border-slate-200 rounded-2xl overflow-hidden shadow-2xs">
        <button
          onClick={() => setShowAdvancedOps(!showAdvancedOps)}
          className="w-full p-4 flex items-center justify-between hover:bg-slate-50 transition text-left"
        >
          <div className="flex items-center space-x-2.5">
            <Terminal className="w-4 h-4 text-slate-600" />
            <div>
              <span className="font-bold text-xs text-slate-900">高级运维与 SubStore 算子脚本管理</span>
              <span className="text-2xs text-slate-400 ml-2">底层 JS 算子在线编辑 · 拓扑自主整理 · 失效 Socks 清理</span>
            </div>
          </div>
          <div className="flex items-center space-x-2 text-2xs text-slate-500">
            <span>{showAdvancedOps ? '收起面板' : '展开详情'}</span>
            {showAdvancedOps ? <ChevronUp className="w-4 h-4" /> : <ChevronDown className="w-4 h-4" />}
          </div>
        </button>

        {showAdvancedOps && (
          <div className="p-5 border-t border-slate-200 space-y-5 bg-slate-50/50">
            {/* Quick Maintenance Actions */}
            <div className="flex flex-wrap items-center gap-2">
              <button
                onClick={async () => {
                  try {
                    const res = await api.reconcileTopology();
                    setNotice({ type: 'success', text: res.message || 'SubStore 拓扑自主整理完毕！' });
                  } catch (e: any) {
                    setNotice({ type: 'error', text: e.message });
                  }
                }}
                className="px-3 py-1.5 text-2xs font-semibold rounded-lg bg-white border border-slate-300 hover:bg-slate-50 text-slate-700 transition shadow-2xs"
              >
                🛠️ SubStore 拓扑自主整理 (Reconcile)
              </button>

              <button
                onClick={async () => {
                  try {
                    const res = await api.cleanupDeadSocks('Q');
                    setNotice({ type: 'success', text: `已成功清理 ${res.removed_count || 0} 个失效 Socks5 节点` });
                  } catch (e: any) {
                    setNotice({ type: 'error', text: e.message });
                  }
                }}
                className="px-3 py-1.5 text-2xs font-semibold rounded-lg bg-white border border-slate-300 hover:bg-slate-50 text-slate-700 transition shadow-2xs"
              >
                🧹 清理 Q 订阅失效 Socks 节点
              </button>
            </div>

            {/* Script Operator Live Code Editor */}
            <div className="space-y-3">
              <div className="flex items-center justify-between">
                <div className="flex items-center space-x-2">
                  <FileCode className="w-4 h-4 text-blue-600" />
                  <span className="font-bold text-xs text-slate-900">算子脚本在线实时编辑与发布</span>
                </div>
                <div className="flex items-center space-x-2">
                  <select
                    value={selectedScript}
                    onChange={(e) => setSelectedScript(e.target.value)}
                    className="px-2.5 py-1 text-2xs border border-slate-300 rounded-lg bg-white text-slate-800 focus:outline-none"
                  >
                    <option value="custom_patch.js">custom_patch.js (主分流清洗算子)</option>
                    <option value="convert.js">convert.js (Sing-box/Clash 格式转换算子)</option>
                  </select>
                  <button
                    onClick={handleUpdateScript}
                    disabled={savingScript}
                    className="px-3 py-1 text-2xs font-semibold rounded-lg bg-blue-600 hover:bg-blue-700 text-white transition shadow-2xs flex items-center"
                  >
                    <Save className="w-3 h-3 mr-1" />
                    {savingScript ? '部署中...' : '保存并热部署'}
                  </button>
                </div>
              </div>

              <textarea
                value={scriptContent}
                onChange={(e) => setScriptContent(e.target.value)}
                rows={12}
                className="w-full p-3 font-mono text-2xs bg-slate-900 text-emerald-400 rounded-xl border border-slate-800 focus:outline-none focus:ring-1 focus:ring-blue-500"
              />
            </div>
          </div>
        )}
      </section>

      {/* TOPOLOGY & VERIFICATION FULL-HEIGHT DRAWER */}
      {drawerSubName && (
        <SubscriptionTopologyDrawer
          subscriptionName={drawerSubName}
          availableSubscriptions={['Q', 'ashan', 'business']}
          onClose={() => setDrawerSubName(null)}
          onSelectSubscription={(name) => setDrawerSubName(name)}
        />
      )}

      {/* Add External Sub Modal */}
      {showAddExtModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/50 backdrop-blur-2xs p-4">
          <div className="bg-white rounded-2xl shadow-xl max-w-md w-full border border-slate-200 p-6 animate-in zoom-in-95 duration-150">
            <h3 className="font-bold text-sm text-slate-900 mb-1">纳管外部商业机场订阅</h3>
            <p className="text-2xs text-slate-500 mb-4">
              直接传入商业机场订阅链接，系统将在 SubStore 自动创建并挂载到当前模式。
            </p>

            <form onSubmit={handleSaveExtSub} className="space-y-4 text-xs">
              <div>
                <label className="block font-semibold text-slate-700 mb-1">机场订阅标识 / 名称</label>
                <input
                  type="text"
                  required
                  value={extSubName}
                  onChange={(e) => setExtSubName(e.target.value)}
                  placeholder="例如: dler-cloud 或 tag-airport"
                  className="w-full px-3 py-2 border border-slate-300 rounded-lg focus:ring-1 focus:ring-blue-600 focus:outline-none"
                />
              </div>

              <div>
                <label className="block font-semibold text-slate-700 mb-1">订阅 URL</label>
                <textarea
                  rows={3}
                  required
                  value={extSubURL}
                  onChange={(e) => setExtSubURL(e.target.value)}
                  placeholder="https://api.airport.com/link/token?clash=1"
                  className="w-full px-3 py-2 border border-slate-300 rounded-lg font-mono text-2xs focus:ring-1 focus:ring-blue-600 focus:outline-none"
                />
              </div>

              <div className="flex items-center space-x-2">
                <input
                  type="checkbox"
                  id="ext_enabled"
                  checked={extSubEnabled}
                  onChange={(e) => setExtSubEnabled(e.target.checked)}
                  className="rounded border-slate-300 text-blue-600 focus:ring-blue-500"
                />
                <label htmlFor="ext_enabled" className="text-xs text-slate-700">立即启用并挂载到双轨订阅中</label>
              </div>

              <div className="flex justify-end space-x-2 pt-2">
                <button
                  type="button"
                  onClick={() => setShowAddExtModal(false)}
                  className="px-3.5 py-1.5 text-slate-600 hover:bg-slate-100 rounded-lg font-medium"
                >
                  取消
                </button>
                <button
                  type="submit"
                  disabled={actionLoading === 'save_ext'}
                  className="px-4 py-1.5 bg-blue-600 hover:bg-blue-700 text-white rounded-lg font-semibold shadow-2xs disabled:opacity-50"
                >
                  {actionLoading === 'save_ext' ? '保存中...' : '确认纳管'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
};
