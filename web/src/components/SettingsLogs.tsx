import React, { useState, useEffect } from 'react';
import { 
  Settings, 
  RotateCw, 
  Save, 
  Server, 
  ShieldCheck, 
  FileText, 
  CheckCircle2, 
  AlertCircle,
  Loader2,
  HardDrive,
  Globe,
  Plus
} from 'lucide-react';
import { api } from '../api/client';
import { SubStoreConfig, CustomRulesData } from '../types';

export const SettingsLogs: React.FC = () => {
  const [subCfg, setSubCfg] = useState<SubStoreConfig | null>(null);
  const [sysEnv, setSysEnv] = useState<any>(null);
  const [rules, setRules] = useState<CustomRulesData | null>(null);
  const [newDomain, setNewDomain] = useState('');
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [addingDomain, setAddingDomain] = useState(false);
  const [statusMsg, setStatusMsg] = useState<{ type: 'success' | 'error'; text: string } | null>(null);

  const fetchData = async () => {
    setLoading(true);
    try {
      const [cfg, env, rData] = await Promise.all([
        api.getSubStoreConfig().catch(() => null),
        api.getSystemEnv().catch(() => null),
        api.getCustomRules().catch(() => null),
      ]);
      if (cfg) setSubCfg(cfg);
      if (env) setSysEnv(env);
      if (rData) setRules(rData);
    } catch (err: any) {
      setStatusMsg({ type: 'error', text: err.message || '加载系统设置失败' });
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchData();
  }, []);

  const handleAddDirectDomain = async (e: React.FormEvent) => {
    e.preventDefault();
    const d = newDomain.trim();
    if (!d) return;
    setAddingDomain(true);
    setStatusMsg(null);
    try {
      await api.addDirectDomain(d);
      setStatusMsg({ type: 'success', text: `域名【${d}】已成功加入国内直连白名单并热重载生效！` });
      setNewDomain('');
      const rData = await api.getCustomRules().catch(() => null);
      if (rData) setRules(rData);
    } catch (err: any) {
      setStatusMsg({ type: 'error', text: `添加直连域名失败: ${err.message}` });
    } finally {
      setAddingDomain(false);
    }
  };

  const handleSaveSubStore = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!subCfg) return;
    setSaving(true);
    setStatusMsg(null);
    try {
      await api.updateSubStoreConfig(subCfg);
      setStatusMsg({ type: 'success', text: 'Sub-Store 连接参数已热更新持久化并即刻生效！' });
    } catch (err: any) {
      setStatusMsg({ type: 'error', text: `保存失败: ${err.message}` });
    } finally {
      setSaving(false);
    }
  };

  const handleReloadConfig = async () => {
    setSaving(true);
    setStatusMsg(null);
    try {
      const res = await api.reloadConfig();
      setStatusMsg({ type: 'success', text: res.message || '全量配置已成功从磁盘重载！' });
      await fetchData();
    } catch (err: any) {
      setStatusMsg({ type: 'error', text: `磁盘配置重载失败: ${err.message}` });
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="space-y-6">
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

      {/* 1. SubStore Connection Settings */}
      <div className="bg-white border border-slate-200 rounded-xl p-5 shadow-2xs space-y-4">
        <div className="flex items-center justify-between pb-3 border-b border-slate-100">
          <div>
            <h2 className="text-base font-bold text-slate-900 flex items-center">
              <Settings className="w-5 h-5 mr-2 text-blue-600" />
              Sub-Store 远程连接参数与热重载
            </h2>
            <p className="text-xs text-slate-500 mt-0.5">
              修改后即刻生效并自动覆写保存至本地 config.yaml，无需重启服务
            </p>
          </div>

          <button
            onClick={handleReloadConfig}
            disabled={saving}
            className="inline-flex items-center px-3 py-1.5 text-xs font-medium rounded-lg text-slate-700 bg-white border border-slate-200 hover:bg-slate-50 disabled:opacity-50 transition shadow-2xs"
          >
            <RotateCw className="w-3.5 h-3.5 mr-1.5 text-slate-500" />
            从磁盘重新载入
          </button>
        </div>

        {subCfg ? (
          <form onSubmit={handleSaveSubStore} className="space-y-4 text-xs">
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div>
                <label className="block font-semibold text-slate-700 mb-1">
                  SubStore 服务端 URL
                </label>
                <input
                  type="text"
                  value={subCfg.url || ''}
                  onChange={(e) => setSubCfg({ ...subCfg, url: e.target.value })}
                  className="w-full px-3 py-2 border border-slate-300 rounded-lg font-mono text-xs focus:ring-1 focus:ring-blue-600 focus:outline-none"
                />
              </div>

              <div>
                <label className="block font-semibold text-slate-700 mb-1">
                  登录用户名
                </label>
                <input
                  type="text"
                  value={subCfg.username || ''}
                  onChange={(e) => setSubCfg({ ...subCfg, username: e.target.value })}
                  className="w-full px-3 py-2 border border-slate-300 rounded-lg text-xs focus:ring-1 focus:ring-blue-600 focus:outline-none"
                />
              </div>

              <div>
                <label className="block font-semibold text-slate-700 mb-1">
                  密码 / Secret
                </label>
                <input
                  type="password"
                  value={subCfg.password || ''}
                  onChange={(e) => setSubCfg({ ...subCfg, password: e.target.value })}
                  placeholder="留空则保持原密码"
                  className="w-full px-3 py-2 border border-slate-300 rounded-lg font-mono text-xs focus:ring-1 focus:ring-blue-600 focus:outline-none"
                />
              </div>

              <div>
                <label className="block font-semibold text-slate-700 mb-1">
                  自有节点主订阅名称
                </label>
                <input
                  type="text"
                  value={subCfg.group_self_online || ''}
                  onChange={(e) => setSubCfg({ ...subCfg, group_self_online: e.target.value })}
                  className="w-full px-3 py-2 border border-slate-300 rounded-lg font-mono text-xs focus:ring-1 focus:ring-blue-600 focus:outline-none"
                />
              </div>
            </div>

            <div className="flex justify-end pt-2">
              <button
                type="submit"
                disabled={saving}
                className="inline-flex items-center px-4 py-2 text-xs font-semibold rounded-lg text-white bg-blue-600 hover:bg-blue-700 disabled:opacity-50 transition shadow-2xs"
              >
                {saving ? (
                  <>
                    <Loader2 className="w-3.5 h-3.5 mr-1.5 animate-spin" />
                    保存中...
                  </>
                ) : (
                  <>
                    <Save className="w-3.5 h-3.5 mr-1.5" />
                    保存并热重载配置
                  </>
                )}
              </button>
            </div>
          </form>
        ) : (
          <div className="text-slate-400 text-xs py-4 text-center">
            加载 SubStore 配置中...
          </div>
        )}
      </div>

      {/* 2. System Runtime Environment */}
      <div className="bg-white border border-slate-200 rounded-xl p-5 shadow-2xs space-y-3">
        <h3 className="text-sm font-bold text-slate-900 flex items-center">
          <HardDrive className="w-4 h-4 mr-2 text-blue-600" />
          系统运行时与探针环境诊断
        </h3>

        <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 text-xs">
          <div className="p-3 bg-slate-50 border border-slate-200 rounded-lg font-mono">
            <div className="text-2xs text-slate-500">运行平台 / 架构</div>
            <div className="font-semibold text-slate-800 mt-1">
              {sysEnv ? `${sysEnv.os} / ${sysEnv.arch}` : 'Loading...'}
            </div>
          </div>

          <div className="p-3 bg-slate-50 border border-slate-200 rounded-lg font-mono">
            <div className="text-2xs text-slate-500">Go 运行时版本</div>
            <div className="font-semibold text-slate-800 mt-1">
              {sysEnv ? sysEnv.go_version : 'Loading...'}
            </div>
          </div>

          <div className="p-3 bg-slate-50 border border-slate-200 rounded-lg font-mono">
            <div className="text-2xs text-slate-500">内置 sing-box 二进制</div>
            <div className="font-semibold text-slate-800 mt-1 truncate">
              {sysEnv ? sysEnv.singbox_bin : 'sing-box'}
            </div>
          </div>

          <div className="p-3 bg-slate-50 border border-slate-200 rounded-lg font-mono">
            <div className="text-2xs text-slate-500">sing-box 状态</div>
            <div className="font-semibold text-emerald-600 mt-1 flex items-center">
              <CheckCircle2 className="w-3.5 h-3.5 mr-1" />
              就绪可用
            </div>
          </div>
        </div>
      </div>

      {/* 3. Custom Direct Domains & Split Rules (直连域名白名单与自定义规则) */}
      <div className="bg-white border border-slate-200 rounded-xl p-5 shadow-2xs space-y-4">
        <div className="flex items-center justify-between pb-3 border-b border-slate-100">
          <div>
            <h3 className="text-sm font-bold text-slate-900 flex items-center">
              <Globe className="w-4 h-4 mr-2 text-indigo-600" />
              分流规则补丁与国内直连域名管理
            </h3>
            <p className="text-xs text-slate-500 mt-0.5">
              动态添加需要强制国内直连解析的办公、企业或政务域名，自动合并进自定义 Patch 算子
            </p>
          </div>
        </div>

        {/* Add Domain Form */}
        <form onSubmit={handleAddDirectDomain} className="flex gap-2">
          <input
            type="text"
            required
            placeholder="例如: oa.company.com 或 .edu.cn"
            value={newDomain}
            onChange={(e) => setNewDomain(e.target.value)}
            className="flex-1 px-3 py-2 border border-slate-300 rounded-lg font-mono text-xs focus:ring-1 focus:ring-indigo-600 focus:outline-none"
          />
          <button
            type="submit"
            disabled={addingDomain}
            className="inline-flex items-center px-4 py-2 text-xs font-semibold rounded-lg text-white bg-indigo-600 hover:bg-indigo-700 disabled:opacity-50 transition shadow-2xs"
          >
            {addingDomain ? (
              <Loader2 className="w-3.5 h-3.5 mr-1.5 animate-spin" />
            ) : (
              <Plus className="w-3.5 h-3.5 mr-1.5" />
            )}
            添加直连域名
          </button>
        </form>

        {/* Current Direct Domains */}
        {rules?.direct_domains && rules.direct_domains.length > 0 ? (
          <div className="space-y-1.5">
            <span className="text-2xs font-semibold text-slate-500 uppercase tracking-wider block">
              当前已生效的自定义直连域名 ({rules.direct_domains.length}):
            </span>
            <div className="flex flex-wrap gap-1.5">
              {rules.direct_domains.map((dom) => (
                <span
                  key={dom}
                  className="px-2.5 py-1 bg-slate-100 text-slate-700 border border-slate-200 rounded-lg font-mono text-xs"
                >
                  {dom}
                </span>
              ))}
            </div>
          </div>
        ) : (
          <div className="text-slate-400 text-2xs italic">
            尚未配置自定义直连域名（内置默认涵盖国内常见 CDN 与常用白名单）。
          </div>
        )}
      </div>
    </div>
  );
};
