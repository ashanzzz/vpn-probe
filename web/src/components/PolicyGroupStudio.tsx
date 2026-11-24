import React, { useCallback, useEffect, useState, useMemo } from 'react';
import { 
  Plus, 
  Pencil, 
  Trash2, 
  Save, 
  RefreshCw, 
  CheckCircle2, 
  AlertTriangle, 
  RotateCcw,
  Layers,
  ShieldCheck,
  Check,
  Zap,
  Globe,
  Sliders,
  Radio,
  ExternalLink,
  ChevronDown,
  ChevronUp,
  Info
} from 'lucide-react';
import { api } from '../api/client';
import type { CustomGroupDef, GroupFilter, AtomNode, PublishJob } from '../types';

const blank = (): CustomGroupDef => ({
  name: '', 
  display_name: '', 
  enabled: true, 
  type: 'url-test', 
  order: 100,
  add_to_entry: true, 
  filter: { mode: 'filter', countries: [], kinds: ['transit', 'residential', 'chain'], clean_only: true },
});

const options = ['US', 'JP', 'TW', 'HK', 'SG', 'KR', 'UK', 'DE', 'OTHER'];
const infraOptions = [
  ['nat_resi', 'NAT 家宽'], ['vps_resi', 'VPS 家宽'],
  ['nat_line', 'NAT 线路'], ['vps_line', 'VPS 线路'], ['commercial', '商业节点'],
];
const inputClass = 'w-full border border-slate-300 rounded-lg px-2.5 py-1.5 text-xs bg-white text-slate-800 focus:outline-none focus:ring-1 focus:ring-emerald-500';
const split = (s: string) => s.split(',').map(x => x.trim()).filter(Boolean);
const asText = (s?: string[]) => (s || []).join(', ');

// 8 大内置核心基准组的标准名称清单
const BASELINE_GROUP_NAMES = new Set([
  '自建-线路机',
  '自建-美国和美国家宽',
  '自建-日本和日本家宽',
  '自建-台湾家宽分组',
  '自建-其他',
  '自建-家宽分组',
  '自建-不送中',
  '不送中',
  '自建-送中',
]);

// 外部应用策略组联动注入关系字典
const EXTERNAL_APP_INJECTIONS = [
  {
    app: 'AI服务 (OpenAI / Claude / Copilot)',
    preferred: ['自建-美国和美国家宽', '自建-家宽分组', '自建-线路机'],
    desc: '高防风控业务：优先调度美国家宽纯净出口，保障 ChatGPT 4o / Claude 3.7 解锁',
  },
  {
    app: 'TikTok (国际版抖音)',
    preferred: ['自建-美国和美国家宽', '自建-日本和日本家宽', '自建-台湾家宽分组'],
    desc: '强风控业务：按地区智能选用日/美/台原生家宽，避免被标记或封锁',
  },
  {
    app: 'YouTube / 谷歌服务',
    preferred: ['自建-不送中', '自建-线路机'],
    desc: '防送中直通：强行排除 Google-HK 送中节点，锁定美日原生 YouTube Premium',
  },
  {
    app: 'Netflix / Disney+ / HBO',
    preferred: ['自建-美国和美国家宽', '自建-日本和日本家宽', '自建-台湾家宽分组', '自建-家宽分组'],
    desc: '流媒体解锁：优先全量日美原生住宅家宽，享受最高清无拦截串流',
  },
  {
    app: '巴哈姆特 (动画疯)',
    preferred: ['自建-台湾家宽分组'],
    desc: '台区专享：仅限自建台湾中华电信 HiNet 家宽直连或专线链式',
  },
  {
    app: 'Spotify / 音乐串流',
    preferred: ['自建-美国和美国家宽', '自建-日本和日本家宽', '自建-台湾家宽分组'],
    desc: '跨区聆听：纯净原生 IP 锁定，避免频繁弹登录风控',
  },
];

export const PolicyGroupStudio: React.FC = () => {
  const [groups, setGroups] = useState<CustomGroupDef[]>([]);
  const [atoms, setAtoms] = useState<AtomNode[]>([]);
  const [form, setForm] = useState<CustomGroupDef | null>(null);
  const [preview, setPreview] = useState<{ count: number; nodes: {id: string;name: string}[] } | null>(null);
  const [job, setJob] = useState<PublishJob | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState<{ type: 'success' | 'info' | 'error'; text: string } | null>(null);
  const [isDirty, setIsDirty] = useState(false);

  // 视角切换：ashan (全栈全家桶) vs Q (轻量骨干)
  const [viewPerspective, setViewPerspective] = useState<'ashan' | 'q'>('ashan');

  // 是否展开外置策略联动卡片
  const [showExtMatrix, setShowExtMatrix] = useState(false);

  // 节点元数据打标状态
  const [metadataNode, setMetadataNode] = useState<AtomNode | null>(null);
  const [metadataType, setMetadataType] = useState('');
  const [metadataTags, setMetadataTags] = useState('');

  const load = useCallback(async () => {
    try {
      const [g, a, j] = await Promise.all([
        api.getCustomGroups(),
        api.getAtoms({}).catch(() => ({ atoms: [], count: 0, status: '' })),
        api.latestPublishJob().catch(() => null),
      ]);
      setGroups(g.groups || []);
      setAtoms(a.atoms || []);
      setIsDirty(false);
      if (j && j.id) setJob(j);
    } catch (e: any) { 
      setError(e.message || '读取失败'); 
    }
  }, []);

  useEffect(() => { load(); }, [load]);

  // 轮询发布任务
  useEffect(() => {
    if (!job?.id || ['succeeded', 'failed', 'superseded'].includes(job.status)) return;
    const timer = window.setInterval(() => {
      api.getPublishJob(job.id).then(setJob).catch((e: Error) => setError(e.message));
    }, 1500);
    return () => window.clearInterval(timer);
  }, [job?.id, job?.status]);

  const patchFilter = (changes: Partial<GroupFilter>) => 
    setForm(old => old ? { ...old, filter: { ...old.filter, ...changes } } : null);

  const toggle = (field: 'countries' | 'kinds' | 'infra_types', v: string) => {
    if (!form) return;
    const values = (form.filter[field] || []) as string[];
    patchFilter({ [field]: values.includes(v) ? values.filter(x => x !== v) : [...values, v] });
  };

  const edit = (g?: CustomGroupDef) => { 
    setForm(g ? JSON.parse(JSON.stringify(g)) : blank()); 
    setPreview(null); 
    setError(''); 
  };

  // 单个策略组提交
  const submitSingle = async (g: CustomGroupDef) => {
    if (!g.name || !g.display_name) { setError('请填写英文 ID 和显示名称'); return; }
    setBusy(true); setError('');
    try {
      const existing = groups.some(x => x.name === g.name);
      const res = existing ? await api.updatePolicyGroup(g.name, g) : await api.createPolicyGroup(g);
      setJob({ id: res.job_id, revision: res.revision, status: 'queued' });
      setNotice({ type: 'success', text: `策略组「${g.display_name}」保存成功，任务 #${res.revision} 已排队同步！` });
      setForm(null); setPreview(null); await load();
    } catch (e: any) { setError(e.message || '保存失败'); }
    finally { setBusy(false); }
  };

  // 一键恢复官方 8 大核心基准组 (url-test 默认自动测速优选)
  const handleResetDefaults = async () => {
    if (!window.confirm('确定将策略列表重置为官方 8 大核心基准组（默认自动测速 url-test）？\n重置后您可以在此基础上任意增删节点、修改策略类型或添加个性化扩展，满意后再点击「保存全量策略并同步」生效。')) {
      return;
    }
    setBusy(true);
    setError('');
    try {
      const res = await api.getPolicyGroupDefaults();
      setGroups(res.groups || []);
      setIsDirty(true);
      setNotice({
        type: 'info',
        text: '已将暂存列表重置为 8 大核心基准组（默认全部配置为 url-test 自动测速优选）。您可以自由编辑、增删任意组，检查无误后点击「保存全量策略并同步」生效。'
      });
    } catch (e: any) {
      setError(`获取基准模板失败: ${e.message}`);
    } finally {
      setBusy(false);
    }
  };

  // 批量保存并同步所有策略组
  const handleSaveAll = async () => {
    if (groups.length === 0) {
      if (!window.confirm('当前没有任何策略组，确认保存空策略？')) return;
    }
    setBusy(true);
    setError('');
    try {
      const res = await api.batchSavePolicyGroups(groups);
      setJob({ id: res.job_id, revision: res.revision, status: 'queued' });
      setIsDirty(false);
      setNotice({
        type: 'success',
        text: `全量策略组已保存！发布任务 #${res.revision} 已排队编译同步至 Sub-Store。`
      });
      await load();
    } catch (e: any) {
      setError(e.message || '全量保存失败');
    } finally {
      setBusy(false);
    }
  };

  const remove = async (g: CustomGroupDef) => {
    if (!window.confirm(`确定删除“${g.display_name}”？`)) return;
    if (isDirty) {
      // 暂存模式直接在本地列表移除
      setGroups(groups.filter(x => x.name !== g.name));
      return;
    }
    setBusy(true); setError('');
    try {
      const res = await api.removePolicyGroup(g.name);
      setJob({ id: res.job_id, revision: res.revision, status: 'queued' });
      await load();
    } catch (e: any) { setError(e.message || '删除失败'); }
    finally { setBusy(false); }
  };

  const doPreview = async () => {
    if (!form) return;
    setBusy(true); setError('');
    try { setPreview(await api.previewPolicyGroup(form)); }
    catch (e: any) { setError(e.message || '预览失败'); }
    finally { setBusy(false); }
  };

  const saveMeta = async () => {
    if (!metadataNode) return;
    setBusy(true);
    try {
      await api.saveNodeMetadata(metadataNode.id, { infra_type: metadataType, tags: split(metadataTags) });
      setMetadataNode(null); await load();
    } catch (e: any) { setError(e.message || '保存节点属性失败'); }
    finally { setBusy(false); }
  };

  // 评估当前组在指定视角（Q 极速骨干 vs ashan 全家桶）下的命中节点统计
  const evaluateCount = useCallback((g: CustomGroupDef, isQ: boolean) => {
    const filter = { ...g.filter };
    let matched = atoms.filter(a => {
      // 种类筛选
      if (filter.kinds && filter.kinds.length > 0 && !filter.kinds.includes(a.kind)) return false;
      // 地区筛选
      if (filter.countries && filter.countries.length > 0) {
        if (!a.country || !filter.countries.includes(a.country)) return false;
      }
      // 送中/防送中
      const isLeaked = !!(a.china_leak || a.china_exit);
      if ((filter.clean_only || filter.exclude_china_leak) && isLeaked) return false;
      if ((filter.china_leak_only || filter.leak_only) && !isLeaked) return false;
      return true;
    });

    if (isQ) {
      // Q 订阅视角：严格剔除住宅与链式
      const backboneOnly = matched.filter(a => a.kind === 'transit');
      return {
        count: backboneOnly.length,
        isIsolated: matched.length > 0 && backboneOnly.length === 0,
      };
    }

    return {
      count: matched.length,
      isIsolated: false,
    };
  }, [atoms]);

  return (
    <section className="bg-white border border-slate-200 rounded-2xl p-5 space-y-5">
      {/* 1. Header Bar with Actions & Perspective Switcher */}
      <div className="flex flex-col md:flex-row md:items-center justify-between gap-4 pb-4 border-b border-slate-100">
        <div>
          <div className="flex items-center space-x-2">
            <Sliders className="w-5 h-5 text-emerald-600" />
            <h3 className="text-base font-bold text-slate-900">
              策略组与标签编排工坊 (Policy Group & Tag Studio)
            </h3>
          </div>
          <p className="text-xs text-slate-500 mt-1">
            统一编排 8 大核心基准组与个性化扩展组 · 默认自动测速优选 (url-test) · 支持全量自定义与一键恢复默认
          </p>
        </div>

        {/* Global Action Buttons */}
        <div className="flex flex-wrap items-center gap-2">
          <button
            onClick={handleResetDefaults}
            disabled={busy}
            className="text-xs font-semibold px-3 py-2 bg-slate-100 hover:bg-slate-200 text-slate-700 rounded-lg flex items-center gap-1.5 transition border border-slate-200 shadow-2xs"
            title="一键将策略列表重置为官方 8 大基准组标准规范（默认自动测速 url-test）"
          >
            <RotateCcw size={14} className="text-slate-600" />
            <span>恢复默认核心基准组</span>
          </button>

          <button
            onClick={() => edit()}
            disabled={busy}
            className="text-xs font-semibold px-3 py-2 bg-emerald-600 hover:bg-emerald-700 text-white rounded-lg flex items-center gap-1.5 shadow-2xs transition"
          >
            <Plus size={14} />
            <span>新建扩展策略组</span>
          </button>

          {isDirty && (
            <button
              onClick={handleSaveAll}
              disabled={busy}
              className="text-xs font-bold px-3.5 py-2 bg-indigo-600 hover:bg-indigo-700 text-white rounded-lg flex items-center gap-1.5 shadow-xs transition animate-pulse"
              title="提交并发布暂存列表中的全量策略组至 Sub-Store"
            >
              <Save size={14} />
              <span>保存全量策略并同步</span>
            </button>
          )}

          <button
            onClick={load}
            disabled={busy}
            className="text-xs border border-slate-200 rounded-lg p-2 text-slate-600 hover:bg-slate-50 transition"
            title="重新从后端拉取"
          >
            <RefreshCw size={14} className={busy ? 'animate-spin' : ''} />
          </button>
        </div>
      </div>

      {/* 2. Target Perspective Switcher Pill (Q vs ashan) */}
      <div className="flex flex-wrap items-center justify-between gap-3 p-3 bg-slate-50 rounded-xl border border-slate-200">
        <div className="flex items-center space-x-2">
          <span className="text-2xs font-semibold uppercase tracking-wider text-slate-400">
            双轨呈现视角 (Preview Perspective):
          </span>
          <div className="flex bg-white p-0.5 rounded-lg border border-slate-200 text-xs font-medium">
            <button
              onClick={() => setViewPerspective('ashan')}
              className={`px-3 py-1 rounded-md transition flex items-center space-x-1.5 ${
                viewPerspective === 'ashan'
                  ? 'bg-purple-600 text-white font-bold shadow-2xs'
                  : 'text-slate-600 hover:text-slate-900'
              }`}
            >
              <span>ashan 全栈旗舰</span>
              <span className="text-2xs opacity-80 font-mono">(全量家宽+骨干)</span>
            </button>
            <button
              onClick={() => setViewPerspective('q')}
              className={`px-3 py-1 rounded-md transition flex items-center space-x-1.5 ${
                viewPerspective === 'q'
                  ? 'bg-blue-600 text-white font-bold shadow-2xs'
                  : 'text-slate-600 hover:text-slate-900'
              }`}
            >
              <span>Q 极速轻量骨干</span>
              <span className="text-2xs opacity-80 font-mono">(0 住宅硬隔离)</span>
            </button>
          </div>
        </div>

        <button
          onClick={() => setShowExtMatrix(!showExtMatrix)}
          className="text-xs text-indigo-600 hover:text-indigo-800 font-semibold flex items-center space-x-1"
        >
          <span>查看外置应用分流注入规则</span>
          {showExtMatrix ? <ChevronUp size={14} /> : <ChevronDown size={14} />}
        </button>
      </div>

      {/* Notice Banner */}
      {notice && (
        <div className={`p-3 rounded-lg text-xs flex items-center justify-between border ${
          notice.type === 'success' ? 'bg-emerald-50 text-emerald-800 border-emerald-200' :
          notice.type === 'info' ? 'bg-blue-50 text-blue-800 border-blue-200' :
          'bg-rose-50 text-rose-800 border-rose-200'
        }`}>
          <span>{notice.text}</span>
          <button onClick={() => setNotice(null)} className="underline text-xs ml-2">关闭</button>
        </div>
      )}

      {/* Error alert */}
      {error && <p role="alert" className="text-xs text-rose-700 bg-rose-50 rounded-lg p-3 border border-rose-200">{error}</p>}

      {/* Job status pill */}
      {job && (
        <div className={`text-xs rounded-lg px-3 py-2 flex items-start gap-2 border ${
          job.status === 'failed' ? 'bg-rose-50 text-rose-800 border-rose-200' : 
          job.status === 'succeeded' ? 'bg-emerald-50 text-emerald-800 border-emerald-200' :
          'bg-slate-50 text-slate-700 border-slate-200'
        }`}>
          {job.status === 'succeeded' ? <CheckCircle2 size={15} className="text-emerald-600 flex-shrink-0 mt-0.5" /> : 
           <AlertTriangle size={15} className="text-amber-500 flex-shrink-0 mt-0.5" />}
          <div className="flex-1">
            发布任务 #{job.revision}：
            {job.status === 'queued' ? '排队中' : 
             job.status === 'running' ? '正在编译脚本并同步至 Sub-Store...' : 
             job.status === 'verifying' ? '验证最终 Clash YAML 拓扑中...' : 
             job.status === 'succeeded' ? 'Sub-Store 发布校验成功！策略组与分流已热生效。' : 
             job.status === 'failed' ? '发布失败' : job.status}
            {job.error && <p className="mt-1 break-all text-2xs font-mono">{job.error}</p>}
          </div>
          {job.status === 'failed' && (
            <button className="underline text-rose-700 font-semibold" onClick={async () => { const j = await api.retryPublishJob(job.id); setJob({ ...j, id: j.job_id }); }}>
              重试
            </button>
          )}
        </div>
      )}

      {/* 3. External App Policy Injection Matrix (Collapsible) */}
      {showExtMatrix && (
        <div className="bg-gradient-to-r from-slate-50 to-indigo-50/40 border border-indigo-200 rounded-xl p-4 space-y-3">
          <div className="flex items-center justify-between">
            <div className="flex items-center space-x-2">
              <Zap className="w-4 h-4 text-indigo-600" />
              <h4 className="text-xs font-bold text-slate-900">
                外置应用策略组与自建策略组注入联动矩阵 (External Application Routing Matrix)
              </h4>
            </div>
            <span className="text-2xs font-mono px-2 py-0.5 rounded bg-indigo-100 text-indigo-800 font-semibold">
              东京 SubStore 编译层无感挂载
            </span>
          </div>
          <p className="text-2xs text-slate-500">
            当下方自建策略组启用时，系统将在最终生成的 Clash 规则中，自动把对应自建策略组挂载到外部各专有应用组顶部，无需您手动重配。
          </p>

          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-2.5 pt-1">
            {EXTERNAL_APP_INJECTIONS.map(item => (
              <div key={item.app} className="bg-white border border-slate-200 rounded-lg p-2.5 space-y-1.5 shadow-2xs">
                <div className="font-bold text-xs text-slate-800">{item.app}</div>
                <div className="flex flex-wrap gap-1">
                  {item.preferred.map(p => (
                    <span key={p} className="text-2xs font-mono px-1.5 py-0.5 rounded bg-emerald-50 text-emerald-800 font-semibold border border-emerald-200">
                      {p}
                    </span>
                  ))}
                </div>
                <div className="text-2xs text-slate-400">{item.desc}</div>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* 4. Policy Group Grid (Unified Cards) */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3">
        {groups.map(g => {
          const isBaseline = BASELINE_GROUP_NAMES.has(g.display_name) || BASELINE_GROUP_NAMES.has(g.name);
          const evalRes = evaluateCount(g, viewPerspective === 'q');

          return (
            <div 
              key={g.name} 
              className={`border rounded-xl p-4 flex flex-col justify-between space-y-3 transition shadow-2xs ${
                !g.enabled ? 'bg-slate-50/60 border-slate-200 opacity-60' :
                isBaseline ? 'bg-white border-slate-300 hover:border-slate-400' :
                'bg-white border-emerald-300 hover:border-emerald-400'
              }`}
            >
              <div className="space-y-1.5">
                <div className="flex items-center justify-between">
                  <div className="flex items-center space-x-1.5 truncate">
                    <strong className="text-sm font-bold text-slate-900 truncate">
                      {g.display_name}
                    </strong>
                    {isBaseline ? (
                      <span className="text-2xs font-mono px-1.5 py-0.2 rounded bg-indigo-50 text-indigo-700 font-semibold border border-indigo-200 flex-shrink-0">
                        核心基准
                      </span>
                    ) : (
                      <span className="text-2xs font-mono px-1.5 py-0.2 rounded bg-emerald-50 text-emerald-700 font-semibold border border-emerald-200 flex-shrink-0">
                        自定义
                      </span>
                    )}
                  </div>
                  <span className={`text-2xs font-mono font-bold px-2 py-0.5 rounded ${
                    g.enabled ? 'bg-emerald-100 text-emerald-800' : 'bg-slate-200 text-slate-600'
                  }`}>
                    {g.enabled ? '启用' : '已停用'}
                  </span>
                </div>

                <div className="text-2xs font-mono text-slate-500 space-y-0.5">
                  <div className="flex items-center space-x-1.5">
                    <span className="text-slate-400">ID:</span>
                    <span className="text-slate-700 font-semibold">{g.name}</span>
                    <span className="text-slate-300">·</span>
                    <span className={`px-1 rounded ${
                      g.type === 'url-test' ? 'bg-blue-50 text-blue-700 font-bold' :
                      g.type === 'select' ? 'bg-amber-50 text-amber-800' : 'bg-slate-100 text-slate-700'
                    }`}>
                      {g.type === 'url-test' ? 'url-test 测速优选' : g.type || 'select'}
                    </span>
                  </div>

                  <div>
                    {g.filter.mode === 'manual' ? (
                      <span>手选 {g.filter.node_ids?.length || 0} 个原子节点</span>
                    ) : (
                      <span>
                        地区: {asText(g.filter.countries) || '不限'} | 种类: {asText(g.filter.kinds) || '全量'}
                        {g.filter.clean_only && ' | 防送中'}
                        {g.filter.china_leak_only && ' | 仅送中'}
                      </span>
                    )}
                  </div>
                </div>

                {/* Perspective Matched Nodes Badge */}
                <div className="pt-1">
                  {evalRes.isIsolated ? (
                    <span className="inline-flex items-center text-2xs font-mono px-2 py-0.5 rounded bg-slate-100 text-slate-500 border border-slate-200">
                      【Q 规范：0 住宅硬隔离 ➔ DIRECT】
                    </span>
                  ) : (
                    <span className={`inline-flex items-center text-2xs font-mono px-2 py-0.5 rounded border ${
                      viewPerspective === 'q' 
                        ? 'bg-blue-50 text-blue-800 border-blue-200' 
                        : 'bg-purple-50 text-purple-800 border-purple-200'
                    }`}>
                      命中 <b>{evalRes.count}</b> 个节点 ({viewPerspective === 'q' ? 'Q 纯骨干' : 'ashan 全量'})
                    </span>
                  )}
                </div>
              </div>

              {/* Action Buttons */}
              <div className="flex items-center justify-between pt-2 border-t border-slate-100 text-xs">
                <span className="text-2xs text-slate-400 font-mono">顺序: {g.order ?? 100}</span>
                <div className="flex gap-2 items-center">
                  <button 
                    onClick={() => edit(g)} 
                    className="text-xs text-blue-700 hover:text-blue-900 font-semibold flex gap-1 items-center"
                  >
                    <Pencil size={12}/>编辑
                  </button>
                  <button 
                    onClick={() => submitSingle({ ...g, enabled: !g.enabled })} 
                    disabled={busy} 
                    className="text-xs text-slate-600 hover:text-slate-900"
                  >
                    {g.enabled ? '停用' : '启用'}
                  </button>
                  <button 
                    onClick={() => remove(g)} 
                    disabled={busy} 
                    className="text-xs text-rose-600 hover:text-rose-800 flex gap-1 items-center"
                  >
                    <Trash2 size={12}/>删除
                  </button>
                </div>
              </div>
            </div>
          );
        })}

        {groups.length === 0 && (
          <div className="col-span-full text-center text-xs text-slate-500 py-10 border border-dashed border-slate-300 rounded-xl space-y-3">
            <p>当前暂存策略列表为空</p>
            <button
              onClick={handleResetDefaults}
              className="px-3.5 py-2 bg-emerald-600 hover:bg-emerald-700 text-white rounded-lg font-semibold text-xs shadow-2xs inline-flex items-center gap-1.5"
            >
              <RotateCcw size={14} />
              <span>一键载入官方 8 大基准组</span>
            </button>
          </div>
        )}
      </div>

      {/* 5. Policy Group Editor Form (Drawer/Modal Inline) */}
      {form && (
        <div className="border-2 border-emerald-500/80 rounded-xl p-5 space-y-4 bg-emerald-50/15 shadow-md">
          <div className="flex items-center justify-between border-b border-emerald-200/60 pb-2">
            <h4 className="font-bold text-sm text-slate-900 flex items-center space-x-2">
              <Sliders className="w-4 h-4 text-emerald-600" />
              <span>{groups.some(g => g.name === form.name) ? `编辑策略组：${form.display_name}` : '新建个性化策略组'}</span>
            </h4>
            <span className="text-2xs font-mono text-slate-400">所有字段修改后即时预览生效</span>
          </div>

          <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-4 gap-3">
            <label className="text-xs">
              <span className="font-semibold text-slate-700">英文标识 ID (不可重复)</span>
              <input 
                className={inputClass} 
                disabled={groups.some(g => g.name === form.name)} 
                value={form.name} 
                onChange={e => setForm({ ...form, name: e.target.value })} 
                placeholder="例如: self-us 或 ashan-tw-resi" 
              />
            </label>

            <label className="text-xs">
              <span className="font-semibold text-slate-700">客户端显示组名 (Display Name)</span>
              <input 
                className={inputClass} 
                value={form.display_name} 
                onChange={e => setForm({ ...form, display_name: e.target.value })} 
                placeholder="例如: 自建-美国和美国家宽" 
              />
            </label>

            <label className="text-xs">
              <span className="font-semibold text-slate-700">策略类型 (Type)</span>
              <select 
                className={inputClass} 
                value={form.type || 'url-test'} 
                onChange={e => setForm({ ...form, type: e.target.value })}
              >
                <option value="url-test">url-test (自动测速优选 - 推荐)</option>
                <option value="select">select (手动选择)</option>
                <option value="fallback">fallback (故障转移)</option>
                <option value="load-balance">load-balance (负载均衡)</option>
              </select>
            </label>

            <label className="text-xs">
              <span className="font-semibold text-slate-700">在 Clash 中的排列顺序</span>
              <input 
                type="number" 
                className={inputClass} 
                value={form.order ?? 100} 
                onChange={e => setForm({ ...form, order: +e.target.value })} 
              />
            </label>
          </div>

          <div className="flex flex-wrap items-center gap-4 text-xs pt-1">
            <label className="flex items-center gap-2 cursor-pointer font-medium text-slate-700">
              <input 
                type="checkbox" 
                checked={!!form.add_to_entry} 
                onChange={e => setForm({ ...form, add_to_entry: e.target.checked })}
                className="rounded border-slate-300 text-emerald-600 focus:ring-emerald-500"
              />
              <span>加入全局「选择代理」入口组</span>
            </label>

            <label className="flex items-center gap-2 cursor-pointer font-medium text-slate-700">
              <input 
                type="checkbox" 
                checked={form.enabled} 
                onChange={e => setForm({ ...form, enabled: e.target.checked })}
                className="rounded border-slate-300 text-emerald-600 focus:ring-emerald-500"
              />
              <span>启用此策略组</span>
            </label>
          </div>

          {/* Filter Rules or Manual Node Picker */}
          <div className="space-y-3 pt-2 border-t border-slate-200">
            <div className="flex gap-5 text-xs font-semibold text-slate-800">
              <label className="flex items-center gap-1.5 cursor-pointer">
                <input 
                  type="radio" 
                  checked={form.filter.mode !== 'manual'} 
                  onChange={() => patchFilter({ mode: 'filter' })}
                />
                <span>规则动态筛选 (按国家/种类/送中标签自动匹配)</span>
              </label>
              <label className="flex items-center gap-1.5 cursor-pointer">
                <input 
                  type="radio" 
                  checked={form.filter.mode === 'manual'} 
                  onChange={() => patchFilter({ mode: 'manual' })}
                />
                <span>手动手选节点 (傻瓜式精准勾选)</span>
              </label>
            </div>

            {form.filter.mode === 'manual' ? (
              <div className="max-h-56 overflow-y-auto border border-slate-300 rounded-lg bg-white p-3 space-y-1.5">
                <div className="text-2xs font-semibold text-slate-400 mb-1">
                  勾选要归入该策略组的原子节点（共 {atoms.length} 个备选）：
                </div>
                {atoms.map(n => (
                  <label key={n.id} className="flex items-center gap-2 text-xs py-1 hover:bg-slate-50 px-1 rounded cursor-pointer">
                    <input 
                      type="checkbox" 
                      checked={(form.filter.node_ids || []).includes(n.id)} 
                      onChange={e => patchFilter({
                        node_ids: e.target.checked 
                          ? [...(form.filter.node_ids || []), n.id] 
                          : (form.filter.node_ids || []).filter(x => x !== n.id)
                      })}
                      className="rounded border-slate-300 text-emerald-600 focus:ring-emerald-500"
                    />
                    <span className="truncate font-medium text-slate-800">{n.name}</span>
                    <span className="text-slate-400 text-2xs font-mono">({n.country || 'GLOBAL'} · {n.kind})</span>
                  </label>
                ))}
              </div>
            ) : (
              <div className="space-y-3 bg-white p-3.5 rounded-xl border border-slate-200">
                <div className="text-xs">
                  <span className="font-semibold text-slate-700">国家/地区筛选（多选，留空为不限地区）:</span>
                  <div className="flex flex-wrap gap-2 mt-1.5">
                    {options.map(c => (
                      <label key={c} className="border border-slate-300 rounded-lg px-2.5 py-1 text-xs cursor-pointer flex items-center gap-1.5 hover:bg-slate-50">
                        <input 
                          type="checkbox" 
                          checked={(form.filter.countries || []).includes(c)} 
                          onChange={() => toggle('countries', c)}
                          className="rounded text-emerald-600"
                        /> 
                        <span>{c}</span>
                      </label>
                    ))}
                  </div>
                </div>

                <div className="text-xs">
                  <span className="font-semibold text-slate-700">节点种类（多选）:</span>
                  <div className="flex flex-wrap gap-2 mt-1.5">
                    {[
                      ['transit', '线路机 (骨干 VPS)'],
                      ['residential', '原生家宽直连'],
                      ['chain', '智能链式中继 (骨干 ➔ 家宽)'],
                    ].map(([k, v]) => (
                      <label key={k} className="border border-slate-300 rounded-lg px-2.5 py-1 text-xs cursor-pointer flex items-center gap-1.5 hover:bg-slate-50">
                        <input 
                          type="checkbox" 
                          checked={(form.filter.kinds || []).includes(k)} 
                          onChange={() => toggle('kinds', k)}
                          className="rounded text-emerald-600"
                        /> 
                        <span>{v}</span>
                      </label>
                    ))}
                  </div>
                </div>

                <div className="flex flex-wrap items-center gap-5 text-xs pt-1">
                  <label className="flex items-center gap-2 cursor-pointer text-slate-700">
                    <input 
                      type="checkbox" 
                      checked={!!(form.filter.clean_only || form.filter.exclude_china_leak)} 
                      onChange={e => patchFilter({ clean_only: e.target.checked, exclude_china_leak: e.target.checked })}
                      className="rounded border-slate-300 text-emerald-600"
                    />
                    <span>排除送中节点 (只保留纯净出口)</span>
                  </label>

                  <label className="flex items-center gap-2 cursor-pointer text-slate-700">
                    <input 
                      type="checkbox" 
                      checked={!!form.filter.china_leak_only} 
                      onChange={e => patchFilter({ china_leak_only: e.target.checked, clean_only: !e.target.checked })}
                      className="rounded border-slate-300 text-rose-600"
                    />
                    <span>仅包含送中节点 (用于送中隔离池)</span>
                  </label>
                </div>
              </div>
            )}
          </div>

          {/* Form Actions */}
          <div className="flex flex-wrap items-center justify-between gap-2 pt-2 border-t border-slate-200">
            <button 
              type="button" 
              onClick={doPreview} 
              disabled={busy} 
              className="px-3.5 py-2 border border-slate-300 bg-white hover:bg-slate-50 rounded-lg text-xs font-semibold text-slate-700 transition"
            >
              🔍 预览当前筛选命中的节点
            </button>

            <div className="flex items-center gap-2">
              <button 
                type="button" 
                onClick={() => setForm(null)} 
                className="px-3 py-2 text-xs text-slate-600 hover:text-slate-900 font-medium"
              >
                取消
              </button>
              <button 
                type="button" 
                onClick={() => submitSingle(form)} 
                disabled={busy} 
                className="px-4 py-2 bg-emerald-600 hover:bg-emerald-700 text-white rounded-lg text-xs font-bold flex items-center gap-1.5 shadow-2xs transition"
              >
                <Save size={14}/>
                <span>保存并异步发布该组</span>
              </button>
            </div>
          </div>

          {preview && (
            <div className="text-xs bg-white border border-slate-200 rounded-lg p-3 max-h-40 overflow-y-auto space-y-1">
              <strong className="text-slate-800">当前条件命中 {preview.count} 个原子节点：</strong>
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-1 pt-1">
                {preview.nodes.map(n => (
                  <p className="text-slate-600 text-2xs truncate py-0.5" key={n.id}>
                    • {n.name}
                  </p>
                ))}
              </div>
            </div>
          )}
        </div>
      )}

      {/* 6. Node NAT / VPS / Tags maintenance (Collapsible) */}
      <details className="border-t border-slate-200 pt-3">
        <summary className="cursor-pointer text-xs font-semibold text-slate-700 hover:text-slate-900 flex items-center justify-between">
          <span>节点 NAT / VPS / 标签元数据维护（{atoms.length} 个原子节点）</span>
          <span className="text-2xs text-slate-400">点击展开/折叠</span>
        </summary>
        <p className="text-2xs text-slate-500 py-2">
          NAT/VPS 不会依据名字自动猜测。你可以逐个标记，供分组筛选使用。
        </p>
        <div className="max-h-48 overflow-auto space-y-1 bg-slate-50/50 p-2 rounded-xl border border-slate-200">
          {atoms.map(n => (
            <div key={n.id} className="flex items-center justify-between border-b border-slate-100 py-1 text-xs gap-2">
              <span className="truncate text-slate-700">
                {n.name} · <span className="font-mono text-2xs text-slate-400">{n.infra_type || '未分类'}</span>
                {n.tags && n.tags.length > 0 && <span className="text-2xs text-indigo-600 ml-1">[{asText(n.tags)}]</span>}
              </span>
              <button 
                className="text-blue-600 hover:underline shrink-0 text-2xs font-semibold" 
                onClick={() => {
                  setMetadataNode(n);
                  setMetadataType(n.infra_type || '');
                  setMetadataTags(asText(n.tags));
                }}
              >
                标记属性
              </button>
            </div>
          ))}
        </div>
      </details>

      {metadataNode && (
        <div className="border border-slate-300 rounded-xl bg-slate-50 p-4 space-y-2.5 text-xs">
          <strong className="text-slate-800">编辑节点元数据：{metadataNode.name}</strong>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
            <select 
              className={inputClass} 
              value={metadataType} 
              onChange={e => setMetadataType(e.target.value)}
            >
              <option value="">未分类基础设施</option>
              {infraOptions.map(([v, label]) => <option value={v} key={v}>{label}</option>)}
            </select>
            <input 
              className={inputClass} 
              value={metadataTags} 
              onChange={e => setMetadataTags(e.target.value)} 
              placeholder="标签，多个以逗号分隔"
            />
          </div>
          <div className="flex gap-2 justify-end">
            <button onClick={() => setMetadataNode(null)} className="px-3 py-1 text-slate-600 hover:bg-slate-200 rounded">取消</button>
            <button onClick={saveMeta} className="bg-blue-600 hover:bg-blue-700 text-white rounded px-3 py-1 font-semibold">保存元数据</button>
          </div>
        </div>
      )}
    </section>
  );
};