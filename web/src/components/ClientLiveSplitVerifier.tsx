import React, { useState, useEffect, useCallback } from 'react';
import { 
  Play, 
  RotateCw, 
  Globe, 
  CheckCircle2, 
  XCircle, 
  AlertTriangle, 
  Copy, 
  Check, 
  Layers,
  Radio
} from 'lucide-react';

export interface ClientSplitItem {
  id: string;
  name: string;
  category: 'ai' | 'media' | 'direct';
  domain: string;
  testUrl: string;
  isCF: boolean;
  status: 'idle' | 'testing' | 'ok' | 'blocked' | 'error';
  latencyMs: number;
  exitIp?: string;
  city?: string;
  country?: string;
  countryCode?: string;
  isp?: string;
  ipType?: '家宽' | '机房';
  detail?: string;
}

export const determineIPType = (ip?: string, isp?: string, org?: string): '家宽' | '机房' => {
  if (!ip) return '机房';
  const trimmed = ip.trim();
  if (
    trimmed.startsWith('104.28.') || trimmed.startsWith('104.29.') ||
    trimmed.startsWith('162.158.') || trimmed.startsWith('172.64.') ||
    trimmed.startsWith('172.65.') || trimmed.startsWith('172.66.') ||
    trimmed.startsWith('172.67.') || trimmed.startsWith('172.68.') ||
    trimmed.startsWith('172.69.') || trimmed.startsWith('172.70.') ||
    trimmed.startsWith('172.71.') || trimmed.startsWith('198.41.')
  ) {
    return '机房';
  }
  const text = `${isp || ''} ${org || ''}`.toLowerCase();
  const dcKeywords = [
    'cloudflare', 'amazon', 'aws', 'google', 'microsoft', 'azure', 'digitalocean',
    'linode', 'akamai', 'fastly', 'vultr', 'hetzner', 'ovh', 'oracle', 'alibaba',
    'aliyun', 'tencent', 'hosting', 'datacenter', 'data center', 'server', 'leaseweb',
    'choopa', 'm247', 'zenlayer', 'cogent', 'hurricane', 'psychz', 'colocrossing', 'contabo'
  ];
  for (const kw of dcKeywords) {
    if (text.includes(kw)) return '机房';
  }
  const resiKeywords = [
    'frontier', 'verizon', 'comcast', 'at&t', 'charter', 'spectrum', 'cox',
    'centurylink', 'lumen', 'optimum', 'xfinity', 'kddi', 'softbank',
    'iij', 'sonet', 'so-net', 'hkbn', 'hgc', 'pccw', 'netvigator',
    'chunghwa', 'hinet', 'kbro', 'china telecom', 'china unicom', 'china mobile',
    'broadband', 'telecom', 'cable', 'fiber', 'ftth', 'residential', 'dialup', 'isp'
  ];
  for (const kw of resiKeywords) {
    if (text.includes(kw)) return '家宽';
  }
  return '机房';
};

const DEFAULT_TARGETS: Omit<ClientSplitItem, 'status' | 'latencyMs'>[] = [
  {
    id: 'chatgpt',
    name: 'ChatGPT / OpenAI',
    category: 'ai',
    domain: 'chatgpt.com',
    testUrl: 'https://chatgpt.com/cdn-cgi/trace',
    isCF: true,
  },
  {
    id: 'claude',
    name: 'Claude / Anthropic',
    category: 'ai',
    domain: 'claude.ai',
    testUrl: 'https://claude.ai/cdn-cgi/trace',
    isCF: true,
  },
  {
    id: 'gemini',
    name: 'Gemini (Google AI)',
    category: 'ai',
    domain: 'gemini.google.com',
    testUrl: 'https://gemini.google.com/',
    isCF: false,
  },
  {
    id: 'youtube',
    name: 'YouTube (Google Video)',
    category: 'media',
    domain: 'youtube.com',
    testUrl: 'https://www.youtube.com/generate_204',
    isCF: false,
  },
  {
    id: 'baidu',
    name: '百度 (国内直连对照)',
    category: 'direct',
    domain: 'baidu.com',
    testUrl: 'https://www.baidu.com/favicon.ico',
    isCF: false,
  },
];

const IATA_CITIES: Record<string, [string, string]> = {
  SJC: ['San Jose', 'US'],
  LAX: ['Los Angeles', 'US'],
  SFO: ['San Francisco', 'US'],
  SEA: ['Seattle', 'US'],
  ORD: ['Chicago', 'US'],
  DFW: ['Dallas', 'US'],
  EWR: ['Newark', 'US'],
  JFK: ['New York', 'US'],
  IAD: ['Washington', 'US'],
  NRT: ['Tokyo', 'JP'],
  HND: ['Tokyo', 'JP'],
  KIX: ['Osaka', 'JP'],
  HKG: ['Hong Kong', 'HK'],
  TPE: ['Taipei', 'TW'],
  SIN: ['Singapore', 'SG'],
  ICN: ['Seoul', 'KR'],
  LHR: ['London', 'GB'],
  FRA: ['Frankfurt', 'DE'],
  AMS: ['Amsterdam', 'NL'],
  CDG: ['Paris', 'FR'],
  SYD: ['Sydney', 'AU'],
};

export const ClientLiveSplitVerifier: React.FC = () => {
  const [items, setItems] = useState<ClientSplitItem[]>(() =>
    DEFAULT_TARGETS.map((t) => ({ ...t, status: 'idle', latencyMs: 0 }))
  );
  const [isRunning, setIsRunning] = useState(false);
  const [lastTestedAt, setLastTestedAt] = useState<string | null>(null);
  const [copiedIp, setCopiedIp] = useState<string | null>(null);

  const fetchGeoInfo = async (ip: string, colo?: string) => {
    if (!ip) {
      if (colo && IATA_CITIES[colo.toUpperCase()]) {
        const m = IATA_CITIES[colo.toUpperCase()];
        return { city: m[0], countryCode: m[1], isp: 'Cloudflare Edge', org: 'Cloudflare' };
      }
      return null;
    }
    // 1. ip.sb
    try {
      const resp = await fetch(`https://api.ip.sb/geoip/${ip}`, { signal: AbortSignal.timeout(3500) });
      if (resp.ok) {
        const d = await resp.json();
        return {
          city: d.city || '',
          country: d.country || '',
          countryCode: d.country_code || '',
          isp: d.isp || '',
          org: d.organization || '',
        };
      }
    } catch {}

    // 2. ipwho.is fallback
    try {
      const resp = await fetch(`https://ipwho.is/${ip}`, { signal: AbortSignal.timeout(3500) });
      if (resp.ok) {
        const d = await resp.json();
        if (d.success) {
          return {
            city: d.city || '',
            country: d.country || '',
            countryCode: d.country_code || '',
            isp: d.connection?.isp || '',
            org: d.connection?.org || '',
          };
        }
      }
    } catch {}

    // 3. Colo fallback
    if (colo && IATA_CITIES[colo.toUpperCase()]) {
      const m = IATA_CITIES[colo.toUpperCase()];
      return { city: m[0], countryCode: m[1], isp: 'Cloudflare Edge', org: 'Cloudflare' };
    }

    return null;
  };

  const handleTestOne = async (index: number) => {
    const target = items[index];
    setItems((prev) =>
      prev.map((it, i) => (i === index ? { ...it, status: 'testing', detail: '探测中...' } : it))
    );

    const start = performance.now();
    try {
      if (target.isCF) {
        const resp = await fetch(target.testUrl, {
          method: 'GET',
          cache: 'no-store',
          signal: AbortSignal.timeout(8000),
        });
        const duration = Math.round(performance.now() - start);

        if (!resp.ok) {
          setItems((prev) =>
            prev.map((it, i) =>
              i === index
                ? {
                    ...it,
                    status: resp.status === 403 ? 'blocked' : 'error',
                    latencyMs: duration,
                    detail: resp.status === 403 ? '403 Forbidden (可能被节点拦截或限制)' : `HTTP ${resp.status}`,
                  }
                : it
            )
          );
          return;
        }

        const text = await resp.text();
        let ip = '';
        let loc = '';
        let colo = '';
        for (const line of text.split('\n')) {
          const t = line.trim();
          if (t.startsWith('ip=')) ip = t.slice(3);
          else if (t.startsWith('loc=')) loc = t.slice(4);
          else if (t.startsWith('colo=')) colo = t.slice(5);
        }

        let geo = await fetchGeoInfo(ip, colo);
        const resolvedIPType = determineIPType(ip, geo?.isp, geo?.org);

        setItems((prev) =>
          prev.map((it, i) =>
            i === index
              ? {
                  ...it,
                  status: 'ok',
                  latencyMs: duration,
                  exitIp: ip,
                  city: geo?.city || (colo ? IATA_CITIES[colo]?.[0] : undefined),
                  country: geo?.country,
                  countryCode: loc || geo?.countryCode,
                  isp: geo?.isp,
                  ipType: resolvedIPType,
                  detail: `Trace 通畅 (${duration}ms)`,
                }
              : it
          )
        );
      } else {
        // Non-CF targets: Use no-cors or image check for latency, query client public IP
        await fetch(target.testUrl, {
          method: 'HEAD',
          mode: 'no-cors',
          cache: 'no-store',
          signal: AbortSignal.timeout(8000),
        }).catch(async () => {
          return fetch(target.testUrl, {
            method: 'GET',
            mode: 'no-cors',
            cache: 'no-store',
            signal: AbortSignal.timeout(8000),
          });
        });

        const duration = Math.round(performance.now() - start);

        // Fetch egress IP via ip.sb if Google/direct
        let ip = '';
        let geo: any = null;
        try {
          const ipResp = await fetch('https://api.ip.sb/geoip', { signal: AbortSignal.timeout(4000) });
          if (ipResp.ok) {
            geo = await ipResp.json();
            ip = geo.ip;
          }
        } catch {}

        const resolvedIPType = determineIPType(ip || target.exitIp, geo?.isp || target.isp, geo?.organization);

        setItems((prev) =>
          prev.map((it, i) =>
            i === index
              ? {
                  ...it,
                  status: 'ok',
                  latencyMs: duration,
                  exitIp: ip || it.exitIp,
                  city: geo?.city || it.city,
                  country: geo?.country || it.country,
                  countryCode: geo?.country_code || it.countryCode,
                  isp: geo?.isp || it.isp,
                  ipType: resolvedIPType,
                  detail: `连通就绪 (${duration}ms)`,
                }
              : it
          )
        );
      }
    } catch (err: any) {
      const duration = Math.round(performance.now() - start);
      setItems((prev) =>
        prev.map((it, i) =>
          i === index
            ? {
                ...it,
                status: 'error',
                latencyMs: duration,
                detail: err.message || '请求超时或被阻断',
              }
            : it
        )
      );
    }
  };

  const handleRunAll = useCallback(async () => {
    setIsRunning(true);
    for (let i = 0; i < items.length; i++) {
      await handleTestOne(i);
    }
    setLastTestedAt(new Date().toLocaleTimeString());
    setIsRunning(false);
  }, [items]);

  const handleCopy = (ip?: string) => {
    if (!ip) return;
    navigator.clipboard.writeText(ip);
    setCopiedIp(ip);
    setTimeout(() => setCopiedIp(null), 2000);
  };

  // Stats summary
  const okCount = items.filter((it) => it.status === 'ok').length;
  const blockedCount = items.filter((it) => it.status === 'blocked').length;
  const errorCount = items.filter((it) => it.status === 'error').length;
  const uniqueIps = Array.from(new Set(items.map((it) => it.exitIp).filter(Boolean)));

  return (
    <div className="space-y-6">
      {/* 1. Header Banner */}
      <div className="bg-gradient-to-r from-slate-900 via-indigo-950 to-slate-900 rounded-2xl p-6 text-white shadow-md relative overflow-hidden">
        <div className="absolute right-0 top-0 p-8 opacity-10 pointer-events-none">
          <Globe className="w-48 h-48" />
        </div>
        <div className="relative z-10 space-y-4">
          <div className="flex flex-wrap items-center justify-between gap-4">
            <div>
              <div className="flex items-center space-x-2">
                <span className="px-2.5 py-0.5 rounded-full text-2xs font-bold uppercase tracking-wider bg-blue-500/20 text-blue-300 border border-blue-400/30">
                  Client-Side Live Split Lab
                </span>
                <span className="text-2xs text-slate-400 font-mono">对标 Net.Coffee 实时架构</span>
              </div>
              <h2 className="text-xl font-extrabold tracking-tight text-white mt-1.5 flex items-center">
                <Radio className="w-5 h-5 mr-2 text-indigo-400 animate-pulse" />
                本机客户端实时分流验证台
              </h2>
              <p className="text-xs text-slate-300 max-w-2xl mt-1 leading-relaxed">
                由您的浏览器直接发起探测，真实检验本地代理软件（Clash / Sing-box / Surge）的分流策略是否准确将流量分配至对应落地节点、归属城市与【家宽】/【机房】属性。
              </p>
            </div>

            <button
              onClick={handleRunAll}
              disabled={isRunning}
              className="inline-flex items-center px-5 py-2.5 rounded-xl font-bold text-xs bg-blue-600 hover:bg-blue-500 active:bg-blue-700 text-white shadow-sm transition disabled:opacity-50"
            >
              <Play className={`w-4 h-4 mr-2 ${isRunning ? 'animate-spin' : ''}`} />
              {isRunning ? '正在分流测试中...' : '开始全景分流测验'}
            </button>
          </div>

          {/* Metric Stats Chips */}
          <div className="pt-3 border-t border-white/10 flex flex-wrap items-center gap-4 text-xs font-mono">
            <div className="flex items-center space-x-2 bg-white/5 px-3 py-1.5 rounded-lg border border-white/10">
              <span className="text-slate-400">检测目标:</span>
              <span className="font-bold text-white">{items.length} 个核心业务</span>
            </div>
            <div className="flex items-center space-x-2 bg-white/5 px-3 py-1.5 rounded-lg border border-white/10">
              <span className="text-slate-400">通畅:</span>
              <span className="font-bold text-emerald-400">[通过] {okCount} 个</span>
            </div>
            {blockedCount > 0 && (
              <div className="flex items-center space-x-2 bg-amber-500/10 px-3 py-1.5 rounded-lg border border-amber-500/30">
                <span className="text-amber-300">受限:</span>
                <span className="font-bold text-amber-400">[受限] {blockedCount} 个 (403)</span>
              </div>
            )}
            {errorCount > 0 && (
              <div className="flex items-center space-x-2 bg-red-500/10 px-3 py-1.5 rounded-lg border border-red-500/30">
                <span className="text-red-300">超时:</span>
                <span className="font-bold text-red-400">[不可达] {errorCount} 个</span>
              </div>
            )}
            <div className="flex items-center space-x-2 bg-white/5 px-3 py-1.5 rounded-lg border border-white/10">
              <span className="text-slate-400">分流出口数:</span>
              <span className="font-bold text-indigo-300">{uniqueIps.length} 个独立 IP</span>
            </div>
            {lastTestedAt && (
              <span className="text-2xs text-slate-400 ml-auto">
                上次测验: {lastTestedAt}
              </span>
            )}
          </div>
        </div>
      </div>

      {/* 2. Split Targets Table */}
      <div className="bg-white border border-slate-200 rounded-xl shadow-2xs overflow-hidden">
        <div className="p-4 border-b border-slate-100 flex items-center justify-between">
          <div className="flex items-center space-x-2">
            <Layers className="w-4 h-4 text-blue-600" />
            <h3 className="text-xs font-bold text-slate-900 uppercase tracking-wider">
              核心业务分流明细表 (Core AI & Media Egress Split)
            </h3>
          </div>
          <span className="text-2xs text-slate-500 font-mono">
            标准工业版 · 完整展示落地 IP、归属城市与 [家宽] / [机房] 属性
          </span>
        </div>

        <div className="overflow-x-auto">
          <table className="min-w-full divide-y divide-slate-200 text-left text-xs">
            <thead className="bg-slate-50 text-slate-500 uppercase font-semibold text-2xs tracking-wider">
              <tr>
                <th scope="col" className="px-5 py-3">服务与测试目标</th>
                <th scope="col" className="px-5 py-3">连通状态</th>
                <th scope="col" className="px-5 py-3">延迟 (RTT)</th>
                <th scope="col" className="px-5 py-3">落地具体 IP</th>
                <th scope="col" className="px-5 py-3">IP 属性</th>
                <th scope="col" className="px-5 py-3">落地城市与归属地</th>
                <th scope="col" className="px-5 py-3">运营商 / 托管机构</th>
                <th scope="col" className="px-5 py-3 text-right">操作</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-100 bg-white">
              {items.map((item, idx) => (
                <tr key={item.id} className="hover:bg-slate-50/80 transition">
                  {/* Service Name */}
                  <td className="px-5 py-3.5">
                    <div className="flex items-center space-x-2.5">
                      <div className="w-7 h-7 rounded-lg bg-slate-100 border border-slate-200 flex items-center justify-center font-bold text-2xs text-slate-700">
                        {item.name.slice(0, 2)}
                      </div>
                      <div>
                        <div className="font-bold text-slate-900">{item.name}</div>
                        <div className="text-2xs text-slate-400 font-mono truncate max-w-xs">{item.domain}</div>
                      </div>
                    </div>
                  </td>

                  {/* Status */}
                  <td className="px-5 py-3.5 whitespace-nowrap">
                    {item.status === 'testing' ? (
                      <span className="inline-flex items-center text-2xs font-semibold px-2 py-0.5 rounded bg-blue-50 text-blue-700 border border-blue-200 animate-pulse">
                        <RotateCw className="w-3 h-3 mr-1 animate-spin" />
                        [探测中...]
                      </span>
                    ) : item.status === 'ok' ? (
                      <span className="inline-flex items-center text-2xs font-semibold px-2 py-0.5 rounded bg-emerald-50 text-emerald-800 border border-emerald-200">
                        <CheckCircle2 className="w-3.5 h-3.5 mr-1 text-emerald-600" />
                        [通过]
                      </span>
                    ) : item.status === 'blocked' ? (
                      <span className="inline-flex items-center text-2xs font-semibold px-2 py-0.5 rounded bg-amber-50 text-amber-800 border border-amber-200">
                        <AlertTriangle className="w-3.5 h-3.5 mr-1 text-amber-600" />
                        [受限] 403
                      </span>
                    ) : item.status === 'error' ? (
                      <span className="inline-flex items-center text-2xs font-semibold px-2 py-0.5 rounded bg-red-50 text-red-800 border border-red-200">
                        <XCircle className="w-3.5 h-3.5 mr-1 text-red-600" />
                        [不可达]
                      </span>
                    ) : (
                      <span className="text-2xs text-slate-400">[未测试]</span>
                    )}
                  </td>

                  {/* Latency */}
                  <td className="px-5 py-3.5 whitespace-nowrap font-mono">
                    {item.latencyMs > 0 ? (
                      <span className={`font-bold ${item.latencyMs < 200 ? 'text-emerald-600' : item.latencyMs < 500 ? 'text-amber-600' : 'text-slate-600'}`}>
                        {item.latencyMs} ms
                      </span>
                    ) : (
                      <span className="text-slate-400">—</span>
                    )}
                  </td>

                  {/* Egress IP */}
                  <td className="px-5 py-3.5 whitespace-nowrap font-mono">
                    {item.exitIp ? (
                      <div className="flex items-center space-x-1.5">
                        <span className="font-bold text-slate-800 text-xs select-all">
                          {item.exitIp}
                        </span>
                        <button
                          onClick={() => handleCopy(item.exitIp)}
                          className="p-1 rounded hover:bg-slate-100 text-slate-400 hover:text-slate-700 transition"
                          title="复制 IP"
                        >
                          {copiedIp === item.exitIp ? (
                            <Check className="w-3 h-3 text-emerald-600" />
                          ) : (
                            <Copy className="w-3 h-3" />
                          )}
                        </button>
                      </div>
                    ) : (
                      <span className="text-slate-400 text-2xs">—</span>
                    )}
                  </td>

                  {/* IP Type */}
                  <td className="px-5 py-3.5 whitespace-nowrap">
                    {item.exitIp ? (
                      <span className={`px-2 py-0.5 rounded text-2xs font-mono font-bold ${
                        item.ipType === '家宽'
                          ? 'bg-purple-100 text-purple-700 border border-purple-200'
                          : 'bg-blue-100 text-blue-700 border border-blue-200'
                      }`}>
                        [{item.ipType || '机房'}]
                      </span>
                    ) : (
                      <span className="text-slate-400 text-2xs">—</span>
                    )}
                  </td>

                  {/* City & Country */}
                  <td className="px-5 py-3.5 whitespace-nowrap">
                    {item.city || item.countryCode ? (
                      <div className="flex items-center space-x-1.5 font-mono text-xs">
                        <span className="px-1.5 py-0.5 rounded bg-slate-100 text-slate-700 text-2xs font-semibold">
                          [{item.countryCode || 'GL'}]
                        </span>
                        <span className="font-semibold text-slate-800">
                          {item.city ? `${item.city}, ` : ''}{item.countryCode || item.country}
                        </span>
                      </div>
                    ) : (
                      <span className="text-slate-400 text-2xs">—</span>
                    )}
                  </td>

                  {/* ISP */}
                  <td className="px-5 py-3.5 whitespace-nowrap text-2xs text-slate-600 font-mono">
                    {item.isp || '—'}
                  </td>

                  {/* Single Run Action */}
                  <td className="px-5 py-3.5 whitespace-nowrap text-right">
                    <button
                      onClick={() => handleTestOne(idx)}
                      disabled={item.status === 'testing'}
                      className="px-2.5 py-1 text-2xs font-semibold rounded-lg bg-slate-100 hover:bg-slate-200 text-slate-700 transition disabled:opacity-50"
                    >
                      单测
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
};
