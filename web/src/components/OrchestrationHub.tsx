import React, { useState, useEffect, useMemo, useRef } from 'react';
import { 
  Layers, 
  Plus, 
  RotateCw, 
  CheckCircle2, 
  AlertTriangle, 
  ShieldCheck, 
  ShieldAlert, 
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
  Info
} from 'lucide-react';
import { api } from '../api/client';
import { PolicyGroupStudio } from './PolicyGroupStudio';
import { 
  CategoriesSummary, 
  CategoryStat, 
  ExternalCategoryStat, 
  CustomGroupDef, 
  CustomChainBind, 
  AtomNode,
  GroupFilter
} from '../types';

interface OrchestrationHubProps {
  onRefreshGlobal?: () => void;
}

export const OrchestrationHub: React.FC<OrchestrationHubProps> = ({ onRefreshGlobal }) => {
  const [summary, setSummary] = useState<CategoriesSummary | null>(null);
  const [customChains, setCustomChains] = useState<CustomChainBind[]>([]);
  const [loading, setLoading] = useState(true);
  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const [notice, setNotice] = useState<{ type: 'success' | 'error'; text: string } | null>(null);

  // 折叠状态控制
  const [expandedCards, setExpandedCards] = useState<Record<string, boolean>>({});

  // 模态弹窗状态
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [modalForm, setModalForm] = useState<{
    name: string;
    display_name: string;
    countries: string[];
    kinds: string[];
    clean_only: boolean;
    keywords: string;
    exclude: string;
  }>({
    name: '',
    display_name: '',
    countries: ['US'],
    kinds: ['residential', 'chain'],
    clean_only: true,
    keywords: '',
    exclude: '',
  });

  // 实时试算预览状态
  const [evaluating, setEvaluating] = useState(false);
  const [previewResult, setPreviewResult] = useState<{ count: number; nodes: string[] }>({ count: 0, nodes: [] });

  // 显式链路绑定表单
  const [selectedTransit, setSelectedTransit] = useState('');
  const [selectedResi, setSelectedResi] = useState('');

  // 加载数据
  const fetchData = async () => {
    setLoading(true);
    try {
      const [sumRes, chainsRes] = await Promise.all([
        api.getCategoriesSummary().catch(() => null),
        api.getCustomChains().catch(() => ({ count: 0, chains: [] })),
      ]);
      if (sumRes) setSummary(sumRes);
      setCustomChains(chainsRes.chains || []);
    } catch (err: any) {
      setNotice({ type: 'error', text: `加载策略编排数据失败: ${err.message}` });
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchData();
  }, []);

  // 自动防抖试算预览
  const evalTimerRef = useRef<number | null>(null);
  useEffect(() => {
    if (!showCreateModal) return;

    if (evalTimerRef.current) clearTimeout(evalTimerRef.current);
    evalTimerRef.current = window.setTimeout(async () => {
      setEvaluating(true);
      try {
        const filter: GroupFilter = {
          countries: modalForm.countries.length > 0 ? modalForm.countries : undefined,
          kinds: modalForm.kinds.length > 0 ? modalForm.kinds : undefined,
          clean_only: modalForm.clean_only,
          keywords: modalForm.keywords ? modalForm.keywords.split(',').map(s => s.trim()).filter(Boolean) : undefined,
          exclude: modalForm.exclude ? modalForm.exclude.split(',').map(s => s.trim()).filter(Boolean) : undefined,
        };
        const res = await api.evaluateGroup(filter, true);
        setPreviewResult({ count: res.count, nodes: res.nodes || [] });
      } catch (e) {
        setPreviewResult({ count: 0, nodes: [] });
      } finally {
        setEvaluating(false);
      }
    }, 300);

    return () => {
      if (evalTimerRef.current) clearTimeout(evalTimerRef.current);
    };
  }, [modalForm, showCreateModal]);

  // 切换折叠卡片
  const toggleCard = (key: string) => {
    setExpandedCards(prev => ({ ...prev, [key]: !prev[key] }));
  };

  // 一键重建全量网状链式池
  const handleRebuildChains = async () => {
    setActionLoading('rebuild_chains');
    setNotice(null);
    try {
      const res = await api.rebuildFullMeshChains();
      setNotice({ type: 'success', text: `全量网状链式池化重建并烘焙快照成功: ${res.message || 'OK'}` });
      await fetchData();
      if (onRefreshGlobal) onRefreshGlobal();
    } catch (err: any) {
      setNotice({ type: 'error', text: `重建链式池失败: ${err.message}` });
    } finally {
      setActionLoading(null);
    }
  };

  // 一键同步五维策略组
  const handleSyncFiveGroups = async () => {
    setActionLoading('sync_five');
    setNotice(null);
    try {
      const res = await api.syncFiveGroups();
      setNotice({ type: 'success', text: `五维策略分组已精准推送至 Sub-Store: ${res.message || 'OK'}` });
      await fetchData();
      if (onRefreshGlobal) onRefreshGlobal();
    } catch (err: any) {
      setNotice({ type: 'error', text: `同步五维分组失败: ${err.message}` });
    } finally {
      setActionLoading(null);
    }
  };

  // 保存自定义策略组
  const handleSaveGroup = async () => {
    if (!modalForm.name.trim()) {
      setNotice({ type: 'error', text: '分组英文标识 (ID) 不能为空' });
      return;
    }
    setActionLoading('save_group');
    try {
      const def: CustomGroupDef = {
        name: modalForm.name.trim(),
        display_name: modalForm.display_name.trim() || modalForm.name.trim(),
        enabled: true,
        filter: {
          countries: modalForm.countries,
          kinds: modalForm.kinds,
          clean_only: modalForm.clean_only,
          keywords: modalForm.keywords ? modalForm.keywords.split(',').map(s => s.trim()).filter(Boolean) : undefined,
          exclude: modalForm.exclude ? modalForm.exclude.split(',').map(s => s.trim()).filter(Boolean) : undefined,
        },
      };
      await api.saveCustomGroup(def, true);
      setNotice({ type: 'success', text: `自定义策略组 [${def.display_name}] 已创建并同步至 Sub-Store!` });
      setShowCreateModal(false);
      await fetchData();
      if (onRefreshGlobal) onRefreshGlobal();
    } catch (err: any) {
      setNotice({ type: 'error', text: `创建策略组失败: ${err.message}` });
    } finally {
      setActionLoading(null);
    }
  };

  // 删除自定义分组
  const handleDeleteGroup = async (name: string) => {
    if (!window.confirm(`确定要删除自定义策略组 [${name}] 吗？`)) return;
    setActionLoading(`del_${name}`);
    try {
      await api.deleteCustomGroup(name, true);
      setNotice({ type: 'success', text: `已安全移除策略组 [${name}]` });
      await fetchData();
      if (onRefreshGlobal) onRefreshGlobal();
    } catch (err: any) {
      setNotice({ type: 'error', text: `删除失败: ${err.message}` });
    } finally {
      setActionLoading(null);
    }
  };

  // 显式绑定中继
  const handleBindChain = async () => {
    if (!selectedTransit || !selectedResi) {
      setNotice({ type: 'error', text: '请同时选择线路机与目标家宽机' });
      return;
    }
    setActionLoading('bind_chain');
    try {
      const res = await api.bindCustomChain(selectedTransit, selectedResi);
      setNotice({ type: 'success', text: res.message || '链路显式绑定成功' });
      setSelectedTransit('');
      setSelectedResi('');
      await fetchData();
      if (onRefreshGlobal) onRefreshGlobal();
    } catch (err: any) {
      setNotice({ type: 'error', text: `绑定失败: ${err.message}` });
    } finally {
      setActionLoading(null);
    }
  };

  // 解除显式中继绑定
  const handleUnbindChain = async (target: string) => {
    if (!window.confirm(`确定解除链路绑定 [${target}] 吗？`)) return;
    setActionLoading(`unbind_${target}`);
    try {
      const res = await api.unbindCustomChain(target);
      setNotice({ type: 'success', text: res.message || '已成功解除中继绑定' });
      await fetchData();
      if (onRefreshGlobal) onRefreshGlobal();
    } catch (err: any) {
      setNotice({ type: 'error', text: `解绑失败: ${err.message}` });
    } finally {
      setActionLoading(null);
    }
  };

  // 提取可用线路机和家宽机候选项
  const transitOptions = useMemo(() => {
    const list = summary?.categories?.transit?.nodes || [];
    return list.map(n => n.name);
  }, [summary]);

  const resiOptions = useMemo(() => {
    const list = summary?.categories?.residential?.nodes || [];
    return list.filter(n => n.kind === 'residential').map(n => n.name);
  }, [summary]);

  return (
    <div className="space-y-6">
      {/* 顶部通知横幅 */}
      {notice && (
        <div className={`p-4 rounded-xl border flex items-center justify-between text-sm animate-in fade-in ${
          notice.type === 'success' 
            ? 'bg-emerald-50 border-emerald-200 text-emerald-800' 
            : 'bg-red-50 border-red-200 text-red-800'
        }`}>
          <div className="flex items-center space-x-2">
            {notice.type === 'success' ? <CheckCircle2 className="w-5 h-5 text-emerald-600" /> : <AlertTriangle className="w-5 h-5 text-red-600" />}
            <span>{notice.text}</span>
          </div>
          <button onClick={() => setNotice(null)} className="text-slate-400 hover:text-slate-600 text-xs">
            关闭
          </button>
        </div>
      )}

      {/* 1. 顶部工作台标题与全局控制栏 */}
      <div className="bg-white border border-slate-200/80 rounded-2xl p-5 shadow-xs">
        <div className="flex flex-col lg:flex-row lg:items-center justify-between gap-4">
          <div className="space-y-1">
            <div className="flex items-center space-x-2.5">
              <div className="p-2 bg-blue-50 text-blue-600 rounded-lg">
                <Layers className="w-5 h-5" />
              </div>
              <h2 className="text-lg font-bold text-slate-900 tracking-tight">
                策略编排与动态原子中枢
              </h2>
              <span className="px-2 py-0.5 rounded-full text-xs font-mono font-medium bg-blue-100 text-blue-800">
                Orchestration Engine
              </span>
            </div>
            <p className="text-xs text-slate-500">
              统一管理自建五维策略组、全量网状链式中继池、商业外部分组及任意自定义过滤公式。
            </p>
          </div>

          {/* 快捷操作按钮组 */}
          <div className="flex flex-wrap items-center gap-2">
            <button
              onClick={handleRebuildChains}
              disabled={actionLoading !== null}
              className="px-3 py-1.5 bg-slate-900 hover:bg-slate-800 text-white rounded-lg text-xs font-semibold shadow-2xs transition flex items-center space-x-1.5 disabled:opacity-50"
            >
              <RotateCw className={`w-3.5 h-3.5 ${actionLoading === 'rebuild_chains' ? 'animate-spin' : ''}`} />
              <span>一键重建网状链式池</span>
            </button>

            <button
              onClick={handleSyncFiveGroups}
              disabled={actionLoading !== null}
              className="px-3 py-1.5 bg-blue-600 hover:bg-blue-700 text-white rounded-lg text-xs font-semibold shadow-2xs transition flex items-center space-x-1.5 disabled:opacity-50"
            >
              <Zap className={`w-3.5 h-3.5 ${actionLoading === 'sync_five' ? 'animate-spin' : ''}`} />
              <span>一键同步五维策略组</span>
            </button>

            <button
              onClick={() => setShowCreateModal(true)}
              className="px-3 py-1.5 bg-emerald-600 hover:bg-emerald-700 text-white rounded-lg text-xs font-semibold shadow-2xs transition flex items-center space-x-1.5"
            >
              <Plus className="w-3.5 h-3.5" />
              <span>新建自定义策略组</span>
            </button>
          </div>
        </div>

        {/* 状态徽标与统计指标 Pill */}
        <div className="mt-4 pt-4 border-t border-slate-100 flex flex-wrap items-center gap-3 text-xs">
          <div className="flex items-center space-x-1.5 bg-slate-50 px-2.5 py-1 rounded-md border border-slate-200 text-slate-600">
            <span className="text-slate-400">总可用原子节点:</span>
            <span className="font-bold font-mono text-slate-900">{summary?.total_atoms || 0}</span>
          </div>

          <div className="flex items-center space-x-1.5 bg-emerald-50 px-2.5 py-1 rounded-md border border-emerald-200 text-emerald-700">
            <ShieldCheck className="w-3.5 h-3.5" />
            <span>纯净骨干/家宽池:</span>
            <span className="font-bold font-mono">
              {(summary?.categories?.residential?.total_clean_count || 0) + (summary?.categories?.transit?.total_clean_count || 0)}
            </span>
          </div>

          <div className="flex items-center space-x-1.5 bg-indigo-50 px-2.5 py-1 rounded-md border border-indigo-200 text-indigo-700">
            <Link2 className="w-3.5 h-3.5" />
            <span>常驻网状链式中继:</span>
            <span className="font-bold font-mono">{summary?.categories?.residential?.chain_count || 0}</span>
          </div>

          <div className="flex items-center space-x-1.5 bg-red-50 px-2.5 py-1 rounded-md border border-red-200 text-red-700">
            <ShieldAlert className="w-3.5 h-3.5" />
            <span>送中物理强隔离:</span>
            <span className="font-bold font-mono">{summary?.categories?.china_leaks?.length || 0}</span>
          </div>
        </div>
      </div>

      {/* 2. 五维策略大盘 & 外部机场池卡片矩阵 */}
      <div>
        <div className="flex items-center justify-between mb-3">
          <div className="flex items-center space-x-2">
            <h3 className="text-sm font-bold text-slate-800 uppercase tracking-wider">
              五维策略大盘与分发概览 (Sub-Store 5 Groups)
            </h3>
            <span className="text-2xs font-mono text-slate-400">零硬编码 · 动态分类</span>
          </div>
        </div>

        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
          {/* A. 外部商业机场分组 */}
          {summary?.categories?.external && (
            <div className="bg-white border border-slate-200/90 rounded-xl p-4 shadow-2xs hover:border-slate-300 transition flex flex-col justify-between">
              <div>
                <div className="flex items-center justify-between mb-2">
                  <span className="px-2 py-0.5 rounded text-2xs font-bold uppercase bg-amber-100 text-amber-800">
                    商业外源
                  </span>
                  <span className="text-2xs text-slate-400 font-mono">随插即用</span>
                </div>
                <h4 className="font-bold text-slate-900 text-sm mb-1">
                  {summary.categories.external.display_name}
                </h4>
                <p className="text-xs text-slate-500 mb-3 line-clamp-2">
                  {summary.categories.external.description}
                </p>
                <div className="flex flex-wrap gap-1.5 mb-2">
                  {summary.categories.external.sources.map((s, idx) => (
                    <span key={idx} className="px-2 py-0.5 rounded-md bg-slate-100 text-slate-700 text-xs font-mono">
                      {s}
                    </span>
                  ))}
                </div>
              </div>
              <div className="pt-2 border-t border-slate-100 flex items-center justify-between text-2xs text-slate-400">
                <span>外部分发源</span>
                <span className="font-mono font-bold text-slate-700">{summary.categories.external.source_count} 个订阅</span>
              </div>
            </div>
          )}

          {/* B. 全量自建家宽池 */}
          {summary?.categories?.residential && (
            <div className="bg-white border border-slate-200/90 rounded-xl p-4 shadow-2xs hover:border-slate-300 transition flex flex-col justify-between">
              <div>
                <div className="flex items-center justify-between mb-2">
                  <span className="px-2 py-0.5 rounded text-2xs font-bold uppercase bg-emerald-100 text-emerald-800">
                    全量家宽池
                  </span>
                  <span className="text-2xs text-emerald-600 font-mono font-bold">100% 住宅出口</span>
                </div>
                <h4 className="font-bold text-slate-900 text-sm mb-1">
                  {summary.categories.residential.display_name}
                </h4>
                <p className="text-xs text-slate-500 mb-3">
                  聚合所有自建原生家宽直连与多骨干链式优化中继节点。
                </p>
                <div className="grid grid-cols-2 gap-2 mb-2 text-xs">
                  <div className="bg-slate-50 p-2 rounded-lg border border-slate-100">
                    <span className="text-slate-400 text-2xs block">直连节点</span>
                    <span className="font-bold font-mono text-slate-800">{summary.categories.residential.residential_count}</span>
                  </div>
                  <div className="bg-slate-50 p-2 rounded-lg border border-slate-100">
                    <span className="text-slate-400 text-2xs block">链式中继</span>
                    <span className="font-bold font-mono text-indigo-700">{summary.categories.residential.chain_count}</span>
                  </div>
                </div>
              </div>
              <div className="pt-2 border-t border-slate-100 flex items-center justify-between text-2xs">
                <span className="text-slate-400">纯净总数</span>
                <span className="font-mono font-bold text-emerald-600">{summary.categories.residential.total_clean_count} 个连接</span>
              </div>
            </div>
          )}

          {/* C. 美国家宽分组 */}
          {summary?.categories?.us_resi && (
            <div className="bg-white border border-slate-200/90 rounded-xl p-4 shadow-2xs hover:border-slate-300 transition flex flex-col justify-between">
              <div>
                <div className="flex items-center justify-between mb-2">
                  <span className="px-2 py-0.5 rounded text-2xs font-bold uppercase bg-blue-100 text-blue-800 flex items-center space-x-1">
                    <span>🇺🇸</span>
                    <span>美区全家桶</span>
                  </span>
                  <button 
                    onClick={() => toggleCard('us_resi')} 
                    className="text-slate-400 hover:text-slate-600"
                  >
                    {expandedCards['us_resi'] ? <ChevronUp className="w-3.5 h-3.5" /> : <ChevronDown className="w-3.5 h-3.5" />}
                  </button>
                </div>
                <h4 className="font-bold text-slate-900 text-sm mb-1">
                  {summary.categories.us_resi.display_name}
                </h4>
                <p className="text-xs text-slate-500 mb-3">
                  美西4837骨干 + 美国家宽直连 + 4837全网状美国家宽中继池。
                </p>
                <div className="flex items-center justify-between text-xs font-mono bg-blue-50/60 p-2 rounded-lg text-blue-900 mb-2">
                  <span>纯净节点数</span>
                  <span className="font-bold">{summary.categories.us_resi.total_clean_count}</span>
                </div>
              </div>

              {expandedCards['us_resi'] && (
                <div className="mt-2 pt-2 border-t border-slate-100 space-y-1 max-h-32 overflow-y-auto text-2xs font-mono">
                  {summary.categories.us_resi.nodes.map(n => (
                    <div key={n.id} className="flex items-center justify-between text-slate-600 py-0.5">
                      <span className="truncate pr-2">{n.name}</span>
                      <span className="text-slate-400">{n.kind}</span>
                    </div>
                  ))}
                </div>
              )}

              <div className="pt-2 border-t border-slate-100 flex items-center justify-between text-2xs text-slate-400">
                <span>策略标识</span>
                <span className="font-mono font-semibold text-slate-600">ashan-us-resi</span>
              </div>
            </div>
          )}

          {/* D. 日本家宽分组 */}
          {summary?.categories?.jp_resi && (
            <div className="bg-white border border-slate-200/90 rounded-xl p-4 shadow-2xs hover:border-slate-300 transition flex flex-col justify-between">
              <div>
                <div className="flex items-center justify-between mb-2">
                  <span className="px-2 py-0.5 rounded text-2xs font-bold uppercase bg-rose-100 text-rose-800 flex items-center space-x-1">
                    <span>🇯🇵</span>
                    <span>日区全家桶</span>
                  </span>
                  <button 
                    onClick={() => toggleCard('jp_resi')} 
                    className="text-slate-400 hover:text-slate-600"
                  >
                    {expandedCards['jp_resi'] ? <ChevronUp className="w-3.5 h-3.5" /> : <ChevronDown className="w-3.5 h-3.5" />}
                  </button>
                </div>
                <h4 className="font-bold text-slate-900 text-sm mb-1">
                  {summary.categories.jp_resi.display_name}
                </h4>
                <p className="text-xs text-slate-500 mb-3">
                  东京软银/IIJ骨干 + 日本家宽直连 + 日本链式中继池。
                </p>
                <div className="flex items-center justify-between text-xs font-mono bg-rose-50/60 p-2 rounded-lg text-rose-900 mb-2">
                  <span>纯净节点数</span>
                  <span className="font-bold">{summary.categories.jp_resi.total_clean_count}</span>
                </div>
              </div>

              {expandedCards['jp_resi'] && (
                <div className="mt-2 pt-2 border-t border-slate-100 space-y-1 max-h-32 overflow-y-auto text-2xs font-mono">
                  {summary.categories.jp_resi.nodes.map(n => (
                    <div key={n.id} className="flex items-center justify-between text-slate-600 py-0.5">
                      <span className="truncate pr-2">{n.name}</span>
                      <span className="text-slate-400">{n.kind}</span>
                    </div>
                  ))}
                </div>
              )}

              <div className="pt-2 border-t border-slate-100 flex items-center justify-between text-2xs text-slate-400">
                <span>策略标识</span>
                <span className="font-mono font-semibold text-slate-600">ashan-jp-resi</span>
              </div>
            </div>
          )}

          {/* E. 台湾家宽 */}
          {summary?.categories?.tw_resi && (
            <div className="bg-white border border-slate-200/90 rounded-xl p-4 shadow-2xs hover:border-slate-300 transition flex flex-col justify-between">
              <div>
                <div className="flex items-center justify-between mb-2">
                  <span className="px-2 py-0.5 rounded text-2xs font-bold uppercase bg-emerald-100 text-emerald-800 flex items-center space-x-1">
                    <span>🇹🇼</span>
                    <span>台区全家桶</span>
                  </span>
                  <button 
                    onClick={() => toggleCard('tw_resi')} 
                    className="text-slate-400 hover:text-slate-600"
                  >
                    {expandedCards['tw_resi'] ? <ChevronUp className="w-3.5 h-3.5" /> : <ChevronDown className="w-3.5 h-3.5" />}
                  </button>
                </div>
                <h4 className="font-bold text-slate-900 text-sm mb-1">
                  {summary.categories.tw_resi.display_name}
                </h4>
                <p className="text-xs text-slate-500 mb-3">
                  HiNet 中华电信原生家宽 + 台湾链式中继池。
                </p>
                <div className="flex items-center justify-between text-xs font-mono bg-emerald-50/60 p-2 rounded-lg text-emerald-900 mb-2">
                  <span>纯净节点数</span>
                  <span className="font-bold">{summary.categories.tw_resi.total_clean_count}</span>
                </div>
              </div>

              {expandedCards['tw_resi'] && (
                <div className="mt-2 pt-2 border-t border-slate-100 space-y-1 max-h-32 overflow-y-auto text-2xs font-mono">
                  {summary.categories.tw_resi.nodes.map(n => (
                    <div key={n.id} className="flex items-center justify-between text-slate-600 py-0.5">
                      <span className="truncate pr-2">{n.name}</span>
                      <span className="text-slate-400">{n.kind}</span>
                    </div>
                  ))}
                </div>
              )}

              <div className="pt-2 border-t border-slate-100 flex items-center justify-between text-2xs text-slate-400">
                <span>策略标识</span>
                <span className="font-mono font-semibold text-slate-600">ashan-tw-resi</span>
              </div>
            </div>
          )}

          {/* F. 其他自建分组 */}
          {summary?.categories?.other_resi && (
            <div className="bg-white border border-slate-200/90 rounded-xl p-4 shadow-2xs hover:border-slate-300 transition flex flex-col justify-between">
              <div>
                <div className="flex items-center justify-between mb-2">
                  <span className="px-2 py-0.5 rounded text-2xs font-bold uppercase bg-amber-100 text-amber-800 flex items-center space-x-1">
                    <span>🌐</span>
                    <span>其他自建池</span>
                  </span>
                  <button 
                    onClick={() => toggleCard('other_resi')} 
                    className="text-slate-400 hover:text-slate-600"
                  >
                    {expandedCards['other_resi'] ? <ChevronUp className="w-3.5 h-3.5" /> : <ChevronDown className="w-3.5 h-3.5" />}
                  </button>
                </div>
                <h4 className="font-bold text-slate-900 text-sm mb-1">
                  {summary.categories.other_resi.display_name}
                </h4>
                <p className="text-xs text-slate-500 mb-3">
                  新加坡、韩国等其他原生家宽直连与链式中继池。
                </p>
                <div className="flex items-center justify-between text-xs font-mono bg-amber-50/60 p-2 rounded-lg text-amber-900 mb-2">
                  <span>纯净节点数</span>
                  <span className="font-bold">{summary.categories.other_resi.total_clean_count}</span>
                </div>
              </div>

              {expandedCards['other_resi'] && (
                <div className="mt-2 pt-2 border-t border-slate-100 space-y-1 max-h-32 overflow-y-auto text-2xs font-mono">
                  {summary.categories.other_resi.nodes.map(n => (
                    <div key={n.id} className="flex items-center justify-between text-slate-600 py-0.5">
                      <span className="truncate pr-2">{n.name}</span>
                      <span className="text-slate-400">{n.kind}</span>
                    </div>
                  ))}
                </div>
              )}

              <div className="pt-2 border-t border-slate-100 flex items-center justify-between text-2xs text-slate-400">
                <span>策略标识</span>
                <span className="font-mono font-semibold text-slate-600">ashan-other</span>
              </div>
            </div>
          )}

          {/* G. 骨干线路机分组 */}
          {summary?.categories?.transit && (
            <div className="bg-white border border-slate-200/90 rounded-xl p-4 shadow-2xs hover:border-slate-300 transition flex flex-col justify-between">
              <div>
                <div className="flex items-center justify-between mb-2">
                  <span className="px-2 py-0.5 rounded text-2xs font-bold uppercase bg-purple-100 text-purple-800">
                    纯骨干直连
                  </span>
                  <span className="text-2xs text-purple-600 font-mono font-bold">VLESS Reality</span>
                </div>
                <h4 className="font-bold text-slate-900 text-sm mb-1">
                  {summary.categories.transit.display_name}
                </h4>
                <p className="text-xs text-slate-500 mb-3">
                  自有专线骨干机，CF 443 WS 在直连正常时静默待命。
                </p>
                <div className="flex items-center justify-between text-xs font-mono bg-purple-50/60 p-2 rounded-lg text-purple-900 mb-2">
                  <span>骨干专线数</span>
                  <span className="font-bold">{summary.categories.transit.transit_count}</span>
                </div>
              </div>
              <div className="pt-2 border-t border-slate-100 flex items-center justify-between text-2xs text-slate-400">
                <span>策略标识</span>
                <span className="font-mono font-semibold text-slate-600">ashan-transit</span>
              </div>
            </div>
          )}

          {/* F. 送中物理强隔离分组 */}
          {summary?.categories?.china_leaks && (
            <div className="bg-white border border-red-200/90 rounded-xl p-4 shadow-2xs hover:border-red-300 transition flex flex-col justify-between">
              <div>
                <div className="flex items-center justify-between mb-2">
                  <span className="px-2 py-0.5 rounded text-2xs font-bold uppercase bg-red-100 text-red-800 flex items-center space-x-1">
                    <ShieldAlert className="w-3 h-3" />
                    <span>送中强隔离</span>
                  </span>
                  <span className="text-2xs text-red-600 font-mono font-bold">严禁中继分流</span>
                </div>
                <h4 className="font-bold text-slate-900 text-sm mb-1">
                  自建-送中隔离池
                </h4>
                <p className="text-xs text-slate-500 mb-3">
                  Google NCR 判定为 CN 出口的自建节点，强制物理隔离。
                </p>
                <div className="flex items-center justify-between text-xs font-mono bg-red-50 p-2 rounded-lg text-red-900 mb-2">
                  <span>隔离节点数</span>
                  <span className="font-bold">{summary.categories.china_leaks.length}</span>
                </div>
              </div>
              <div className="pt-2 border-t border-slate-100 flex items-center justify-between text-2xs text-slate-400">
                <span>策略标识</span>
                <span className="font-mono font-semibold text-red-600">ashan-china-leak</span>
              </div>
            </div>
          )}
        </div>
      </div>

      {/* Client-visible groups are exclusively user-defined via PolicyGroupStudio */}
      <PolicyGroupStudio />

      {/* 4. 底部：线路➔家宽链式中继定制管理 (Custom Chain Matrix) */}
      <div className="bg-white border border-slate-200/80 rounded-2xl p-5 shadow-xs">
        <div className="mb-4">
          <h3 className="text-sm font-bold text-slate-900 flex items-center space-x-2">
            <Link2 className="w-4 h-4 text-indigo-600" />
            <span>线路➔家宽显式链式中继定制 (Custom Chain Matrix)</span>
          </h3>
          <p className="text-xs text-slate-500">
            满足特定业务需要：显式指定某台线路机拉某台家宽，生成常驻 dialerProxy 链式节点。
          </p>
        </div>

        {/* 显式绑定快速表单 */}
        <div className="bg-indigo-50/40 border border-indigo-100 rounded-xl p-4 mb-4">
          <div className="flex flex-col sm:flex-row items-stretch sm:items-center gap-3">
            <div className="flex-1">
              <label className="block text-2xs font-semibold text-slate-600 mb-1">
                选择前置线路机 (Transit Backbone)
              </label>
              <select
                value={selectedTransit}
                onChange={e => setSelectedTransit(e.target.value)}
                className="w-full text-xs rounded-lg border-slate-200 shadow-2xs bg-white py-1.5 px-2.5 focus:border-indigo-500 focus:ring-indigo-500 font-mono"
              >
                <option value="">-- 选择线路骨干专线 --</option>
                {transitOptions.map(opt => (
                  <option key={opt} value={opt}>{opt}</option>
                ))}
              </select>
            </div>

            <div className="flex items-center justify-center pt-4 sm:pt-4 text-indigo-400">
              ➔
            </div>

            <div className="flex-1">
              <label className="block text-2xs font-semibold text-slate-600 mb-1">
                选择落地家宽机 (Residential Target)
              </label>
              <select
                value={selectedResi}
                onChange={e => setSelectedResi(e.target.value)}
                className="w-full text-xs rounded-lg border-slate-200 shadow-2xs bg-white py-1.5 px-2.5 focus:border-indigo-500 focus:ring-indigo-500 font-mono"
              >
                <option value="">-- 选择住宅家宽节点 --</option>
                {resiOptions.map(opt => (
                  <option key={opt} value={opt}>{opt}</option>
                ))}
              </select>
            </div>

            <div className="sm:self-end pt-1">
              <button
                onClick={handleBindChain}
                disabled={actionLoading === 'bind_chain' || !selectedTransit || !selectedResi}
                className="w-full sm:w-auto px-4 py-1.5 bg-indigo-600 hover:bg-indigo-700 text-white rounded-lg text-xs font-semibold shadow-2xs transition flex items-center justify-center space-x-1.5 disabled:opacity-50"
              >
                <Link2 className="w-3.5 h-3.5" />
                <span>立即绑定链式中继</span>
              </button>
            </div>
          </div>
        </div>

        {/* 已生效显式绑定列表 */}
        {customChains.length > 0 ? (
          <div className="overflow-x-auto">
            <table className="w-full text-left border-collapse text-xs">
              <thead>
                <tr className="border-b border-slate-200 text-2xs font-semibold text-slate-400 uppercase tracking-wider bg-slate-50/50">
                  <th className="py-2.5 px-3">前置线路机</th>
                  <th className="py-2.5 px-3">落地家宽</th>
                  <th className="py-2.5 px-3">生成链式节点名</th>
                  <th className="py-2.5 px-3">绑定时间</th>
                  <th className="py-2.5 px-3 text-right">操作</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-100 font-mono">
                {customChains.map(c => (
                  <tr key={c.id} className="hover:bg-slate-50/80 transition">
                    <td className="py-2.5 px-3 font-semibold text-slate-800">{c.transit_name}</td>
                    <td className="py-2.5 px-3 text-slate-700">{c.residential_name}</td>
                    <td className="py-2.5 px-3 text-indigo-700 font-semibold">{c.chain_name}</td>
                    <td className="py-2.5 px-3 text-slate-400 text-2xs">{c.created_at}</td>
                    <td className="py-2.5 px-3 text-right">
                      <button
                        onClick={() => handleUnbindChain(c.id || c.chain_name)}
                        disabled={actionLoading === `unbind_${c.id}`}
                        className="text-red-500 hover:text-red-700 font-sans font-semibold text-xs"
                      >
                        解绑
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <div className="text-center py-6 text-slate-400 text-xs">
            暂无显式定制链式绑定，系统默认通过区域就近原则（美拉美、日拉日）全网状自动池化。
          </div>
        )}
      </div>

      {/* 新建自定义分组模态弹窗 */}
      {showCreateModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/50 backdrop-blur-xs p-4">
          <div className="bg-white rounded-2xl shadow-xl border border-slate-200 w-full max-w-2xl overflow-hidden animate-in fade-in zoom-in-95 duration-150">
            <div className="p-4 border-b border-slate-100 flex items-center justify-between">
              <div className="flex items-center space-x-2">
                <Sliders className="w-5 h-5 text-emerald-600" />
                <h3 className="font-bold text-slate-900 text-base">创建自定义策略分组</h3>
              </div>
              <button
                onClick={() => setShowCreateModal(false)}
                className="text-slate-400 hover:text-slate-600 p-1 rounded-lg"
              >
                <X className="w-5 h-5" />
              </button>
            </div>

            <div className="p-6 space-y-4 max-h-[80vh] overflow-y-auto">
              {/* 名称与标识 */}
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                <div>
                  <label className="block text-xs font-semibold text-slate-700 mb-1">
                    分组英文标识 (ID) *
                  </label>
                  <input
                    type="text"
                    placeholder="e.g. my-us-netflix"
                    value={modalForm.name}
                    onChange={e => setModalForm(prev => ({ ...prev, name: e.target.value }))}
                    className="w-full text-xs rounded-lg border-slate-200 px-3 py-2 font-mono focus:border-emerald-500 focus:ring-emerald-500"
                  />
                </div>
                <div>
                  <label className="block text-xs font-semibold text-slate-700 mb-1">
                    中文展示名称
                  </label>
                  <input
                    type="text"
                    placeholder="e.g. 美区极速流媒体"
                    value={modalForm.display_name}
                    onChange={e => setModalForm(prev => ({ ...prev, display_name: e.target.value }))}
                    className="w-full text-xs rounded-lg border-slate-200 px-3 py-2 focus:border-emerald-500 focus:ring-emerald-500"
                  />
                </div>
              </div>

              {/* 国家/地区多选积木 */}
              <div>
                <label className="block text-xs font-semibold text-slate-700 mb-1.5">
                  国家/地区选择 (Countries)
                </label>
                <div className="flex flex-wrap gap-2">
                  {['US', 'JP', 'SG', 'HK', 'OTHER'].map(c => {
                    const active = modalForm.countries.includes(c);
                    return (
                      <button
                        key={c}
                        type="button"
                        onClick={() => {
                          setModalForm(prev => ({
                            ...prev,
                            countries: active 
                              ? prev.countries.filter(x => x !== c)
                              : [...prev.countries, c]
                          }));
                        }}
                        className={`px-3 py-1.5 rounded-lg text-xs font-mono font-semibold border transition ${
                          active 
                            ? 'bg-blue-600 border-blue-600 text-white' 
                            : 'bg-slate-50 border-slate-200 text-slate-700 hover:bg-slate-100'
                        }`}
                      >
                        {c === 'US' ? '🇺🇸 US 美国' : c === 'JP' ? '🇯🇵 JP 日本' : c === 'SG' ? '🇸🇬 SG 新加坡' : c === 'HK' ? '🇭🇰 HK 香港' : '🌐 其他'}
                      </button>
                    );
                  })}
                </div>
              </div>

              {/* 节点角色多选积木 */}
              <div>
                <label className="block text-xs font-semibold text-slate-700 mb-1.5">
                  节点角色类型 (Kinds)
                </label>
                <div className="flex flex-wrap gap-2">
                  {[
                    { key: 'transit', label: '骨干线路 (transit)' },
                    { key: 'residential', label: '家宽直连 (residential)' },
                    { key: 'chain', label: '链式中继 (chain)' },
                  ].map(k => {
                    const active = modalForm.kinds.includes(k.key);
                    return (
                      <button
                        key={k.key}
                        type="button"
                        onClick={() => {
                          setModalForm(prev => ({
                            ...prev,
                            kinds: active 
                              ? prev.kinds.filter(x => x !== k.key)
                              : [...prev.kinds, k.key]
                          }));
                        }}
                        className={`px-3 py-1.5 rounded-lg text-xs font-mono font-semibold border transition ${
                          active 
                            ? 'bg-purple-600 border-purple-600 text-white' 
                            : 'bg-slate-50 border-slate-200 text-slate-700 hover:bg-slate-100'
                        }`}
                      >
                        {k.label}
                      </button>
                    );
                  })}
                </div>
              </div>

              {/* 纯净度过滤开关 */}
              <div className="flex items-center space-x-2 pt-1">
                <input
                  type="checkbox"
                  id="clean_only_cb"
                  checked={modalForm.clean_only}
                  onChange={e => setModalForm(prev => ({ ...prev, clean_only: e.target.checked }))}
                  className="rounded text-emerald-600 focus:ring-emerald-500 h-4 w-4"
                />
                <label htmlFor="clean_only_cb" className="text-xs font-semibold text-slate-800 cursor-pointer">
                  只保留 Google NCR 纯净出口节点 (强行排除送中节点)
                </label>
              </div>

              {/* 关键字过滤 */}
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 pt-1">
                <div>
                  <label className="block text-2xs font-semibold text-slate-600 mb-1">
                    包含关键词 (逗号分隔)
                  </label>
                  <input
                    type="text"
                    placeholder="e.g. 4837, 家宽"
                    value={modalForm.keywords}
                    onChange={e => setModalForm(prev => ({ ...prev, keywords: e.target.value }))}
                    className="w-full text-xs rounded-lg border-slate-200 px-3 py-1.5 font-mono"
                  />
                </div>
                <div>
                  <label className="block text-2xs font-semibold text-slate-600 mb-1">
                    排除关键词 (逗号分隔)
                  </label>
                  <input
                    type="text"
                    placeholder="e.g. 软银, 备用"
                    value={modalForm.exclude}
                    onChange={e => setModalForm(prev => ({ ...prev, exclude: e.target.value }))}
                    className="w-full text-xs rounded-lg border-slate-200 px-3 py-1.5 font-mono"
                  />
                </div>
              </div>

              {/* 实时预演 Live Preview 区域 */}
              <div className="bg-slate-50 border border-slate-200 rounded-xl p-3.5 space-y-2">
                <div className="flex items-center justify-between text-xs">
                  <span className="font-semibold text-slate-700 flex items-center space-x-1.5">
                    <Radio className={`w-3.5 h-3.5 text-emerald-600 ${evaluating ? 'animate-pulse' : ''}`} />
                    <span>实时试算预演 (Live Preview)</span>
                  </span>
                  <span className="font-mono font-bold text-emerald-700">
                    命中 {previewResult.count} 个节点
                  </span>
                </div>

                <div className="max-h-24 overflow-y-auto flex flex-wrap gap-1.5 text-2xs font-mono">
                  {previewResult.nodes.length > 0 ? (
                    previewResult.nodes.map((node, i) => (
                      <span key={i} className="px-2 py-0.5 rounded bg-white border border-slate-200 text-slate-700 truncate max-w-xs">
                        {node}
                      </span>
                    ))
                  ) : (
                    <span className="text-slate-400 text-2xs">当前过滤规则未匹配到任何原子节点</span>
                  )}
                </div>
              </div>
            </div>

            <div className="p-4 bg-slate-50 border-t border-slate-100 flex items-center justify-between">
              <span className="text-2xs text-slate-400">
                保存后将自动挂载至 Sub-Store 并生成持久化订阅快照
              </span>
              <div className="flex items-center space-x-2">
                <button
                  type="button"
                  onClick={() => setShowCreateModal(false)}
                  className="px-4 py-2 border border-slate-200 text-slate-600 hover:bg-white rounded-lg text-xs font-semibold"
                >
                  取消
                </button>
                <button
                  type="button"
                  onClick={handleSaveGroup}
                  disabled={actionLoading === 'save_group' || !modalForm.name.trim()}
                  className="px-4 py-2 bg-emerald-600 hover:bg-emerald-700 text-white rounded-lg text-xs font-semibold shadow-2xs transition flex items-center space-x-1.5 disabled:opacity-50"
                >
                  <Save className="w-4 h-4" />
                  <span>保存并立即同步</span>
                </button>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};
