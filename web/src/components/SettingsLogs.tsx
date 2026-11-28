import React, { useState, useEffect } from 'react';
import {
  Settings,
  RotateCw,
  Save,
  CheckCircle2,
  Loader2,
  HardDrive,
  Globe,
  Plus,
  Gauge,
  Activity,
  AlertTriangle,
  FileText,
} from 'lucide-react';
import { api } from '../api/client';
import { SubStoreConfig, CustomRulesData } from '../types';
import { useFeedback } from '../context/FeedbackContext';

interface SettingsLogsProps {
  focusSection?: string | null;
}

export const SettingsLogs: React.FC<SettingsLogsProps> = ({ focusSection }) => {
  const { pushActivity, setConsoleOpen } = useFeedback();

  // Speedtest Source Config
  const [url10m, setUrl10m] = useState('');
  const [url30m, setUrl30m] = useState('');
  const [testingSpeed, setTestingSpeed] = useState(false);
  const [savingSpeed, setSavingSpeed] = useState(false);
  const [speedProbeResults, setSpeedProbeResults] = useState<
    Array<{ slot: string; url: string; ok: boolean; status_code?: number; sampled_bytes?: number; error?: string }>
  >([]);

  // SubStore & Rules
  const [subCfg, setSubCfg] = useState<SubStoreConfig | null>(null);
  const [sysEnv, setSysEnv] = useState<any>(null);
  const [rules, setRules] = useState<CustomRulesData | null>(null);
  const [newDomain, setNewDomain] = useState('');
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [addingDomain, setAddingDomain] = useState(false);

  // Recent Backend Logs Viewer
  const [logSource, setLogSource] = useState<'runtime' | 'audit'>('runtime');
  const [logLines, setLogLines] = useState<string[]>([]);
  const [loadingLogs, setLoadingLogs] = useState(false);

  const fetchData = async () => {
    setLoading(true);
    try {
      const [cfg, env, rData, spCfg] = await Promise.all([
        api.getSubStoreConfig().catch(() => null),
        api.getSystemEnv().catch(() => null),
        api.getCustomRules().catch(() => null),
        api.getSpeedTestConfig().catch(() => null),
      ]);
      if (cfg) setSubCfg(cfg);
      if (env) setSysEnv(env);
      if (rData) setRules(rData);
      if (spCfg) {
        setUrl10m(spCfg.self_hosted_url_10m || '');
        setUrl30m(spCfg.self_hosted_url_30m || '');
      }
    } catch (err: any) {
      pushActivity({
        level: 'error',
        source: '设置',
        title: '加载系统设置失败',
        detail: err.message,
      });
    } finally {
      setLoading(false);
    }
  };

  const fetchRecentLogs = async (src: 'runtime' | 'audit' = logSource) => {
    setLoadingLogs(true);
    try {
      const res = await api.getRecentLogs(src, 120);
      setLogLines(res.lines || []);
    } catch (err: any) {
      pushActivity({
        level: 'error',
        source: '日志',
        title: '拉取后端日志失败',
        detail: err.message,
      });
    } finally {
      setLoadingLogs(false);
    }
  };

  useEffect(() => {
    fetchData();
    fetchRecentLogs('runtime');
  }, []);

  const handleTestSpeedEndpoints = async () => {
    if (!url10m.trim() && !url30m.trim()) {
      pushActivity({
        level: 'warn',
        source: '测速设置',
        title: '请先填写至少一个自建测速 URL',
        detail: '例如 http://your-vps/speedtest/10m.bin（需免 Basic Auth 访问）',
      });
      return;
    }
    setTestingSpeed(true);
    try {
      const res = await api.testSpeedTestConfig(url10m.trim(), url30m.trim());
      setSpeedProbeResults(res.results || []);
      if (res.ok) {
        pushActivity({
          level: 'success',
          source: '测速设置',
          title: '自建测速端点连通性预检通过',
          detail: (res.results || []).map((r) => `${r.slot.toUpperCase()} HTTP ${r.status_code} (${r.sampled_bytes}B)`).join(' · '),
        });
      } else {
        const failed = (res.results || []).filter((r) => !r.ok);
        pushActivity({
          level: 'error',
          source: '测速设置',
          title: '测速端点连通性预检未通过',
          detail: failed.map((f) => `[${f.slot.toUpperCase()}] ${f.error}`).join('；'),
        });
      }
    } catch (err: any) {
      pushActivity({
        level: 'error',
        source: '测速设置',
        title: '测速端点预检请求失败',
        detail: err.message,
      });
    } finally {
      setTestingSpeed(false);
    }
  };

  const handleSaveSpeedConfig = async (verify: boolean = true) => {
    setSavingSpeed(true);
    try {
      const res = await api.setSpeedTestConfig(url10m.trim(), url30m.trim(), verify);
      setSpeedProbeResults([]);
      pushActivity({
        level: 'success',
        source: '测速设置',
        title: '自建测速配置已持久化保存',
        detail: `${res.message || ''}（已同步至 data/speedtest_config.json${res.persisted_yaml ? ' 与 config.yaml' : ''}）`,
      });
    } catch (err: any) {
      pushActivity({
        level: 'error',
        source: '测速设置',
        title: '保存自建测速配置被拦截',
        detail: err.message,
      });
    } finally {
      setSavingSpeed(false);
    }
  };

  const handleAddDirectDomain = async (e: React.FormEvent) => {
    e.preventDefault();
    const d = newDomain.trim();
    if (!d) return;
    setAddingDomain(true);
    try {
      await api.addDirectDomain(d);
      pushActivity({
        level: 'success',
        source: '分流规则',
        title: `直连域名【${d}】已加入白名单并热重载`,
      });
      setNewDomain('');
      const rData = await api.getCustomRules().catch(() => null);
      if (rData) setRules(rData);
    } catch (err: any) {
      pushActivity({
        level: 'error',
        source: '分流规则',
        title: '添加直连域名失败',
        detail: err.message,
      });
    } finally {
      setAddingDomain(false);
    }
  };

  const handleSaveSubStore = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!subCfg) return;
    setSaving(true);
    try {
      await api.updateSubStoreConfig(subCfg);
      pushActivity({
        level: 'success',
        source: 'SubStore',
        title: 'Sub-Store 连接参数已持久化并热重载',
      });
    } catch (err: any) {
      pushActivity({
        level: 'error',
        source: 'SubStore',
        title: '保存 Sub-Store 配置失败',
        detail: err.message,
      });
    } finally {
      setSaving(false);
    }
  };

  const handleReloadConfig = async () => {
    setSaving(true);
    try {
      const res = await api.reloadConfig();
      pushActivity({
        level: 'success',
        source: '系统配置',
        title: '磁盘配置已全量重载',
        detail: res.message,
      });
      await fetchData();
    } catch (err: any) {
      pushActivity({
        level: 'error',
        source: '系统配置',
        title: '磁盘配置重载失败',
        detail: err.message,
      });
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="space-y-5">
      {/* 1. Self-Hosted Speedtest Config (Persistent + Pre-flight Verify) */}
      <div
        id="settings-speedtest-section"
        className={`bg-white rounded-xl p-5 shadow-2xs space-y-4 border transition ${
          focusSection === 'speedtest'
            ? 'border-blue-500 ring-2 ring-blue-100'
            : 'border-slate-200'
        }`}
      >
        <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-2 pb-3 border-b border-slate-100">
          <div>
            <h2 className="text-sm font-bold text-slate-900 flex items-center">
              <Gauge className="w-4 h-4 mr-2 text-blue-600" />
              自建 10M / 30M 测速文件端点（持久化 + 免鉴权预检）
            </h2>
            <p className="text-xs text-slate-500 mt-0.5">
              严禁任何公网测速站回退；保存时自动预检免鉴权可达性（拦截 401/404），并同步持久化到 config.yaml 与 data/speedtest_config.json。
            </p>
          </div>
          <span
            className={`px-2 py-0.5 rounded-full text-[11px] font-mono border shrink-0 ${
              url10m.trim()
                ? 'bg-emerald-50 text-emerald-700 border-emerald-200'
                : 'bg-amber-50 text-amber-800 border-amber-200'
            }`}
          >
            {url10m.trim() ? '10M 端点已配置' : '10M 端点未配置'}
          </span>
        </div>

        <div className="grid grid-cols-1 md:grid-cols-2 gap-4 text-xs">
          <div>
            <label className="block font-semibold text-slate-700 mb-1">
              10MB 自建测速文件 URL（日常平滑吞吐测速）
            </label>
            <input
              type="url"
              value={url10m}
              onChange={(e) => setUrl10m(e.target.value)}
              placeholder="https://free.1992004.xyz/speedtest/10m.bin"
              className="w-full px-3 py-2 border border-slate-300 rounded-lg font-mono text-xs focus:ring-1 focus:ring-blue-600 focus:outline-none"
            />
          </div>
          <div>
            <label className="block font-semibold text-slate-700 mb-1">
              30MB 自建测速文件 URL（流式大吞吐采样，可选）
            </label>
            <input
              type="url"
              value={url30m}
              onChange={(e) => setUrl30m(e.target.value)}
              placeholder="https://free.1992004.xyz/speedtest/30m.bin"
              className="w-full px-3 py-2 border border-slate-300 rounded-lg font-mono text-xs focus:ring-1 focus:ring-blue-600 focus:outline-none"
            />
          </div>
        </div>

        {speedProbeResults.length > 0 && (
          <div className="space-y-1.5 bg-slate-50 border border-slate-200 rounded-lg p-3 text-xs font-mono">
            {speedProbeResults.map((r) => (
              <div key={r.slot} className="flex items-center justify-between gap-2">
                <div className="flex items-center gap-2 min-w-0">
                  {r.ok ? (
                    <CheckCircle2 className="w-3.5 h-3.5 text-emerald-600 shrink-0" />
                  ) : (
                    <AlertTriangle className="w-3.5 h-3.5 text-rose-600 shrink-0" />
                  )}
                  <span className="uppercase font-bold text-slate-700">{r.slot}:</span>
                  <span className="truncate text-slate-600">{r.url}</span>
                </div>
                <span className={r.ok ? 'text-emerald-700 shrink-0' : 'text-rose-700 shrink-0'}>
                  {r.ok ? `HTTP ${r.status_code} · 采样 ${r.sampled_bytes}B` : r.error}
                </span>
              </div>
            ))}
          </div>
        )}

        <div className="flex flex-wrap items-center justify-end gap-2 pt-1">
          <button
            type="button"
            onClick={handleTestSpeedEndpoints}
            disabled={testingSpeed || savingSpeed}
            className="inline-flex items-center px-3 py-1.5 text-xs font-medium rounded-lg text-slate-700 bg-white border border-slate-300 hover:bg-slate-50 disabled:opacity-50 transition"
          >
            {testingSpeed ? <Loader2 className="w-3.5 h-3.5 mr-1.5 animate-spin" /> : <Activity className="w-3.5 h-3.5 mr-1.5 text-blue-600" />}
            连通性预检
          </button>
          <button
            type="button"
            onClick={() => handleSaveSpeedConfig(true)}
            disabled={savingSpeed || testingSpeed}
            className="inline-flex items-center px-4 py-1.5 text-xs font-semibold rounded-lg text-white bg-blue-600 hover:bg-blue-700 disabled:opacity-50 transition shadow-2xs"
          >
            {savingSpeed ? <Loader2 className="w-3.5 h-3.5 mr-1.5 animate-spin" /> : <Save className="w-3.5 h-3.5 mr-1.5" />}
            预检并持久化保存
          </button>
        </div>
      </div>

      {/* 2. SubStore Connection Settings */}
      <div className="bg-white border border-slate-200 rounded-xl p-5 shadow-2xs space-y-4">
        <div className="flex items-center justify-between pb-3 border-b border-slate-100">
          <div>
            <h2 className="text-sm font-bold text-slate-900 flex items-center">
              <Settings className="w-4 h-4 mr-2 text-blue-600" />
              Sub-Store 远程连接参数与热重载
            </h2>
            <p className="text-xs text-slate-500 mt-0.5">
              修改后即刻生效并自动覆写保存至本地 config.yaml
            </p>
          </div>

          <button
            type="button"
            onClick={handleReloadConfig}
            disabled={saving || loading}
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
                <label className="block font-semibold text-slate-700 mb-1">SubStore 服务端 URL</label>
                <input
                  type="text"
                  value={subCfg.url || ''}
                  onChange={(e) => setSubCfg({ ...subCfg, url: e.target.value })}
                  className="w-full px-3 py-2 border border-slate-300 rounded-lg font-mono text-xs focus:ring-1 focus:ring-blue-600 focus:outline-none"
                />
              </div>

              <div>
                <label className="block font-semibold text-slate-700 mb-1">登录用户名</label>
                <input
                  type="text"
                  value={subCfg.username || ''}
                  onChange={(e) => setSubCfg({ ...subCfg, username: e.target.value })}
                  className="w-full px-3 py-2 border border-slate-300 rounded-lg text-xs focus:ring-1 focus:ring-blue-600 focus:outline-none"
                />
              </div>

              <div>
                <label className="block font-semibold text-slate-700 mb-1">密码 / Secret</label>
                <input
                  type="password"
                  value={subCfg.password || ''}
                  onChange={(e) => setSubCfg({ ...subCfg, password: e.target.value })}
                  placeholder="留空则保持原密码"
                  className="w-full px-3 py-2 border border-slate-300 rounded-lg font-mono text-xs focus:ring-1 focus:ring-blue-600 focus:outline-none"
                />
              </div>

              <div>
                <label className="block font-semibold text-slate-700 mb-1">自有节点主订阅名称</label>
                <input
                  type="text"
                  value={subCfg.group_self_online || ''}
                  onChange={(e) => setSubCfg({ ...subCfg, group_self_online: e.target.value })}
                  className="w-full px-3 py-2 border border-slate-300 rounded-lg font-mono text-xs focus:ring-1 focus:ring-blue-600 focus:outline-none"
                />
              </div>
            </div>

            <div className="flex justify-end pt-1">
              <button
                type="submit"
                disabled={saving}
                className="inline-flex items-center px-4 py-1.5 text-xs font-semibold rounded-lg text-white bg-blue-600 hover:bg-blue-700 disabled:opacity-50 transition shadow-2xs"
              >
                {saving ? <Loader2 className="w-3.5 h-3.5 mr-1.5 animate-spin" /> : <Save className="w-3.5 h-3.5 mr-1.5" />}
                保存并热重载 Sub-Store
              </button>
            </div>
          </form>
        ) : (
          <div className="text-slate-400 text-xs py-4 text-center">加载 SubStore 配置中...</div>
        )}
      </div>

      {/* 3. Custom Direct Domains & Runtime Environment */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-5">
        <div className="bg-white border border-slate-200 rounded-xl p-5 shadow-2xs space-y-4">
          <div className="pb-2 border-b border-slate-100">
            <h3 className="text-sm font-bold text-slate-900 flex items-center">
              <Globe className="w-4 h-4 mr-2 text-indigo-600" />
              国内直连域名白名单（Patch 算子）
            </h3>
            <p className="text-xs text-slate-500 mt-0.5">动态添加强制直连域名并热重载规则</p>
          </div>

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
              className="inline-flex items-center px-3.5 py-2 text-xs font-semibold rounded-lg text-white bg-indigo-600 hover:bg-indigo-700 disabled:opacity-50 transition"
            >
              {addingDomain ? <Loader2 className="w-3.5 h-3.5 mr-1 animate-spin" /> : <Plus className="w-3.5 h-3.5 mr-1" />}
              添加
            </button>
          </form>

          {rules?.direct_domains && rules.direct_domains.length > 0 ? (
            <div className="flex flex-wrap gap-1.5">
              {rules.direct_domains.map((dom) => (
                <span
                  key={dom}
                  className="px-2 py-0.5 bg-slate-100 text-slate-700 border border-slate-200 rounded font-mono text-xs"
                >
                  {dom}
                </span>
              ))}
            </div>
          ) : (
            <div className="text-slate-400 text-2xs italic">尚未配置额外直连域名（已内置国内常见白名单）。</div>
          )}
        </div>

        <div className="bg-white border border-slate-200 rounded-xl p-5 shadow-2xs space-y-3">
          <h3 className="text-sm font-bold text-slate-900 flex items-center pb-2 border-b border-slate-100">
            <HardDrive className="w-4 h-4 mr-2 text-blue-600" />
            探针运行时环境
          </h3>
          <div className="grid grid-cols-2 gap-3 text-xs font-mono">
            <div className="p-3 bg-slate-50 border border-slate-200 rounded-lg">
              <div className="text-2xs text-slate-500">运行架构</div>
              <div className="font-semibold text-slate-800 mt-1">{sysEnv ? `${sysEnv.os} / ${sysEnv.arch}` : '...'}</div>
            </div>
            <div className="p-3 bg-slate-50 border border-slate-200 rounded-lg">
              <div className="text-2xs text-slate-500">Go 版本</div>
              <div className="font-semibold text-slate-800 mt-1">{sysEnv ? sysEnv.go_version : '...'}</div>
            </div>
            <div className="p-3 bg-slate-50 border border-slate-200 rounded-lg truncate">
              <div className="text-2xs text-slate-500">sing-box 核心</div>
              <div className="font-semibold text-slate-800 mt-1 truncate">{sysEnv ? sysEnv.singbox_bin : 'sing-box'}</div>
            </div>
            <div className="p-3 bg-slate-50 border border-slate-200 rounded-lg">
              <div className="text-2xs text-slate-500">核心状态</div>
              <div className="font-semibold text-emerald-600 mt-1 flex items-center">
                <CheckCircle2 className="w-3.5 h-3.5 mr-1" />
                就绪
              </div>
            </div>
          </div>
        </div>
      </div>

      {/* 4. Backend Logs & Audit Trail */}
      <div className="bg-white border border-slate-200 rounded-xl p-5 shadow-2xs space-y-3">
        <div className="flex flex-wrap items-center justify-between gap-2 pb-3 border-b border-slate-100">
          <div>
            <h3 className="text-sm font-bold text-slate-900 flex items-center">
              <FileText className="w-4 h-4 mr-2 text-slate-700" />
              服务端运行日志与操作审计台账
            </h3>
            <p className="text-xs text-slate-500 mt-0.5">实时读取最新 120 条后端日志；即时前端台位事件流请点击底部「台位操作日志」栏</p>
          </div>

          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={() => setConsoleOpen(true)}
              className="px-2.5 py-1 text-xs font-medium rounded-lg border border-slate-200 bg-slate-50 hover:bg-slate-100 text-slate-700"
            >
              展开底部台位日志
            </button>
            <div className="inline-flex rounded-lg border border-slate-200 p-0.5 bg-slate-50 text-xs">
              <button
                type="button"
                onClick={() => {
                  setLogSource('runtime');
                  fetchRecentLogs('runtime');
                }}
                className={`px-2.5 py-1 rounded-md font-medium ${
                  logSource === 'runtime' ? 'bg-white text-blue-700 shadow-2xs' : 'text-slate-600'
                }`}
              >
                运行日志
              </button>
              <button
                type="button"
                onClick={() => {
                  setLogSource('audit');
                  fetchRecentLogs('audit');
                }}
                className={`px-2.5 py-1 rounded-md font-medium ${
                  logSource === 'audit' ? 'bg-white text-blue-700 shadow-2xs' : 'text-slate-600'
                }`}
              >
                审计日志
              </button>
            </div>
            <button
              type="button"
              onClick={() => fetchRecentLogs(logSource)}
              disabled={loadingLogs}
              className="inline-flex items-center px-2.5 py-1 text-xs rounded-lg border border-slate-200 hover:bg-slate-50 text-slate-700"
            >
              <RotateCw className={`w-3.5 h-3.5 mr-1 ${loadingLogs ? 'animate-spin' : ''}`} />
              刷新
            </button>
          </div>
        </div>

        <div className="bg-slate-950 text-slate-200 rounded-xl p-3 font-mono text-[11px] h-64 overflow-y-auto space-y-1 border border-slate-800">
          {logLines.length === 0 ? (
            <div className="text-slate-500 text-center py-10">暂无日志记录</div>
          ) : (
            logLines.map((line, i) => (
              <div
                key={i}
                className={`whitespace-pre-wrap break-all ${
                  line.includes('ERROR') || line.includes('FAIL')
                    ? 'text-rose-300'
                    : line.includes('WARN')
                    ? 'text-amber-300'
                    : 'text-slate-300'
                }`}
              >
                {line}
              </div>
            ))
          )}
        </div>
      </div>
    </div>
  );
};
