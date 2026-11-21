import React, { useState, useMemo } from 'react';
import { 
  Server, 
  CheckCircle2, 
  XCircle, 
  AlertTriangle, 
  Globe, 
  Zap, 
  Filter, 
  ArrowUpDown, 
  FileText,
  Search,
  Check,
  RotateCw
} from 'lucide-react';
import { MonitorStatusMap, NodeProbeResult } from '../types';

interface MonitorDashboardProps {
  statusMap: MonitorStatusMap;
  onOpenReport: (node: NodeProbeResult) => void;
  onRetestNode: (node: NodeProbeResult) => Promise<void>;
  onSpeedTestNode: (node: NodeProbeResult) => Promise<void>;
  onOpenHealingWizard: (nodeName: string) => void;
  isInspecting: boolean;
}

export const MonitorDashboard: React.FC<MonitorDashboardProps> = ({
  statusMap,
  onOpenReport,
  onRetestNode,
  onSpeedTestNode,
  onOpenHealingWizard,
  isInspecting,
}) => {
  const [filterType, setFilterType] = useState<'all' | 'online' | 'offline' | 'china' | 'clean' | 'blocked'>('all');
  const [searchKeyword, setSearchKeyword] = useState('');
  const [sortBy, setSortBy] = useState<'latency' | 'name' | 'speed'>('latency');

  const nodes = useMemo(() => {
    return Object.entries(statusMap).map(([key, item]) => {
      if (item.last_result) {
        return item.last_result;
      }
      return {
        id: key,
        name: item.config.name,
        protocol: 'unknown',
        online: false,
        latency_ms: 0,
        china_exit: false,
        exit_country: '??',
        category: '未知',
        original_url: item.config.url,
        is_self: item.config.is_self,
      } as NodeProbeResult;
    });
  }, [statusMap]);

  // Metric counts
  const stats = useMemo(() => {
    let total = nodes.length;
    let online = 0;
    let offline = 0;
    let china = 0;
    let clean = 0;
    let blocked = 0;

    nodes.forEach((n) => {
      if (n.online) online++;
      else offline++;

      if (n.online && n.china_exit) china++;
      if (n.online && !n.china_exit) clean++;

      if (n.diagnostic?.fault_type === 'PORT_BLOCKED') blocked++;
    });

    return { total, online, offline, china, clean, blocked };
  }, [nodes]);

  // Filtered & Sorted nodes
  const displayNodes = useMemo(() => {
    return nodes
      .filter((n) => {
        if (filterType === 'online') return n.online;
        if (filterType === 'offline') return !n.online;
        if (filterType === 'china') return n.online && n.china_exit;
        if (filterType === 'clean') return n.online && !n.china_exit;
        if (filterType === 'blocked') return n.diagnostic?.fault_type === 'PORT_BLOCKED';
        return true;
      })
      .filter((n) => {
        if (!searchKeyword.trim()) return true;
        const kw = searchKeyword.toLowerCase();
        return (
          n.name.toLowerCase().includes(kw) ||
          n.protocol.toLowerCase().includes(kw) ||
          n.exit_country.toLowerCase().includes(kw) ||
          (n.original_url && n.original_url.toLowerCase().includes(kw))
        );
      })
      .sort((a, b) => {
        if (sortBy === 'latency') {
          if (!a.online) return 1;
          if (!b.online) return -1;
          return a.latency_ms - b.latency_ms;
        }
        if (sortBy === 'speed') {
          const spdA = a.speed_result?.bandwidth_mb_s || 0;
          const spdB = b.speed_result?.bandwidth_mb_s || 0;
          return spdB - spdA;
        }
        return a.name.localeCompare(b.name);
      });
  }, [nodes, filterType, searchKeyword, sortBy]);

  return (
    <div className="space-y-6">
      {/* 1. Overview Metric Cards */}
      <div className="grid grid-cols-2 sm:grid-cols-3 lg:grid-cols-6 gap-3">
        {/* Total Nodes */}
        <div 
          onClick={() => setFilterType('all')}
          className={`p-4 bg-white border rounded-xl shadow-2xs cursor-pointer transition ${
            filterType === 'all' ? 'border-blue-500 ring-1 ring-blue-500' : 'border-slate-200 hover:border-slate-300'
          }`}
        >
          <div className="text-2xs font-semibold text-slate-500 uppercase tracking-wider">全部节点</div>
          <div className="mt-2 text-2xl font-bold font-mono text-slate-900">{stats.total}</div>
          <div className="text-2xs text-slate-400 mt-0.5">监控全集</div>
        </div>

        {/* Online Nodes */}
        <div 
          onClick={() => setFilterType('online')}
          className={`p-4 bg-white border rounded-xl shadow-2xs cursor-pointer transition ${
            filterType === 'online' ? 'border-emerald-500 ring-1 ring-emerald-500' : 'border-slate-200 hover:border-slate-300'
          }`}
        >
          <div className="text-2xs font-semibold text-emerald-600 uppercase tracking-wider flex items-center">
            <CheckCircle2 className="w-3.5 h-3.5 mr-1" />
            在线健康
          </div>
          <div className="mt-2 text-2xl font-bold font-mono text-emerald-700">{stats.online}</div>
          <div className="text-2xs text-slate-400 mt-0.5">连通率 {stats.total ? Math.round((stats.online / stats.total) * 100) : 0}%</div>
        </div>

        {/* Offline Nodes */}
        <div 
          onClick={() => setFilterType('offline')}
          className={`p-4 bg-white border rounded-xl shadow-2xs cursor-pointer transition ${
            filterType === 'offline' ? 'border-red-500 ring-1 ring-red-500' : 'border-slate-200 hover:border-slate-300'
          }`}
        >
          <div className="text-2xs font-semibold text-red-600 uppercase tracking-wider flex items-center">
            <XCircle className="w-3.5 h-3.5 mr-1" />
            离线停摆
          </div>
          <div className="mt-2 text-2xl font-bold font-mono text-red-700">{stats.offline}</div>
          <div className="text-2xs text-slate-400 mt-0.5">握手超时</div>
        </div>

        {/* Clean Exit */}
        <div 
          onClick={() => setFilterType('clean')}
          className={`p-4 bg-white border rounded-xl shadow-2xs cursor-pointer transition ${
            filterType === 'clean' ? 'border-emerald-500 ring-1 ring-emerald-500' : 'border-slate-200 hover:border-slate-300'
          }`}
        >
          <div className="text-2xs font-semibold text-emerald-700 uppercase tracking-wider flex items-center">
            <Globe className="w-3.5 h-3.5 mr-1" />
            纯净出口
          </div>
          <div className="mt-2 text-2xl font-bold font-mono text-emerald-800">{stats.clean}</div>
          <div className="text-2xs text-slate-400 mt-0.5">Google NCR 纯净</div>
        </div>

        {/* China Exit */}
        <div 
          onClick={() => setFilterType('china')}
          className={`p-4 bg-white border rounded-xl shadow-2xs cursor-pointer transition ${
            filterType === 'china' ? 'border-amber-500 ring-1 ring-amber-500' : 'border-slate-200 hover:border-slate-300'
          }`}
        >
          <div className="text-2xs font-semibold text-amber-600 uppercase tracking-wider flex items-center">
            <span>🇨🇳</span>
            <span className="ml-1">送中路由</span>
          </div>
          <div className="mt-2 text-2xl font-bold font-mono text-amber-700">{stats.china}</div>
          <div className="text-2xs text-slate-400 mt-0.5">重定向至 CN</div>
        </div>

        {/* Blocked Ports */}
        <div 
          onClick={() => setFilterType('blocked')}
          className={`p-4 bg-white border rounded-xl shadow-2xs cursor-pointer transition ${
            filterType === 'blocked' ? 'border-red-500 ring-1 ring-red-500' : 'border-slate-200 hover:border-slate-300'
          }`}
        >
          <div className="text-2xs font-semibold text-red-600 uppercase tracking-wider flex items-center">
            <AlertTriangle className="w-3.5 h-3.5 mr-1" />
            端口阻断
          </div>
          <div className="mt-2 text-2xl font-bold font-mono text-red-700">{stats.blocked}</div>
          <div className="text-2xs text-slate-400 mt-0.5">可一键自愈换端口</div>
        </div>
      </div>

      {/* 2. Controls & Search Toolbar */}
      <div className="bg-white border border-slate-200 rounded-xl p-3 shadow-2xs flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
        {/* Search Input */}
        <div className="relative flex-1 max-w-sm">
          <div className="absolute inset-y-0 left-0 pl-3 flex items-center pointer-events-none text-slate-400">
            <Search className="w-4 h-4" />
          </div>
          <input
            type="text"
            value={searchKeyword}
            onChange={(e) => setSearchKeyword(e.target.value)}
            placeholder="搜索节点名称 / 协议 / 国家..."
            className="block w-full pl-9 pr-3 py-1.5 border border-slate-300 rounded-lg text-xs bg-slate-50 focus:bg-white focus:outline-none focus:ring-1 focus:ring-blue-600 focus:border-blue-600"
          />
        </div>

        {/* Filter Pills & Sort Selector */}
        <div className="flex flex-wrap items-center gap-2">
          {/* Quick Filter */}
          <div className="flex items-center space-x-1 bg-slate-100 p-1 rounded-lg text-xs">
            <span className="text-2xs text-slate-500 px-1">过滤:</span>
            {[
              { id: 'all', label: '全部' },
              { id: 'online', label: '在线' },
              { id: 'offline', label: '离线' },
              { id: 'clean', label: '纯净' },
              { id: 'china', label: '送中' },
              { id: 'blocked', label: '被墙' },
            ].map((f) => (
              <button
                key={f.id}
                onClick={() => setFilterType(f.id as any)}
                className={`px-2 py-0.5 rounded text-xs transition ${
                  filterType === f.id
                    ? 'bg-white font-medium text-slate-900 shadow-2xs'
                    : 'text-slate-600 hover:text-slate-900'
                }`}
              >
                {f.label}
              </button>
            ))}
          </div>

          {/* Sort Selector */}
          <div className="flex items-center space-x-1 border border-slate-200 rounded-lg px-2 py-1 text-xs text-slate-600 bg-white">
            <ArrowUpDown className="w-3.5 h-3.5 text-slate-400" />
            <select
              value={sortBy}
              onChange={(e) => setSortBy(e.target.value as any)}
              className="bg-transparent border-none text-xs focus:outline-none pr-2 cursor-pointer"
            >
              <option value="latency">按延迟从低到高</option>
              <option value="speed">按 30M 吞吐带宽排序</option>
              <option value="name">按节点名称字母排序</option>
            </select>
          </div>
        </div>
      </div>

      {/* 3. Node Table / Grid */}
      <div className="bg-white border border-slate-200 rounded-xl shadow-2xs overflow-hidden">
        <div className="overflow-x-auto">
          <table className="min-w-full divide-y divide-slate-200 text-left text-xs">
            <thead className="bg-slate-50 text-slate-500 uppercase tracking-wider font-semibold">
              <tr>
                <th scope="col" className="px-4 py-3">节点名称 / 协议</th>
                <th scope="col" className="px-4 py-3">地区 / 出口</th>
                <th scope="col" className="px-4 py-3">连通与 RTT 延迟</th>
                <th scope="col" className="px-4 py-3">送中研判</th>
                <th scope="col" className="px-4 py-3">自研 30M 吞吐</th>
                <th scope="col" className="px-4 py-3">业务分流 (ChatGPT/Claude/YT/百度)</th>
                <th scope="col" className="px-4 py-3 text-right">操作动作</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-200 bg-white font-sans">
              {displayNodes.length > 0 ? (
                displayNodes.map((node) => {
                  const isBlocked = node.diagnostic?.fault_type === 'PORT_BLOCKED';
                  return (
                    <tr 
                      key={node.id || node.name}
                      onClick={() => onOpenReport(node)}
                      className="hover:bg-slate-50/80 transition cursor-pointer group"
                    >
                      {/* Name & Protocol */}
                      <td className="px-4 py-3.5">
                        <div className="flex items-center space-x-2.5">
                          <span 
                            className={`w-2.5 h-2.5 rounded-full flex-shrink-0 ${
                              isBlocked 
                                ? 'bg-red-500 ring-4 ring-red-100' 
                                : node.online 
                                ? 'bg-emerald-500' 
                                : 'bg-red-500'
                            }`}
                          />
                          <div className="truncate max-w-xs">
                            <div className="font-semibold text-slate-900 group-hover:text-blue-600 transition truncate">
                              {node.name}
                            </div>
                            <div className="flex items-center space-x-1.5 mt-0.5">
                              <span className="font-mono text-2xs uppercase px-1.5 py-0.2 rounded bg-slate-100 text-slate-600 border border-slate-200">
                                {node.protocol}
                              </span>
                              {node.is_self && (
                                <span className="text-2xs px-1.5 py-0.2 rounded bg-blue-50 text-blue-700 border border-blue-200">
                                  自有
                                </span>
                              )}
                              {isBlocked && (
                                <span className="text-2xs font-semibold px-1.5 py-0.2 rounded bg-red-100 text-red-700">
                                  端口被墙
                                </span>
                              )}
                            </div>
                          </div>
                        </div>
                      </td>

                      {/* Region / Exit */}
                      <td className="px-4 py-3.5 whitespace-nowrap">
                        <span className="font-mono font-medium text-slate-700 bg-slate-100 px-2 py-1 rounded text-xs border border-slate-200">
                          {node.exit_country || 'Global'}
                        </span>
                      </td>

                      {/* Latency & Online Status */}
                      <td className="px-4 py-3.5 whitespace-nowrap font-mono">
                        {node.online ? (
                          <div className="flex items-center space-x-1.5">
                            <span 
                              className={`font-bold ${
                                node.latency_ms < 150
                                  ? 'text-emerald-600'
                                  : node.latency_ms < 300
                                  ? 'text-amber-600'
                                  : 'text-red-600'
                              }`}
                            >
                              {node.latency_ms} ms
                            </span>
                            <span className="text-2xs text-slate-400 font-sans">204 OK</span>
                          </div>
                        ) : (
                          <span className="text-red-600 font-semibold font-sans">
                            {isBlocked ? '国内阻断' : '握手超时'}
                          </span>
                        )}
                      </td>

                      {/* China Exit Status */}
                      <td className="px-4 py-3.5 whitespace-nowrap">
                        {node.online ? (
                          node.china_exit ? (
                            <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-amber-50 text-amber-800 border border-amber-200">
                              🇨🇳 送中路由
                            </span>
                          ) : (
                            <span className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-emerald-50 text-emerald-800 border border-emerald-200">
                              🌐 纯净出口
                            </span>
                          )
                        ) : (
                          <span className="text-slate-400 text-xs">-</span>
                        )}
                      </td>

                      {/* 30MB Speed Bandwidth */}
                      <td className="px-4 py-3.5 whitespace-nowrap font-mono">
                        {node.speed_result?.bandwidth_mb_s ? (
                          <div className="flex items-center space-x-1.5">
                            <span className="font-bold text-blue-700">
                              {node.speed_result.bandwidth_mb_s.toFixed(1)}
                            </span>
                            <span className="text-2xs text-slate-500">MB/s</span>
                          </div>
                        ) : (
                          <span className="text-slate-400 text-xs">未采样</span>
                        )}
                      </td>

                      {/* Split Pills */}
                      <td className="px-4 py-3.5 whitespace-nowrap">
                        <div className="flex items-center space-x-1 text-2xs">
                          {['chatgpt', 'claude', 'youtube', 'baidu'].map((srv) => {
                            const val = node.split?.[srv as keyof typeof node.split];
                            const isOk = val && (val.includes('ok') || val.includes('200') || !val.includes('fail'));
                            return (
                              <span
                                key={srv}
                                className={`px-1.5 py-0.5 rounded font-mono ${
                                  val
                                    ? isOk
                                      ? 'bg-emerald-50 text-emerald-700 border border-emerald-200'
                                      : 'bg-red-50 text-red-700 border border-red-200'
                                    : 'bg-slate-100 text-slate-400'
                                }`}
                                title={`${srv}: ${val || '未探测'}`}
                              >
                                {srv === 'chatgpt' ? 'GPT' : srv === 'claude' ? 'Claude' : srv === 'youtube' ? 'YT' : '百度'}
                              </span>
                            );
                          })}
                        </div>
                      </td>

                      {/* Actions */}
                      <td className="px-4 py-3.5 whitespace-nowrap text-right" onClick={(e) => e.stopPropagation()}>
                        <div className="flex items-center justify-end space-x-1">
                          {isBlocked && (
                            <button
                              onClick={() => onOpenHealingWizard(node.name)}
                              className="px-2 py-1 text-xs font-semibold rounded bg-red-600 hover:bg-red-700 text-white shadow-2xs transition"
                              title="一键换端口自愈"
                            >
                              换端口自愈
                            </button>
                          )}
                          <button
                            onClick={() => onRetestNode(node)}
                            className="p-1.5 text-slate-500 hover:text-slate-900 hover:bg-slate-100 rounded-lg transition"
                            title="重新探测此节点"
                          >
                            <RotateCw className="w-3.5 h-3.5" />
                          </button>
                          <button
                            onClick={() => onOpenReport(node)}
                            className="inline-flex items-center px-2 py-1 text-xs font-medium rounded text-slate-700 bg-slate-100 hover:bg-slate-200 transition"
                          >
                            <FileText className="w-3.5 h-3.5 mr-1 text-slate-500" />
                            独立报告页
                          </button>
                        </div>
                      </td>
                    </tr>
                  );
                })
              ) : (
                <tr>
                  <td colSpan={7} className="px-4 py-12 text-center text-slate-400">
                    <Server className="w-8 h-8 mx-auto mb-2 text-slate-300" />
                    未找到匹配的节点数据
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
};
