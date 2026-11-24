import React, { useCallback, useEffect, useState } from 'react';
import { Plus, Pencil, Trash2, Save, RefreshCw, CheckCircle2, AlertTriangle } from 'lucide-react';
import { api } from '../api/client';
import type { CustomGroupDef, GroupFilter, AtomNode, PublishJob } from '../types';

const blank = (): CustomGroupDef => ({
  name: '', display_name: '', enabled: true, type: 'select', order: 100,
  add_to_entry: false, filter: { mode: 'filter', countries: [], kinds: ['residential', 'chain'], clean_only: true },
});
const options = ['US', 'JP', 'TW', 'HK', 'SG', 'KR', 'UK', 'DE', 'OTHER'];
const infraOptions = [
  ['nat_resi', 'NAT 家宽'], ['vps_resi', 'VPS 家宽'],
  ['nat_line', 'NAT 线路'], ['vps_line', 'VPS 线路'], ['commercial', '商业节点'],
];
const inputClass = 'w-full border rounded-lg px-2.5 py-1.5 text-xs bg-white text-slate-800';
const split = (s: string) => s.split(',').map(x => x.trim()).filter(Boolean);
const asText = (s?: string[]) => (s || []).join(', ');

export const PolicyGroupStudio: React.FC = () => {
  const [groups, setGroups] = useState<CustomGroupDef[]>([]);
  const [atoms, setAtoms] = useState<AtomNode[]>([]);
  const [form, setForm] = useState<CustomGroupDef | null>(null);
  const [preview, setPreview] = useState<{ count: number; nodes: {id: string;name: string}[] } | null>(null);
  const [job, setJob] = useState<PublishJob | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [metadataNode, setMetadataNode] = useState<AtomNode | null>(null);
  const [metadataType, setMetadataType] = useState('');
  const [metadataTags, setMetadataTags] = useState('');

  const load = useCallback(async () => {
    try {
      const [g, a, j] = await Promise.all([
        api.getCustomGroups(), api.getAtoms({}).catch(() => ({ atoms: [], count: 0, status: '' })),
        api.latestPublishJob().catch(() => null),
      ]);
      setGroups(g.groups || []);
      setAtoms(a.atoms || []);
      if (j && j.id) setJob(j);
    } catch (e: any) { setError(e.message || '读取失败'); }
  }, []);
  useEffect(() => { load(); }, [load]);
  useEffect(() => {
    if (!job?.id || ['succeeded', 'failed', 'superseded'].includes(job.status)) return;
    const timer = window.setInterval(() => {
      api.getPublishJob(job.id).then(setJob).catch((e: Error) => setError(e.message));
    }, 1500);
    return () => window.clearInterval(timer);
  }, [job?.id, job?.status]);

  const patchFilter = (changes: Partial<GroupFilter>) => setForm(old => old ? { ...old, filter: { ...old.filter, ...changes } } : null);
  const toggle = (field: 'countries' | 'kinds' | 'infra_types', v: string) => {
    if (!form) return;
    const values = (form.filter[field] || []) as string[];
    patchFilter({ [field]: values.includes(v) ? values.filter(x => x !== v) : [...values, v] });
  };
  const edit = (g?: CustomGroupDef) => { setForm(g ? JSON.parse(JSON.stringify(g)) : blank()); setPreview(null); setError(''); };
  const submit = async (g: CustomGroupDef) => {
    if (!g.name || !g.display_name) { setError('请填写英文 ID 和显示名称'); return; }
    setBusy(true); setError('');
    try {
      const existing = groups.some(x => x.name === g.name);
      const res = existing ? await api.updatePolicyGroup(g.name, g) : await api.createPolicyGroup(g);
      setJob({ id: res.job_id, revision: res.revision, status: 'queued' });
      setForm(null); setPreview(null); await load();
    } catch (e: any) { setError(e.message || '保存失败'); }
    finally { setBusy(false); }
  };
  const remove = async (g: CustomGroupDef) => {
    if (!window.confirm(`确定删除“${g.display_name}”？发布成功后最终订阅中也会移除。`)) return;
    setBusy(true);setError('');
    try {
      const res = await api.removePolicyGroup(g.name);
      setJob({ id: res.job_id, revision: res.revision, status: 'queued' }); await load();
    } catch (e: any) { setError(e.message || '删除失败'); }
    finally { setBusy(false); }
  };
  const doPreview = async () => {
    if (!form) return;
    setBusy(true);setError('');
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

  return <section className="bg-white border border-slate-200 rounded-2xl p-5 space-y-4">
    <div className="flex flex-wrap justify-between items-center gap-3">
      <div><h3 className="text-sm font-bold text-slate-900">用户自定义策略组（所见即所得）</h3>
        <p className="text-xs text-slate-500 mt-1">系统已默认内置 8 大核心自建基准组（线路机/美宽/日宽/全量家宽/送中隔离等）；此处用于自由创建更多个性化扩展组（如特定国别、业务标签或测速优选），保存启用后自动并入 Clash YAML。</p></div>
      <div className="flex gap-2">
        <button onClick={load} className="text-xs border rounded-lg px-3 py-2" title="刷新"><RefreshCw size={14}/></button>
        <button onClick={() => edit()} className="text-xs font-semibold bg-emerald-600 text-white rounded-lg px-3 py-2 flex items-center gap-1"><Plus size={14}/>新建策略组</button>
      </div>
    </div>
    {error && <p role="alert" className="text-xs text-rose-700 bg-rose-50 rounded-lg p-3">{error}</p>}
    {job && <div className={`text-xs rounded-lg px-3 py-2 flex items-start gap-2 ${job.status === 'failed' ? 'bg-rose-50 text-rose-800' : 'bg-slate-50 text-slate-700'}`}>
      {job.status === 'succeeded' ? <CheckCircle2 size={15}/> : <AlertTriangle size={15}/>}
      <div className="flex-1">发布任务 #{job.revision}：{job.status === 'queued' ? '排队中' : job.status === 'running' ? '同步中' : job.status === 'verifying' ? '验证最终 YAML 中' : job.status === 'succeeded' ? 'Sub-Store 发布校验成功（静态缓存仍可能延迟刷新）' : job.status === 'failed' ? '发布失败' : job.status}
        {job.error && <p className="mt-1 break-all">{job.error}</p>}</div>
      {job.status === 'failed' && <button className="underline" onClick={async () => { const j = await api.retryPublishJob(job.id); setJob({ ...j, id: j.job_id }); }}>重试</button>}
    </div>}
    <div className="grid md:grid-cols-2 gap-3">
      {groups.map(g => <div key={g.name} className="border rounded-xl p-3 flex flex-col gap-2">
        <div className="flex justify-between items-center"><strong className="text-sm">{g.display_name}</strong><span className="text-2xs text-slate-500">{g.enabled ? '启用' : '停用'}</span></div>
        <p className="text-xs text-slate-500">{g.name} · {g.type || 'select'} · {g.filter.mode === 'manual' ? `手选 ${g.filter.node_ids?.length || 0} 个` : `筛选 ${asText(g.filter.countries) || '不限地区'} / ${asText(g.filter.kinds)}`}</p>
        <div className="flex gap-2 justify-end">
          <button onClick={() => edit(g)} className="text-xs text-blue-700 flex gap-1 items-center"><Pencil size={13}/>编辑</button>
          <button onClick={() => submit({ ...g, enabled: !g.enabled })} disabled={busy} className="text-xs text-slate-600">{g.enabled ? '停用' : '启用'}</button>
          <button onClick={() => remove(g)} disabled={busy} className="text-xs text-rose-600 flex gap-1 items-center"><Trash2 size={13}/>删除</button>
        </div>
      </div>)}
      {groups.length === 0 && <div className="col-span-full text-center text-xs text-slate-500 py-8 border border-dashed rounded-xl">没有自定义策略组；客户端不应生成任何默认“自建-”组。</div>}
    </div>

    {form && <div className="border border-emerald-200 rounded-xl p-4 space-y-3 bg-emerald-50/20">
      <h4 className="font-bold text-sm">{groups.some(g => g.name === form.name) ? '编辑策略组' : '新建策略组'}</h4>
      <div className="grid sm:grid-cols-2 gap-2">
        <label className="text-xs">英文 ID（保存后不可改）<input className={inputClass} disabled={groups.some(g => g.name === form.name)} value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} placeholder="self-us" /></label>
        <label className="text-xs">客户端显示组名<input className={inputClass} value={form.display_name} onChange={e => setForm({ ...form, display_name: e.target.value })} placeholder="自建-美国" /></label>
        <label className="text-xs">策略类型<select className={inputClass} value={form.type || 'select'} onChange={e => setForm({ ...form, type: e.target.value })}>{['select','url-test','fallback','load-balance'].map(x => <option key={x}>{x}</option>)}</select></label>
        <label className="text-xs">排列顺序<input type="number" className={inputClass} value={form.order ?? 100} onChange={e => setForm({ ...form, order: +e.target.value })} /></label>
      </div>
      <label className="flex gap-2 text-xs"><input type="checkbox" checked={!!form.add_to_entry} onChange={e => setForm({ ...form, add_to_entry: e.target.checked })}/>加入“选择代理”入口组</label>
      <label className="flex gap-2 text-xs"><input type="checkbox" checked={form.enabled} onChange={e => setForm({ ...form, enabled: e.target.checked })}/>启用并发布</label>
      <div className="flex gap-5 text-xs"><label><input type="radio" checked={form.filter.mode !== 'manual'} onChange={() => patchFilter({ mode: 'filter' })}/> 规则筛选</label><label><input type="radio" checked={form.filter.mode === 'manual'} onChange={() => patchFilter({ mode: 'manual' })}/> 手动勾选节点</label></div>
      {form.filter.mode === 'manual' ? <div className="max-h-56 overflow-y-auto border rounded-lg bg-white p-2 space-y-1">
        {atoms.map(n => <label key={n.id} className="flex items-center gap-2 text-xs py-1"><input type="checkbox" checked={(form.filter.node_ids || []).includes(n.id)} onChange={e => patchFilter({node_ids: e.target.checked ? [...(form.filter.node_ids||[]), n.id] : (form.filter.node_ids||[]).filter(x => x!==n.id)})}/><span className="truncate">{n.name} <span className="text-slate-400">({n.country} · {n.kind})</span></span></label>)}
      </div> : <>
        <div className="text-xs">国家/地区（留空不限）<div className="flex flex-wrap gap-2 mt-1">{options.map(c => <label key={c} className="border rounded-lg px-2 py-1 bg-white"><input type="checkbox" checked={(form.filter.countries||[]).includes(c)} onChange={() => toggle('countries',c)}/> {c}</label>)}</div></div>
        <div className="text-xs">节点种类<div className="flex flex-wrap gap-2 mt-1">{[['transit','线路机'],['residential','家宽直连'],['chain','链式中继']].map(([k,v]) => <label key={k} className="border rounded-lg px-2 py-1 bg-white"><input type="checkbox" checked={(form.filter.kinds||[]).includes(k)} onChange={() => toggle('kinds',k)}/> {v}</label>)}</div></div>
        <div className="text-xs">基础设施类型（留空不限；先在下方给节点标记类型）<div className="flex flex-wrap gap-2 mt-1">{infraOptions.map(([k,v]) => <label key={k} className="border rounded-lg px-2 py-1 bg-white"><input type="checkbox" checked={(form.filter.infra_types||[]).includes(k)} onChange={() => toggle('infra_types',k)}/> {v}</label>)}</div></div>
        <div className="grid sm:grid-cols-2 gap-2">
          {([['tags_any','匹配任一标签'],['tags_all','必须包含全部标签'],['exclude_tags','排除标签'],['keywords','节点名包含关键词'],['exclude','节点名排除关键词']] as const).map(([key,label]) => <label key={key} className="text-xs">{label}<input className={inputClass} value={asText(form.filter[key])} onChange={e => patchFilter({ [key]: split(e.target.value) })} placeholder="逗号分隔" /></label>)}
        </div>
        <label className="text-xs flex gap-2"><input type="checkbox" checked={!!(form.filter.clean_only || form.filter.exclude_china_leak)} onChange={e => patchFilter({ clean_only: e.target.checked, exclude_china_leak: e.target.checked })}/>排除送中节点</label>
        <label className="text-xs flex gap-2"><input type="checkbox" checked={!!form.filter.online_only} onChange={e => patchFilter({online_only:e.target.checked})}/>只包含在线节点</label>
      </>}
      <div className="flex gap-2"><button onClick={doPreview} disabled={busy} className="px-3 py-2 border rounded-lg text-xs">预览命中节点</button><button onClick={() => submit(form)} disabled={busy} className="px-3 py-2 bg-emerald-600 text-white rounded-lg text-xs flex gap-1 items-center"><Save size={14}/>保存并异步发布</button><button onClick={() => setForm(null)} className="text-xs">取消</button></div>
      {preview && <div className="text-xs bg-white border rounded-lg p-2 max-h-36 overflow-y-auto"><strong>预览 {preview.count} 个节点</strong>{preview.nodes.map(n => <p className="text-slate-600 py-0.5" key={n.id}>{n.name}</p>)}</div>}
    </div>}

    <details className="border-t pt-3"><summary className="cursor-pointer text-xs font-semibold text-slate-700">节点 NAT / VPS / 标签元数据维护（{atoms.length} 个节点）</summary>
      <p className="text-xs text-slate-500 py-2">NAT/VPS 不会依据名字自动猜测。你可以逐个标记，供分组筛选使用。这里不显示节点连接密钥。</p>
      <div className="max-h-48 overflow-auto space-y-1">{atoms.map(n => <div key={n.id} className="flex items-center justify-between border-b py-1 text-xs gap-2"><span className="truncate">{n.name} · {n.infra_type || '未分类'} · {asText(n.tags)}</span><button className="text-blue-600 shrink-0" onClick={() => {setMetadataNode(n);setMetadataType(n.infra_type || '');setMetadataTags(asText(n.tags));}}>标记</button></div>)}</div>
    </details>
    {metadataNode && <div className="border rounded-xl bg-slate-50 p-3 space-y-2 text-xs"><strong>编辑节点元数据：{metadataNode.name}</strong><select className={inputClass} value={metadataType} onChange={e => setMetadataType(e.target.value)}><option value="">未分类</option>{infraOptions.map(([v,label])=><option value={v} key={v}>{label}</option>)}</select><input className={inputClass} value={metadataTags} onChange={e => setMetadataTags(e.target.value)} placeholder="标签，逗号分隔"/><div className="flex gap-2"><button onClick={saveMeta} className="bg-blue-600 text-white rounded px-3 py-1">保存元数据</button><button onClick={() => setMetadataNode(null)}>取消</button></div></div>}
  </section>;
};
