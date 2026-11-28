import React, { useState, useEffect, useMemo, useCallback } from 'react';
import { 
  X, 
  RefreshCw, 
  Layers, 
  FileCode, 
  ShieldCheck, 
  Lock, 
  Plus, 
  Check, 
  Copy, 
  Search, 
  ChevronDown, 
  ChevronUp, 
  Zap, 
  Globe, 
  Link2, 
  Trash2, 
  Tag, 
  ArrowRight,
  Sliders,
  Filter,
  CheckCircle2,
  ExternalLink,
  AlertTriangle
} from 'lucide-react';
import { api } from '../api/client';
import { ConfirmActionButton } from './ConfirmActionButton';
import { useFeedback } from '../context/FeedbackContext';
import type { 
  SubscriptionInspectResponse, 
  GroupInspectItem, 
  ProxyInspectSummary, 
  RuleInspectItem,
  AtomNode,
  CustomGroupDef,
  VerifySubscriptionResponse
} from '../types';

interface SubscriptionTopologyDrawerProps {
  subscriptionName: string | null;
  availableSubscriptions?: string[];
  onClose: () => void;
  onSelectSubscription?: (name: string) => void;
}

export const SubscriptionTopologyDrawer: React.FC<SubscriptionTopologyDrawerProps> = ({
  subscriptionName,
  availableSubscriptions = ['ashan', 'Q', 'business', 'ashan-master', 'ashan-纯净'],
  onClose,
  onSelectSubscription
}) => {
  const { pushActivity } = useFeedback();
  const [currentSub, setCurrentSub] = useState<string>(subscriptionName || 'ashan');
  const [data, setData] = useState<SubscriptionInspectResponse | null>(null);
  const [verifyData, setVerifyData] = useState<VerifySubscriptionResponse | null>(null);
  const [atoms, setAtoms] = useState<AtomNode[]>([]);
  const [loading, setLoading] = useState(false);
  const [verifyLoading, setVerifyLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Active Tab: 'groups' | 'verify' | 'rules'
  const [activeTab, setActiveTab] = useState<'groups' | 'verify' | 'rules'>('groups');

  // Group filter & search
  const [groupFilter, setGroupFilter] = useState<'all' | 'self' | 'external' | 'app'>('all');
  const [searchQuery, setSearchQuery] = useState('');

  // Expanded group IDs
  const [expandedGroups, setExpandedGroups] = useState<Set<string>>(new Set());

  // Editing state for fool-proof node picker inside a self-hosted group
  const [pickerGroupId, setPickerGroupId] = useState<string | null>(null);
  const [selectedNodeNames, setSelectedNodeNames] = useState<string[]>([]);
  const [pickerSearch, setPickerSearch] = useState('');
  const [pickerTagFilter, setPickerTagFilter] = useState<string>('all');
  const [savingGroup, setSavingGroup] = useState(false);

  // New tag prompt state
  const [tagInputGroupId, setTagInputGroupId] = useState<string | null>(null);
  const [newTagText, setNewTagText] = useState('');

  // Script copy state
  const [copiedScript, setCopiedScript] = useState(false);

  // Sync internal sub name when prop changes
  useEffect(() => {
    if (subscriptionName && subscriptionName !== currentSub) {
      setCurrentSub(subscriptionName);
    }
  }, [subscriptionName]);

  // Load topology and verify data
  const loadInspect = useCallback(async (name: string) => {
    setLoading(true);
    setError(null);
    try {
      // 1. 并发请求内存快照拓扑与原子节点库 (瞬时返回，<250ms)
      const [res, atomsRes] = await Promise.all([
        api.getSubscriptionInspect(name),
        api.getAtoms({}).catch(() => ({ atoms: [], count: 0, status: '' })),
      ]);
      setData(res);
      setAtoms(atomsRes.atoms || []);
      // Auto-expand the first self-hosted group if present
      const firstSelf = res.groups.find(g => !g.is_external);
      if (firstSelf) {
        setExpandedGroups(new Set([firstSelf.name]));
      }
      setLoading(false); // 立即解锁 UI，展示策略分组矩阵！

      // 2. 异步在后台发起双域名与质量验证 (不阻塞抽屉拓扑展示)
      setVerifyLoading(true);
      api.verifySubscription(name)
        .then(vRes => {
          if (vRes) setVerifyData(vRes);
        })
        .catch(err => {
          console.warn('verifySubscription in background failed:', err);
        })
        .finally(() => {
          setVerifyLoading(false);
        });
    } catch (err: any) {
      setError(err.message || '加载订阅拓扑失败');
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (currentSub) {
      loadInspect(currentSub);
    }
  }, [currentSub, loadInspect]);

  // Handle ESC key to close
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [onClose]);

  const toggleGroup = (name: string) => {
    setExpandedGroups(prev => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });
  };

  // Filtered groups
  const filteredGroups = useMemo(() => {
    if (!data) return [];
    return data.groups.filter(g => {
      // Type filter
      if (groupFilter === 'self' && g.is_external) return false;
      if (groupFilter === 'external' && !g.is_external) return false;
      if (groupFilter === 'app' && g.category !== 'app_routing') return false;

      // Search query
      if (searchQuery.trim()) {
        const q = searchQuery.toLowerCase();
        const matchName = g.name.toLowerCase().includes(q) || (g.display_name || '').toLowerCase().includes(q);
        const matchProxy = g.proxies.some(p => p.name.toLowerCase().includes(q));
        const matchTag = g.tags.some(t => t.toLowerCase().includes(q));
        return matchName || matchProxy || matchTag;
      }
      return true;
    });
  }, [data, groupFilter, searchQuery]);

  // Open node picker for self-hosted group
  const handleOpenPicker = (group: GroupInspectItem) => {
    setPickerGroupId(group.name);
    setSelectedNodeNames(group.proxies.map(p => p.name));
    setPickerSearch('');
    setPickerTagFilter('all');
  };

  // Toggle node in picker
  const handleTogglePickerNode = (nodeName: string) => {
    setSelectedNodeNames(prev => 
      prev.includes(nodeName) ? prev.filter(n => n !== nodeName) : [...prev, nodeName]
    );
  };

  // Quick tag batch selection
  const handleBatchSelectByTag = (tag: string) => {
    const matchingAtoms = atoms.filter(a => {
      if (tag === 'us_resi') return a.country === 'US' && a.kind === 'residential';
      if (tag === 'jp_resi') return a.country === 'JP' && a.kind === 'residential';
      if (tag === 'transit') return a.kind === 'transit';
      if (tag === 'chain') return a.kind === 'chain';
      if (tag === 'tw') return a.country === 'TW';
      if (tag === 'clean') return !a.china_exit;
      return false;
    });
    const names = matchingAtoms.map(a => a.name);
    setSelectedNodeNames(prev => Array.from(new Set([...prev, ...names])));
  };

  const resolveProfile = (sub: string): 'q' | 'business' | 'ashan' => {
    const s = (sub || '').toLowerCase();
    if (s === 'q') return 'q';
    if (s === 'business') return 'business';
    return 'ashan';
  };

  const resolveGroupId = (group: GroupInspectItem): string => {
    if (group.custom_def?.name) return group.custom_def.name;
    if (/^[a-zA-Z][a-zA-Z0-9_-]{0,63}$/.test(group.name)) return group.name;
    const prefix = resolveProfile(currentSub);
    return `${prefix}-custom-${Date.now().toString(36)}`;
  };

  // Filtered atoms inside the Node Picker
  const pickerFilteredAtoms = useMemo(() => {
    const q = pickerSearch.trim().toLowerCase();
    return atoms.filter(a => {
      if (pickerTagFilter === 'transit' && a.kind !== 'transit') return false;
      if (pickerTagFilter === 'residential' && a.kind !== 'residential') return false;
      if (pickerTagFilter === 'chain' && a.kind !== 'chain') return false;
      if (pickerTagFilter === 'clean' && a.china_exit) return false;
      if (q) {
        return a.name.toLowerCase().includes(q) || a.country.toLowerCase().includes(q) || (a.tags || []).some(t => t.toLowerCase().includes(q));
      }
      return true;
    });
  }, [atoms, pickerSearch, pickerTagFilter]);

  // Save updated nodes to custom group (preserving profile, type, order, add_to_entry, tags)
  const handleSavePickerNodes = async (group: GroupInspectItem) => {
    if (selectedNodeNames.length === 0) {
      pushActivity({ level: 'warn', source: '订阅拓扑', title: '手动模式至少需要保留或勾选 1 个节点' });
      return;
    }
    setSavingGroup(true);
    try {
      const profile = resolveProfile(currentSub);
      const gid = resolveGroupId(group);
      const def: CustomGroupDef = {
        ...(group.custom_def || {}),
        name: gid,
        display_name: group.display_name || group.name,
        enabled: true,
        type: group.custom_def?.type || group.type || 'select',
        filter: {
          ...(group.custom_def?.filter || {}),
          mode: 'manual',
          node_ids: selectedNodeNames,
        }
      };
      await api.createPolicyGroup(def, profile);
      setPickerGroupId(null);
      await loadInspect(currentSub);
      pushActivity({ level: 'success', source: '订阅拓扑', title: `已保存分组「${group.display_name}」节点列表` });
    } catch (err: any) {
      pushActivity({ level: 'error', source: '订阅拓扑', title: '保存分组节点失败', detail: err.message || '未知错误' });
    } finally {
      setSavingGroup(false);
    }
  };

  // Remove single node from group
  const handleRemoveNode = async (group: GroupInspectItem, proxyName: string) => {
    const remaining = group.proxies.map(p => p.name).filter(n => n !== proxyName);
    if (remaining.length === 0) {
      pushActivity({
        level: 'warn',
        source: '订阅拓扑',
        title: '至少需保留 1 个节点',
        detail: '如需删除整个分组，请在「订阅与策略编排」中停用或删除。',
      });
      return;
    }
    try {
      const profile = resolveProfile(currentSub);
      const gid = resolveGroupId(group);
      const def: CustomGroupDef = {
        ...(group.custom_def || {}),
        name: gid,
        display_name: group.display_name,
        enabled: true,
        type: group.custom_def?.type || group.type || 'select',
        filter: {
          ...(group.custom_def?.filter || {}),
          mode: 'manual',
          node_ids: remaining,
        }
      };
      await api.createPolicyGroup(def, profile);
      await loadInspect(currentSub);
      pushActivity({ level: 'info', source: '订阅拓扑', title: `已从「${group.display_name}」移除节点 ${proxyName}` });
    } catch (err: any) {
      pushActivity({ level: 'error', source: '订阅拓扑', title: '移除节点失败', detail: err.message });
    }
  };

  // Add new tag to group and persist to backend
  const handleAddTag = async (group: GroupInspectItem) => {
    const tagVal = newTagText.trim();
    if (!tagVal) return;
    try {
      const profile = resolveProfile(currentSub);
      const gid = resolveGroupId(group);
      const baseCustom = group.custom_def;
      const mergedTags = Array.from(new Set([...(baseCustom?.tags || []), tagVal]));
      const fallbackFilter = baseCustom?.filter || {
        mode: 'manual' as const,
        node_ids: group.proxies.map(p => p.name).filter(Boolean),
      };
      const def: CustomGroupDef = {
        ...(baseCustom || {}),
        name: gid,
        display_name: group.display_name || group.name,
        enabled: baseCustom?.enabled ?? true,
        type: baseCustom?.type || group.type || 'select',
        tags: mergedTags,
        filter: fallbackFilter,
      };
      await api.createPolicyGroup(def, profile);
      setTagInputGroupId(null);
      setNewTagText('');
      await loadInspect(currentSub);
      pushActivity({ level: 'success', source: '订阅拓扑', title: `已为「${group.display_name}」添加标签 ${tagVal}` });
    } catch (err: any) {
      pushActivity({ level: 'error', source: '订阅拓扑', title: '保存便签失败', detail: err.message || '未知错误' });
    }
  };

  // Copy JS script
  const handleCopyScript = (text: string) => {
    navigator.clipboard.writeText(text);
    setCopiedScript(true);
    setTimeout(() => setCopiedScript(false), 2000);
  };

  if (!subscriptionName) return null;

  return (
    <div className="fixed inset-0 z-50 flex justify-end">
      {/* Backdrop */}
      <div 
        className="fixed inset-0 bg-slate-900/50 backdrop-blur-xs transition-opacity animate-in fade-in duration-200"
        onClick={onClose}
      />

      {/* Drawer Container */}
      <div className="relative w-full sm:max-w-3xl md:max-w-4xl bg-white h-full shadow-2xl flex flex-col z-10 border-l border-slate-200 animate-in slide-in-from-right duration-300">
        
        {/* 1. Header Bar */}
        <div className="px-5 py-4 border-b border-slate-200/80 bg-slate-50/80 flex flex-col gap-3 flex-shrink-0">
          <div className="flex items-center justify-between">
            <div className="flex items-center space-x-2.5">
              <div className="w-8 h-8 rounded-lg bg-indigo-600 text-white flex items-center justify-center shadow-xs">
                <Layers className="w-4 h-4" />
              </div>
              <div>
                <div className="flex items-center space-x-2">
                  <h2 className="text-base font-bold text-slate-900">
                    SubStore 订阅分组与分流拓扑透视
                  </h2>
                  <span className="text-2xs px-2 py-0.5 rounded-full font-mono font-bold bg-indigo-100 text-indigo-700">
                    {currentSub}
                  </span>
                </div>
                <p className="text-xs text-slate-500">
                  动态穿透分析订阅内部策略组、自建与外部标记、成员节点及分流算子驱动脚本
                </p>
              </div>
            </div>

            <div className="flex items-center space-x-2">
              <button
                onClick={() => loadInspect(currentSub)}
                disabled={loading}
                title="刷新拓扑"
                className="p-1.5 rounded-lg border border-slate-200 hover:bg-slate-100 text-slate-600 transition disabled:opacity-50"
              >
                <RefreshCw className={`w-4 h-4 ${loading ? 'animate-spin' : ''}`} />
              </button>
              <button
                onClick={onClose}
                title="关闭抽屉"
                className="p-1.5 rounded-lg border border-slate-200 hover:bg-rose-50 hover:text-rose-600 hover:border-rose-200 text-slate-500 transition"
              >
                <X className="w-4 h-4" />
              </button>
            </div>
          </div>

          {/* Quick Subscription Switcher & Metrics */}
          <div className="flex flex-wrap items-center justify-between gap-2 pt-2 border-t border-slate-200/60 text-xs">
            {/* Subscription Switcher Pills */}
            <div className="flex items-center space-x-1.5 bg-white p-1 rounded-lg border border-slate-200 shadow-2xs">
              <span className="text-2xs font-semibold text-slate-400 px-1.5">目标订阅:</span>
              {availableSubscriptions.map(sub => (
                <button
                  key={sub}
                  onClick={() => {
                    setCurrentSub(sub);
                    if (onSelectSubscription) onSelectSubscription(sub);
                  }}
                  className={`px-2.5 py-1 rounded text-xs font-mono font-medium transition ${
                    currentSub === sub 
                      ? 'bg-indigo-600 text-white font-bold shadow-2xs' 
                      : 'text-slate-600 hover:bg-slate-100'
                  }`}
                >
                  {sub}
                </button>
              ))}
            </div>

            {/* Metrics Chips */}
            {data && (
              <div className="flex items-center space-x-2 text-2xs font-mono">
                <span className="px-2 py-0.5 rounded bg-slate-100 text-slate-700">
                  总代理: <strong className="text-slate-900">{data.total_proxies}</strong>
                </span>
                <span className="px-2 py-0.5 rounded bg-emerald-100 text-emerald-800 font-bold">
                  自建组: {data.self_groups}
                </span>
                <span className="px-2 py-0.5 rounded bg-blue-100 text-blue-800">
                  外部组: {data.ext_groups}
                </span>
                <span className="px-2 py-0.5 rounded bg-purple-100 text-purple-800">
                  规则: {data.rules.length}
                </span>
              </div>
            )}
          </div>
        </div>

        {/* 2. Tab Navigation */}
        <div className="flex border-b border-slate-200 bg-white px-5 overflow-x-auto flex-shrink-0">
          <button
            onClick={() => setActiveTab('groups')}
            className={`py-3 px-4 text-xs font-bold border-b-2 flex items-center space-x-2 whitespace-nowrap transition ${
              activeTab === 'groups'
                ? 'border-indigo-600 text-indigo-600'
                : 'border-transparent text-slate-500 hover:text-slate-800'
            }`}
          >
            <Sliders className="w-4 h-4" />
            <span>策略分组矩阵 (Policy Groups)</span>
            {data && (
              <span className="text-2xs px-1.5 py-0.5 rounded-full bg-slate-100 text-slate-600">
                {data.total_groups}
              </span>
            )}
          </button>

          <button
            onClick={() => setActiveTab('verify')}
            className={`py-3 px-4 text-xs font-bold border-b-2 flex items-center space-x-2 whitespace-nowrap transition ${
              activeTab === 'verify'
                ? 'border-indigo-600 text-indigo-600'
                : 'border-transparent text-slate-500 hover:text-slate-800'
            }`}
          >
            <ShieldCheck className="w-4 h-4" />
            <span>双域名与质量验证 (Verify Report)</span>
            {verifyLoading ? (
              <RefreshCw className="w-3 h-3 animate-spin text-indigo-500 ml-1" />
            ) : verifyData ? (
              <span className={`text-2xs px-1.5 py-0.5 rounded-full font-bold ${
                verifyData.all_domains_reachable ? 'bg-emerald-100 text-emerald-800' : 'bg-red-100 text-red-800'
              }`}>
                {verifyData.all_domains_reachable ? '双绿灯' : '连通异常'}
              </span>
            ) : null}
          </button>

          <button
            onClick={() => setActiveTab('rules')}
            className={`py-3 px-4 text-xs font-bold border-b-2 flex items-center space-x-2 whitespace-nowrap transition ${
              activeTab === 'rules'
                ? 'border-indigo-600 text-indigo-600'
                : 'border-transparent text-slate-500 hover:text-slate-800'
            }`}
          >
            <FileCode className="w-4 h-4" />
            <span>分流规则与驱动脚本 (Rules & Script)</span>
            {data && (
              <span className="text-2xs px-1.5 py-0.5 rounded-full bg-purple-100 text-purple-700">
                {data.rules.length} 规则
              </span>
            )}
          </button>
        </div>

        {/* 3. Main Content Area */}
        <div className="flex-1 overflow-y-auto p-5 space-y-4 bg-slate-50/40">
          {loading && (
            <div className="flex flex-col items-center justify-center py-20 text-slate-400 space-y-3">
              <RefreshCw className="w-8 h-8 animate-spin text-indigo-500" />
              <p className="text-xs">正在从内存快照中极速解析订阅拓扑...</p>
            </div>
          )}

          {error && (
            <div className="p-4 rounded-xl bg-rose-50 border border-rose-200 text-rose-800 text-xs">
              <strong>加载出错:</strong> {error}
            </div>
          )}

          {!loading && data && activeTab === 'groups' && (
            <div className="space-y-4">
              {/* Filter & Search Bar */}
              <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 bg-white p-3 rounded-xl border border-slate-200 shadow-2xs">
                <div className="flex items-center space-x-1.5">
                  <span className="text-2xs text-slate-400 font-semibold uppercase">筛选:</span>
                  {[
                    { id: 'all', label: `全部 (${data.groups.length})` },
                    { id: 'self', label: `🛡️ 内部自建 (${data.self_groups})` },
                    { id: 'external', label: `🌐 外部默认 (${data.ext_groups})` },
                    { id: 'app', label: `🎯 应用分流` },
                  ].map(btn => (
                    <button
                      key={btn.id}
                      onClick={() => setGroupFilter(btn.id as any)}
                      className={`px-2.5 py-1 rounded text-xs font-semibold transition ${
                        groupFilter === btn.id
                          ? 'bg-slate-900 text-white'
                          : 'bg-slate-100 hover:bg-slate-200 text-slate-600'
                      }`}
                    >
                      {btn.label}
                    </button>
                  ))}
                </div>

                <div className="relative w-full sm:w-64">
                  <Search className="w-3.5 h-3.5 text-slate-400 absolute left-2.5 top-2.5" />
                  <input
                    type="text"
                    placeholder="搜索策略组、节点名或便签..."
                    value={searchQuery}
                    onChange={e => setSearchQuery(e.target.value)}
                    className="w-full pl-8 pr-3 py-1.5 text-xs bg-slate-50 border border-slate-200 rounded-lg focus:bg-white focus:border-indigo-500"
                  />
                </div>
              </div>

              {/* Group Cards List */}
              <div className="space-y-3">
                {filteredGroups.map(group => {
                  const isExpanded = expandedGroups.has(group.name);
                  const isPickerOpen = pickerGroupId === group.name;

                  return (
                    <div 
                      key={group.name} 
                      className={`border rounded-xl transition duration-200 ${
                        !group.is_external 
                          ? 'border-emerald-200/90 bg-white hover:border-emerald-300' 
                          : 'border-slate-200 bg-white hover:border-slate-300'
                      }`}
                    >
                      {/* Group Card Header */}
                      <div className="p-4 flex flex-col md:flex-row md:items-center justify-between gap-3">
                        <div className="space-y-1.5 flex-1">
                          <div className="flex flex-wrap items-center gap-2">
                            <span className="font-bold text-slate-900 text-sm">
                              {group.display_name}
                            </span>
                            <span className="text-2xs font-mono text-slate-400">
                              ({group.name})
                            </span>

                            {/* Self-hosted vs External Badge */}
                            {!group.is_external ? (
                              <span className="inline-flex items-center px-2 py-0.5 rounded-full text-2xs font-bold bg-emerald-100 text-emerald-800 border border-emerald-200">
                                <ShieldCheck className="w-3 h-3 mr-1 text-emerald-600" />
                                自建·可配置
                              </span>
                            ) : (
                              <span className="inline-flex items-center px-2 py-0.5 rounded-full text-2xs font-medium bg-slate-100 text-slate-600 border border-slate-200">
                                <Lock className="w-3 h-3 mr-1 text-slate-400" />
                                外部默认·只读
                              </span>
                            )}

                            {/* Type Badge */}
                            <span className="px-1.5 py-0.2 rounded text-2xs font-mono uppercase bg-indigo-50 text-indigo-700 border border-indigo-100">
                              {group.type}
                            </span>

                            {/* Matching Rules Count */}
                            {group.rule_count > 0 && (
                              <span className="px-1.5 py-0.2 rounded text-2xs font-mono bg-purple-50 text-purple-700 border border-purple-100">
                                {group.rule_count} 条分流规则
                              </span>
                            )}
                          </div>

                          {/* Dynamic Tag Pills */}
                          <div className="flex flex-wrap items-center gap-1.5">
                            {group.tags.map((t, idx) => (
                              <span 
                                key={idx} 
                                className="px-2 py-0.5 rounded-md text-2xs font-medium bg-slate-100 text-slate-600 border border-slate-200/80 flex items-center space-x-1"
                              >
                                <Tag className="w-2.5 h-2.5 text-slate-400 mr-1" />
                                <span>{t}</span>
                              </span>
                            ))}

                            {/* Add tag button for self-hosted groups */}
                            {!group.is_external && tagInputGroupId !== group.name && (
                              <button
                                onClick={() => {
                                  setTagInputGroupId(group.name);
                                  setNewTagText('');
                                }}
                                className="text-2xs px-1.5 py-0.5 rounded text-indigo-600 hover:bg-indigo-50 font-medium transition"
                              >
                                + 便签
                              </button>
                            )}

                            {/* Inline Tag Input */}
                            {tagInputGroupId === group.name && (
                              <div className="inline-flex items-center space-x-1">
                                <input
                                  type="text"
                                  placeholder="新便签"
                                  value={newTagText}
                                  onChange={e => setNewTagText(e.target.value)}
                                  className="text-2xs px-1.5 py-0.5 border border-indigo-300 rounded focus:ring-1 focus:ring-indigo-500 w-20"
                                />
                                <button
                                  onClick={() => handleAddTag(group)}
                                  className="text-2xs px-1.5 py-0.5 bg-indigo-600 text-white rounded font-bold"
                                >
                                  存
                                </button>
                                <button
                                  onClick={() => setTagInputGroupId(null)}
                                  className="text-2xs px-1 text-slate-400 hover:text-slate-600"
                                >
                                  ✕
                                </button>
                              </div>
                            )}
                          </div>
                        </div>

                        {/* Right Buttons: Foolproof node adding / expand */}
                        <div className="flex items-center space-x-2 self-start md:self-auto">
                          {/* Foolproof Add Nodes (Only for self-hosted groups) */}
                          {!group.is_external && (
                            <button
                              onClick={() => isPickerOpen ? setPickerGroupId(null) : handleOpenPicker(group)}
                              className="px-2.5 py-1.5 bg-emerald-600 hover:bg-emerald-700 text-white rounded-lg text-xs font-semibold shadow-2xs transition flex items-center space-x-1"
                            >
                              <Plus className="w-3.5 h-3.5" />
                              <span>傻瓜式加节点</span>
                            </button>
                          )}

                          <button
                            onClick={() => toggleGroup(group.name)}
                            className="p-1.5 rounded-lg border border-slate-200 hover:bg-slate-100 text-slate-600 text-xs flex items-center space-x-1"
                          >
                            <span className="font-mono text-2xs font-semibold">{group.proxy_count} 节点</span>
                            {isExpanded ? <ChevronUp className="w-3.5 h-3.5" /> : <ChevronDown className="w-3.5 h-3.5" />}
                          </button>
                        </div>
                      </div>

                      {/* Foolproof Node Picker Panel */}
                      {isPickerOpen && (
                        <div className="p-4 bg-emerald-50/50 border-t border-emerald-100 space-y-3">
                          <div className="flex flex-wrap items-center justify-between gap-2">
                            <div>
                              <h4 className="text-xs font-bold text-emerald-950 flex items-center space-x-1.5">
                                <Plus className="w-4 h-4 text-emerald-600" />
                                <span>傻瓜式节点与便签勾选面板</span>
                              </h4>
                              <p className="text-2xs text-emerald-700 mt-0.5">
                                点击常用便签一键全选对应节点，或单独勾选入组
                              </p>
                            </div>

                            {/* Quick Batch Select Pills */}
                            <div className="flex flex-wrap items-center gap-1.5 text-2xs">
                              <span className="font-semibold text-emerald-800">一键便签引入:</span>
                              <button
                                onClick={() => handleBatchSelectByTag('us_resi')}
                                className="px-2 py-0.5 rounded bg-white hover:bg-emerald-100 text-emerald-800 border border-emerald-300 font-medium transition"
                              >
                                🇺🇸 美国家宽
                              </button>
                              <button
                                onClick={() => handleBatchSelectByTag('jp_resi')}
                                className="px-2 py-0.5 rounded bg-white hover:bg-emerald-100 text-emerald-800 border border-emerald-300 font-medium transition"
                              >
                                🇯🇵 日本家宽
                              </button>
                              <button
                                onClick={() => handleBatchSelectByTag('transit')}
                                className="px-2 py-0.5 rounded bg-white hover:bg-emerald-100 text-emerald-800 border border-emerald-300 font-medium transition"
                              >
                                ⚡ 线路骨干
                              </button>
                              <button
                                onClick={() => handleBatchSelectByTag('chain')}
                                className="px-2 py-0.5 rounded bg-white hover:bg-emerald-100 text-emerald-800 border border-emerald-300 font-medium transition"
                              >
                                🔗 链式中继
                              </button>
                            </div>
                          </div>

                          {/* Picker Search & Category Filter */}
                          <div className="flex flex-wrap items-center gap-2">
                            <div className="relative flex-1 min-w-[180px]">
                              <Search className="w-3.5 h-3.5 text-slate-400 absolute left-2.5 top-2" />
                              <input
                                type="text"
                                placeholder="搜索节点名称 / 国家 / 标签..."
                                value={pickerSearch}
                                onChange={e => setPickerSearch(e.target.value)}
                                className="w-full pl-8 pr-2.5 py-1 text-2xs bg-white border border-emerald-200 rounded-md focus:outline-none focus:border-emerald-500"
                              />
                            </div>
                            <select
                              value={pickerTagFilter}
                              onChange={e => setPickerTagFilter(e.target.value)}
                              className="px-2 py-1 text-2xs bg-white border border-emerald-200 rounded-md text-slate-700"
                            >
                              <option value="all">全部节点 ({atoms.length})</option>
                              <option value="transit">仅骨干专线</option>
                              <option value="residential">仅原生家宽</option>
                              <option value="chain">仅链式中继</option>
                              <option value="clean">仅不送中纯净</option>
                            </select>
                          </div>

                          {/* Node Checkboxes List */}
                          <div className="max-h-56 overflow-y-auto bg-white rounded-lg border border-emerald-200/80 p-2 divide-y divide-slate-100 text-xs">
                            {pickerFilteredAtoms.map(atom => {
                              const isChecked = selectedNodeNames.includes(atom.name);
                              return (
                                <label 
                                  key={atom.id || atom.name}
                                  className="flex items-center justify-between p-2 hover:bg-emerald-50/50 rounded cursor-pointer transition"
                                >
                                  <div className="flex items-center space-x-2">
                                    <input
                                      type="checkbox"
                                      checked={isChecked}
                                      onChange={() => handleTogglePickerNode(atom.name)}
                                      className="rounded border-slate-300 text-emerald-600 focus:ring-emerald-500"
                                    />
                                    <span className="font-medium text-slate-800">{atom.name}</span>
                                    <span className="text-2xs font-mono px-1.5 py-0.2 rounded bg-slate-100 text-slate-600">
                                      {atom.kind}
                                    </span>
                                  </div>
                                  <div className="flex items-center space-x-2 text-2xs font-mono text-slate-400">
                                    <span>{atom.country}</span>
                                    <span className={atom.online ? 'text-emerald-600 font-bold' : 'text-slate-400'}>
                                      {atom.online ? `${atom.latency_ms}ms` : '离线'}
                                    </span>
                                  </div>
                                </label>
                              );
                            })}
                          </div>

                          {/* Save / Cancel Bar */}
                          <div className="flex items-center justify-between pt-1">
                            <span className="text-2xs text-emerald-800 font-medium font-mono">
                              已选中: <strong>{selectedNodeNames.length}</strong> 个节点
                            </span>
                            <div className="flex items-center space-x-2">
                              <button
                                onClick={() => setPickerGroupId(null)}
                                className="px-3 py-1.5 rounded-lg border border-slate-300 text-slate-600 text-xs hover:bg-slate-100"
                              >
                                取消
                              </button>
                              <button
                                onClick={() => handleSavePickerNodes(group)}
                                disabled={savingGroup}
                                className="px-3 py-1.5 rounded-lg bg-emerald-600 hover:bg-emerald-700 text-white font-semibold text-xs shadow-2xs transition disabled:opacity-50"
                              >
                                {savingGroup ? '正在保存...' : '应用并异步更新'}
                              </button>
                            </div>
                          </div>
                        </div>
                      )}

                      {/* Expanded Member Nodes View */}
                      {isExpanded && !isPickerOpen && (
                        <div className="p-4 bg-slate-50/70 border-t border-slate-200/80 space-y-2">
                          <div className="text-2xs font-semibold text-slate-500 uppercase tracking-wider flex items-center justify-between">
                            <span>包含的代理节点列表 ({group.proxies.length})</span>
                            <span className="font-normal lowercase">绿色为自建服务</span>
                          </div>

                          <div className="grid grid-cols-1 md:grid-cols-2 gap-2 text-xs">
                            {group.proxies.map((p, idx) => (
                              <div 
                                key={idx} 
                                className="p-2.5 bg-white border border-slate-200/80 rounded-lg flex items-center justify-between shadow-2xs"
                              >
                                <div className="space-y-0.5 truncate pr-2">
                                  <div className="flex items-center space-x-1.5 truncate">
                                    <span className={`w-2 h-2 rounded-full ${p.online !== false ? 'bg-emerald-500' : 'bg-slate-300'}`} />
                                    <span className="font-medium text-slate-900 truncate" title={p.name}>
                                      {p.name}
                                    </span>
                                  </div>
                                  <div className="flex items-center space-x-1.5 text-2xs text-slate-500 font-mono">
                                    {p.is_self_hosted ? (
                                      <span className="text-emerald-700 font-bold bg-emerald-50 px-1 rounded">自建</span>
                                    ) : (
                                      <span className="text-slate-500 bg-slate-100 px-1 rounded">外部</span>
                                    )}
                                    <span>{p.country || '通用'}</span>
                                    {p.latency_ms > 0 && <span>{p.latency_ms}ms</span>}
                                  </div>
                                </div>

                                {/* Remove button for self-hosted groups */}
                                {!group.is_external && (
                                  <ConfirmActionButton
                                    onConfirm={() => handleRemoveNode(group, p.name)}
                                    confirmLabel="移除?"
                                    title="从分组移除"
                                    className="p-1 rounded text-slate-300 hover:text-rose-600 hover:bg-rose-50 transition"
                                  >
                                    <Trash2 className="w-3.5 h-3.5" />
                                  </ConfirmActionButton>
                                )}
                              </div>
                            ))}
                          </div>
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            </div>
          )}

          {/* Tab 2: Dual Domain & Quality Verify Report */}
          {!loading && activeTab === 'verify' && (
            <div className="space-y-5">
              {!verifyData ? (
                <div className="p-8 bg-white border border-slate-200 rounded-xl text-center space-y-2">
                  <ShieldCheck className="w-8 h-8 text-slate-400 mx-auto animate-pulse" />
                  <p className="text-xs font-semibold text-slate-700">正在生成双域名全量检验报告...</p>
                  <p className="text-2xs text-slate-400">系统正对 free.1992004.xyz 与 sub.ashanzzz.com 进行本地 ping 与连通性深度审计。</p>
                </div>
              ) : (
                <>
                  {/* Card 1: Dual Domain Reachability & Anti-GFW Test */}
                  <div className="bg-white border border-slate-200 rounded-xl p-5 shadow-2xs space-y-3">
                    <div className="flex items-center justify-between border-b border-slate-100 pb-3">
                      <div>
                        <h3 className="text-xs font-bold text-slate-900 uppercase tracking-wider flex items-center space-x-1.5">
                          <Globe className="w-4 h-4 text-blue-600" />
                          <span>生产双域名连通性与防墙测试 (Dual-Domain Live Probe)</span>
                        </h3>
                        <p className="text-2xs text-slate-500 mt-0.5">
                          从本地沙箱分别对两大生产分发域名发起真实 HTTP 请求并测算 RTT 延迟与防墙拦截率
                        </p>
                      </div>
                      <span className={`text-2xs font-mono font-bold px-2 py-0.5 rounded ${
                        verifyData.all_domains_reachable 
                          ? 'bg-emerald-100 text-emerald-800' 
                          : 'bg-red-100 text-red-800'
                      }`}>
                        {verifyData.all_domains_reachable ? '🟢 全部域名正常连通' : '🔴 存在不可达域名'}
                      </span>
                    </div>

                    <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                      {(verifyData.domains_check || []).map((d) => (
                        <div
                          key={d.domain}
                          className={`p-4 rounded-xl border space-y-2.5 transition ${
                            d.reachable 
                              ? 'bg-slate-50/80 border-slate-200' 
                              : 'bg-red-50/80 border-red-300'
                          }`}
                        >
                          <div className="flex items-center justify-between">
                            <span className="font-bold text-xs text-slate-900 font-mono">{d.domain}</span>
                            <span className={`px-2 py-0.5 rounded text-2xs font-mono font-bold ${
                              d.reachable ? 'bg-emerald-100 text-emerald-800' : 'bg-red-100 text-red-800'
                            }`}>
                              {d.reachable ? `HTTP ${d.status_code || 200}` : '不可达'}
                            </span>
                          </div>

                          <div className="flex items-center justify-between text-2xs font-mono text-slate-600">
                            <span>测得时延: <b className="text-slate-900">{d.latency_ms > 0 ? `${d.latency_ms} ms` : '—'}</b></span>
                            <span className={`font-semibold ${d.possible_gfw ? 'text-red-600' : 'text-emerald-600'}`}>
                              {d.possible_gfw ? '⚠️ 疑似拦截' : '🛡️ 纯净放行'}
                            </span>
                          </div>

                          <div className="pt-2 border-t border-slate-200/60 flex items-center justify-between text-2xs">
                            <span className="text-slate-400 font-mono truncate max-w-xs">{d.url}</span>
                            <a
                              href={d.url}
                              target="_blank"
                              rel="noreferrer"
                              className="text-blue-600 hover:text-blue-800 font-semibold flex items-center flex-shrink-0 ml-2"
                            >
                              <span>验证</span>
                              <ExternalLink className="w-3 h-3 ml-0.5" />
                            </a>
                          </div>
                        </div>
                      ))}
                    </div>
                  </div>

                  {/* Card 2: Correctness Proof Banner */}
                  <div className="p-4 rounded-xl border bg-gradient-to-r from-blue-50/60 to-purple-50/60 border-indigo-200 shadow-2xs space-y-1.5">
                    <div className="flex items-center space-x-2">
                      <ShieldCheck className="w-4 h-4 text-indigo-600" />
                      <h4 className="text-xs font-bold text-indigo-950">
                        三轨标准合成架构正确性证明 (Architectural Synthesis Proof)
                      </h4>
                    </div>
                    <p className="text-2xs text-indigo-800">
                      {verifyData.summary || '订阅拓扑校验完成'}
                    </p>
                    <div className="flex flex-wrap items-center gap-3 text-2xs font-mono pt-1 text-slate-600">
                      <span>总代理: <b>{verifyData.total_proxies ?? 0}</b></span>
                      <span>骨干在线: <b>{verifyData.self_nodes_online ?? 0}</b></span>
                      <span>家宽中继正常: <b>{verifyData.self_nodes_residential_chained ?? 0}</b></span>
                      <span className={`font-bold ${verifyData.composite_valid ? 'text-emerald-700' : 'text-amber-700'}`}>
                        {currentSub.toLowerCase() === 'q'
                          ? '【Q 规范：0 住宅硬隔离】'
                          : currentSub.toLowerCase() === 'business'
                          ? '【business 规范：纯自建零商业机场】'
                          : '【ashan 规范：商业+全量自建】'}
                      </span>
                    </div>
                  </div>

                  {/* Card 3: Group Proxy Distribution & Dangling References Check */}
                  {verifyData.group_counts && (
                    <div className="bg-white border border-slate-200 rounded-xl p-5 shadow-2xs space-y-3">
                      <div className="flex items-center justify-between border-b border-slate-100 pb-2">
                        <div>
                          <h4 className="text-xs font-bold text-slate-900 uppercase tracking-wider">
                            策略组引用完整性与连接分布 (Policy Groups Reference Audit)
                          </h4>
                          <p className="text-2xs text-slate-500 mt-0.5">
                            严查策略组空引用与悬空引用 (Dangling References)
                          </p>
                        </div>
                        <span className="text-2xs font-mono font-bold px-2 py-0.5 rounded bg-blue-100 text-blue-800">
                          {Object.keys(verifyData.group_counts || {}).length} 个策略组已注册
                        </span>
                      </div>

                      {/* Dangling References Warning if any */}
                      {verifyData.dangling_references && Object.keys(verifyData.dangling_references).length > 0 && (
                        <div className="p-3 bg-amber-50 border border-amber-200 rounded-lg text-2xs text-amber-900 space-y-1">
                          <div className="font-bold flex items-center">
                            <AlertTriangle className="w-3.5 h-3.5 mr-1 text-amber-600" />
                            <span>发现潜在空引用或非活跃依赖:</span>
                          </div>
                          {Object.entries(verifyData.dangling_references).map(([grp, refs]) => (
                            <div key={grp} className="font-mono">
                              组 [{grp}]: {refs.join(', ')}
                            </div>
                          ))}
                        </div>
                      )}

                      {/* Group Counts Grid */}
                      <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 gap-2 font-mono text-2xs max-h-56 overflow-y-auto">
                        {Object.entries(verifyData.group_counts || {}).map(([grpName, count]) => (
                          <div key={grpName} className="p-2 bg-slate-50 border border-slate-200 rounded-lg flex items-center justify-between">
                            <span className="truncate pr-1 font-medium text-slate-700" title={grpName}>{grpName}</span>
                            <span className="px-1.5 py-0.2 rounded bg-indigo-50 text-indigo-700 font-bold flex-shrink-0">
                              {count}
                            </span>
                          </div>
                        ))}
                      </div>
                    </div>
                  )}

                  {/* Card 4: Self Nodes Health in this Subscription */}
                  <div className="bg-white border border-slate-200 rounded-xl p-5 shadow-2xs space-y-3">
                    <div className="flex items-center justify-between border-b border-slate-100 pb-2">
                      <div>
                        <h4 className="text-xs font-bold text-slate-900 uppercase tracking-wider">
                          自建节点健康度与送中状态 (Self-Hosted Nodes Status)
                        </h4>
                        <p className="text-2xs text-slate-500 mt-0.5">
                          仅针对自建连接进行延迟与送中透视（外部商业连接不检测）
                        </p>
                      </div>
                      <span className="text-2xs font-mono text-slate-500">
                        可用: <b>{(verifyData.self_nodes_online || 0) + (verifyData.self_nodes_residential_chained || 0)}</b> / {verifyData.self_nodes_count || 0}
                      </span>
                    </div>

                    <div className="divide-y divide-slate-100 border border-slate-200 rounded-xl overflow-hidden font-mono text-2xs">
                      {(verifyData.self_nodes || []).map((node, i) => (
                        <div key={i} className="p-3 bg-white hover:bg-slate-50 transition flex items-center justify-between">
                          <div className="flex items-center space-x-2 truncate pr-2">
                            <span className={`w-2 h-2 rounded-full ${node.online ? 'bg-emerald-500' : 'bg-amber-500'}`} />
                            <span className="font-bold text-slate-900 truncate">{node.name}</span>
                            {node.node_type && (
                              <span className="px-1 py-0.2 rounded bg-slate-100 text-slate-600 text-3xs">{node.node_type}</span>
                            )}
                          </div>
                          <div className="flex items-center space-x-3 flex-shrink-0">
                            <span className="text-slate-600">
                              RTT: <b className="text-slate-900">{node.latency_ms > 0 ? `${node.latency_ms} ms` : '—'}</b>
                            </span>
                            {!node.china_exit ? (
                              <span className="px-1.5 py-0.5 rounded bg-emerald-50 text-emerald-700 border border-emerald-200">
                                纯净 NCR
                              </span>
                            ) : (
                              <span className="px-1.5 py-0.5 rounded bg-amber-50 text-amber-700 border border-amber-200">
                                送中
                              </span>
                            )}
                          </div>
                        </div>
                      ))}
                    </div>
                  </div>
                </>
              )}
            </div>
          )}

          {/* Tab 3: Rules & Script Operator */}
          {!loading && data && activeTab === 'rules' && (
            <div className="space-y-5">
              {/* Card 1: Routing Flow Overview */}
              <div className="bg-white border border-slate-200 rounded-xl p-5 shadow-2xs space-y-3">
                <div className="flex items-center justify-between border-b border-slate-100 pb-3">
                  <div>
                    <h3 className="text-xs font-bold text-slate-900 uppercase tracking-wider flex items-center space-x-1.5">
                      <Zap className="w-4 h-4 text-amber-500" />
                      <span>业务分流流向概览 (Business Traffic Routing)</span>
                    </h3>
                    <p className="text-2xs text-slate-500 mt-0.5">
                      客户端请求通过规则匹配后路由至对应的代理策略组
                    </p>
                  </div>
                  <span className="text-2xs font-mono font-bold px-2 py-0.5 rounded bg-purple-100 text-purple-800">
                    {data.rules.length} 条生效规则
                  </span>
                </div>

                <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-4 gap-3 text-xs">
                  <div className="p-3 bg-purple-50/50 border border-purple-200 rounded-xl space-y-1">
                    <span className="text-2xs font-bold text-purple-800">🤖 AI 服务分流</span>
                    <div className="text-xs font-medium text-slate-800">ChatGPT / Claude</div>
                    <div className="text-2xs text-purple-600 font-mono mt-1">➔ 自建-美国家宽</div>
                  </div>

                  <div className="p-3 bg-rose-50/50 border border-rose-200 rounded-xl space-y-1">
                    <span className="text-2xs font-bold text-rose-800">🎬 国际流媒体</span>
                    <div className="text-xs font-medium text-slate-800">YouTube / Netflix</div>
                    <div className="text-2xs text-rose-600 font-mono mt-1">➔ 自建-日本家宽</div>
                  </div>

                  <div className="p-3 bg-blue-50/50 border border-blue-200 rounded-xl space-y-1">
                    <span className="text-2xs font-bold text-blue-800">⚡ 纯骨干专线</span>
                    <div className="text-xs font-medium text-slate-800">大流量 / 游戏下载</div>
                    <div className="text-2xs text-blue-600 font-mono mt-1">➔ 自建-线路机</div>
                  </div>

                  <div className="p-3 bg-slate-100 border border-slate-200 rounded-xl space-y-1">
                    <span className="text-2xs font-bold text-slate-700">🌐 兜底分流</span>
                    <div className="text-xs font-medium text-slate-800">MATCH (未命中规则)</div>
                    <div className="text-2xs text-slate-500 font-mono mt-1">➔ 选择代理 / 漏网之鱼</div>
                  </div>
                </div>
              </div>

              {/* Card 2: SubStore JavaScript Script Operator Source Code */}
              <div className="bg-slate-900 border border-slate-800 rounded-xl p-5 shadow-lg text-white space-y-3">
                <div className="flex items-center justify-between border-b border-slate-800 pb-3">
                  <div className="flex items-center space-x-2">
                    <FileCode className="w-4 h-4 text-emerald-400" />
                    <div>
                      <h3 className="text-xs font-bold text-white uppercase tracking-wider font-mono">
                        SubStore 动态驱动算子脚本 (Script Operator)
                      </h3>
                      <p className="text-2xs text-slate-400 mt-0.5">
                        在 Tokyo VPS SubStore 上执行，负责别名映射 (R2)、自建策略组编译与双轨标准硬隔离 (R3)
                      </p>
                    </div>
                  </div>

                  <button
                    onClick={() => handleCopyScript(data.routing_script || '// 无脚本')}
                    className="px-2.5 py-1 rounded bg-slate-800 hover:bg-slate-700 text-xs font-mono font-medium transition flex items-center space-x-1.5 border border-slate-700"
                  >
                    {copiedScript ? <Check className="w-3.5 h-3.5 text-emerald-400" /> : <Copy className="w-3.5 h-3.5 text-slate-300" />}
                    <span>{copiedScript ? '已复制脚本' : '复制代码'}</span>
                  </button>
                </div>

                <div className="relative">
                  <pre className="p-4 bg-slate-950/80 rounded-lg font-mono text-2xs text-emerald-300 overflow-x-auto max-h-72 border border-slate-800/80 leading-relaxed select-all">
                    {data.routing_script || '// 当前订阅尚未绑定自定义驱动脚本'}
                  </pre>
                </div>
              </div>

              {/* Card 3: Clash Rules Detail */}
              <div className="bg-white border border-slate-200 rounded-xl p-5 shadow-2xs space-y-3">
                <div className="flex items-center justify-between border-b border-slate-100 pb-3">
                  <div>
                    <h3 className="text-xs font-bold text-slate-900 uppercase tracking-wider">
                      Clash 分流规则明细清单 (Top Rules)
                    </h3>
                    <p className="text-2xs text-slate-500 mt-0.5">
                      展示前 50 条核心路由分流规则匹配流向
                    </p>
                  </div>
                  <span className="text-2xs font-mono text-slate-400">
                    共 {data.rules.length} 条
                  </span>
                </div>

                <div className="max-h-80 overflow-y-auto divide-y divide-slate-100 border border-slate-200 rounded-lg font-mono text-2xs">
                  {data.rules.slice(0, 50).map((rule, idx) => (
                    <div key={idx} className="p-2.5 flex items-center justify-between hover:bg-slate-50 transition">
                      <div className="flex items-center space-x-2 truncate pr-2">
                        <span className="px-1.5 py-0.5 rounded bg-blue-50 text-blue-700 font-bold">
                          {rule.type}
                        </span>
                        <span className="text-slate-800 font-medium truncate" title={rule.payload}>
                          {rule.payload || 'ALL'}
                        </span>
                      </div>
                      <div className="flex items-center space-x-1.5 flex-shrink-0 text-slate-500">
                        <ArrowRight className="w-3 h-3 text-slate-400" />
                        <span className="px-2 py-0.5 rounded bg-slate-100 text-slate-900 font-bold">
                          {rule.target}
                        </span>
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
};
