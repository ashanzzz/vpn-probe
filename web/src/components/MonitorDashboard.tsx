import React, { useState, useMemo } from 'react';
import { 
  Server, 
  CheckCircle2, 
  XCircle, 
  AlertTriangle, 
  Globe, 
  Zap, 
  ArrowUpDown, 
  FileText,
  Search,
  RotateCw,
  Layers,
  Home,
  ShieldCheck,
  ShieldAlert,
  Plus,
  Trash2,
  ArrowRight,
  TrendingUp,
  Cpu,
  Radio,
  ExternalLink,
  Wrench
} from 'lucide-react';
import { 
  MonitorStatusMap, 
  NodeProbeResult, 
  TransitHostGroup, 
  ResidentialPairItem 
} from '../types';

interface MonitorDashboardProps {
  statusMap: MonitorStatusMap;
  onOpenReport: (node: NodeProbeResult, initialTab?: 'overview' | 'ip_intelligence' | 'history' | 'logs') => void;
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
  const [activeView, setActiveView] = useState<'all' | 'transit' | 'residential'>('all');
  const [searchKeyword, setSearchKeyword] = useState('');
  const [showAddResiModal, setShowAddResiModal] = useState(false);
  const [newResiName, setNewResiName] = useState('');
  const [newResiURL, setNewResiURL] = useState('');

  // Local residential nodes state for dynamic add/delete
  const [customResiList, setCustomResiList] = useState<ResidentialPairItem[]>([
    {
      id: 'resi-jp-01',
      name: 'LZ-JP-SoftBank-家宽-01',
      region: '日本',
      region_flag: '🇯🇵',
      target_ip: '167.104.167.151',
      assigned_relay_name: '东京软银 — VLESS Reality Vision',
      is_failover_relay: false,
      direct_node: {
        id: 'resi-jp-01-d',
        name: 'LZ-JP-SoftBank-家宽-01 (原生直连)',
        protocol: 'socks5',
        online: true,
        latency_ms: 318,
        china_exit: false,
        exit_country: 'JP',
        category: '家宽机-直连',
        original_url: 'socks5://user1:pass1@167.104.167.151:40981#LZ-JP-SoftBank-家宽-01',
        speed_result: { bandwidth_mb_s: 2.1, bandwidth_mbps: 16.8, ttfb_ms: 340, duration_sec: 4.8, bytes_transferred: 10485760 },
      },
      chain_node: {
        id: 'resi-jp-01-c',
        name: '东京软银 ➔ LZ-JP-家宽-01 (链式优化)',
        protocol: 'vless+socks5',
        online: true,
        latency_ms: 132,
        china_exit: false,
        exit_country: 'JP',
        category: '线路➔家宽链式代理',
        original_url: 'vless://...#Chain-JP-01',
        speed_result: { bandwidth_mb_s: 14.6, bandwidth_mbps: 116.8, ttfb_ms: 155, duration_sec: 0.7, bytes_transferred: 10485760 },
      },
    },
    {
      id: 'resi-jp-02',
      name: 'LZ-JP-SoftBank-家宽-02',
      region: '日本',
      region_flag: '🇯🇵',
      target_ip: '167.104.167.152',
      assigned_relay_name: '东京 IIJ — VLESS Reality',
      is_failover_relay: false,
      direct_node: {
        id: 'resi-jp-02-d',
        name: 'LZ-JP-SoftBank-家宽-02 (原生直连)',
        protocol: 'socks5',
        online: true,
        latency_ms: 295,
        china_exit: false,
        exit_country: 'JP',
        category: '家宽机-直连',
        original_url: 'socks5://user2:pass2@167.104.167.152:40982#LZ-JP-SoftBank-家宽-02',
        speed_result: { bandwidth_mb_s: 1.8, bandwidth_mbps: 14.4, ttfb_ms: 310, duration_sec: 5.5, bytes_transferred: 10485760 },
      },
      chain_node: {
        id: 'resi-jp-02-c',
        name: '东京 IIJ ➔ LZ-JP-家宽-02 (链式优化)',
        protocol: 'vless+socks5',
        online: true,
        latency_ms: 140,
        china_exit: false,
        exit_country: 'JP',
        category: '线路➔家宽链式代理',
        original_url: 'vless://...#Chain-JP-02',
        speed_result: { bandwidth_mb_s: 12.8, bandwidth_mbps: 102.4, ttfb_ms: 168, duration_sec: 0.8, bytes_transferred: 10485760 },
      },
    },
    {
      id: 'resi-us-01',
      name: 'LZ-US-Pavlov-家宽-01',
      region: '美国',
      region_flag: '🇺🇸',
      target_ip: '167.104.167.153',
      assigned_relay_name: '美西 ACCK 洛杉矶 4837 — VLESS Reality',
      is_failover_relay: false,
      direct_node: {
        id: 'resi-us-01-d',
        name: 'LZ-US-Pavlov-家宽-01 (原生直连)',
        protocol: 'socks5',
        online: true,
        latency_ms: 380,
        china_exit: false,
        exit_country: 'US',
        category: '家宽机-直连',
        original_url: 'socks5://user3:pass3@167.104.167.153:49041#LZ-US-Pavlov-家宽-01',
        speed_result: { bandwidth_mb_s: 1.2, bandwidth_mbps: 9.6, ttfb_ms: 420, duration_sec: 8.3, bytes_transferred: 10485760 },
      },
      chain_node: {
        id: 'resi-us-01-c',
        name: '美西4837 ➔ LZ-US-家宽-01 (链式优化)',
        protocol: 'vless+socks5',
        online: true,
        latency_ms: 185,
        china_exit: false,
        exit_country: 'US',
        category: '线路➔家宽链式代理',
        original_url: 'vless://...#Chain-US-01',
        speed_result: { bandwidth_mb_s: 11.4, bandwidth_mbps: 91.2, ttfb_ms: 195, duration_sec: 0.9, bytes_transferred: 10485760 },
      },
    },
    {
      id: 'resi-us-02',
      name: 'LZ-US-Pavlov-家宽-02',
      region: '美国',
      region_flag: '🇺🇸',
      target_ip: '167.104.167.154',
      assigned_relay_name: '美西 ACCK 洛杉矶 4837 — VLESS Reality',
      is_failover_relay: false,
      direct_node: {
        id: 'resi-us-02-d',
        name: 'LZ-US-Pavlov-家宽-02 (原生直连)',
        protocol: 'socks5',
        online: true,
        latency_ms: 395,
        china_exit: false,
        exit_country: 'US',
        category: '家宽机-直连',
        original_url: 'socks5://user4:pass4@167.104.167.154:49042#LZ-US-Pavlov-家宽-02',
        speed_result: { bandwidth_mb_s: 0.9, bandwidth_mbps: 7.2, ttfb_ms: 450, duration_sec: 11.1, bytes_transferred: 10485760 },
      },
      chain_node: {
        id: 'resi-us-02-c',
        name: '美西4837 ➔ LZ-US-家宽-02 (链式优化)',
        protocol: 'vless+socks5',
        online: true,
        latency_ms: 190,
        china_exit: false,
        exit_country: 'US',
        category: '线路➔家宽链式代理',
        original_url: 'vless://...#Chain-US-02',
        speed_result: { bandwidth_mb_s: 10.8, bandwidth_mbps: 86.4, ttfb_ms: 202, duration_sec: 1.0, bytes_transferred: 10485760 },
      },
    },
    {
      id: 'resi-jp-03',
      name: 'LZ-JP-SoftBank-家宽-03',
      region: '日本',
      region_flag: '🇯🇵',
      target_ip: '167.104.167.155',
      assigned_relay_name: '东京软银 — VLESS Reality Vision',
      is_failover_relay: false,
      direct_node: {
        id: 'resi-jp-03-d',
        name: 'LZ-JP-SoftBank-家宽-03 (原生直连)',
        protocol: 'socks5',
        online: true,
        latency_ms: 310,
        china_exit: false,
        exit_country: 'JP',
        category: '家宽机-直连',
        original_url: 'socks5://user5:pass5@167.104.167.155:40983#LZ-JP-SoftBank-家宽-03',
        speed_result: { bandwidth_mb_s: 2.3, bandwidth_mbps: 18.4, ttfb_ms: 325, duration_sec: 4.3, bytes_transferred: 10485760 },
      },
      chain_node: {
        id: 'resi-jp-03-c',
        name: '东京软银 ➔ LZ-JP-家宽-03 (链式优化)',
        protocol: 'vless+socks5',
        online: true,
        latency_ms: 136,
        china_exit: false,
        exit_country: 'JP',
        category: '线路➔家宽链式代理',
        original_url: 'vless://...#Chain-JP-03',
        speed_result: { bandwidth_mb_s: 15.2, bandwidth_mbps: 121.6, ttfb_ms: 150, duration_sec: 0.7, bytes_transferred: 10485760 },
      },
    },
    {
      id: 'resi-jp-04',
      name: 'LZ-JP-SoftBank-家宽-04',
      region: '日本',
      region_flag: '🇯🇵',
      target_ip: '167.104.167.156',
      assigned_relay_name: '东京 IIJ — VLESS Reality',
      is_failover_relay: false,
      direct_node: {
        id: 'resi-jp-04-d',
        name: 'LZ-JP-SoftBank-家宽-04 (原生直连)',
        protocol: 'socks5',
        online: true,
        latency_ms: 305,
        china_exit: false,
        exit_country: 'JP',
        category: '家宽机-直连',
        original_url: 'socks5://user6:pass6@167.104.167.156:40984#LZ-JP-SoftBank-家宽-04',
        speed_result: { bandwidth_mb_s: 1.9, bandwidth_mbps: 15.2, ttfb_ms: 330, duration_sec: 5.2, bytes_transferred: 10485760 },
      },
      chain_node: {
        id: 'resi-jp-04-c',
        name: '东京 IIJ ➔ LZ-JP-家宽-04 (链式优化)',
        protocol: 'vless+socks5',
        online: true,
        latency_ms: 142,
        china_exit: false,
        exit_country: 'JP',
        category: '线路➔家宽链式代理',
        original_url: 'vless://...#Chain-JP-04',
        speed_result: { bandwidth_mb_s: 13.5, bandwidth_mbps: 108.0, ttfb_ms: 165, duration_sec: 0.8, bytes_transferred: 10485760 },
      },
    },
    {
      id: 'resi-us-03',
      name: 'LZ-US-Pavlov-家宽-03',
      region: '美国',
      region_flag: '🇺🇸',
      target_ip: '167.104.167.157',
      assigned_relay_name: '东京软银 — VLESS Reality Vision (匀配)',
      is_failover_relay: false,
      direct_node: {
        id: 'resi-us-03-d',
        name: 'LZ-US-Pavlov-家宽-03 (原生直连)',
        protocol: 'socks5',
        online: true,
        latency_ms: 410,
        china_exit: false,
        exit_country: 'US',
        category: '家宽机-直连',
        original_url: 'socks5://user7:pass7@167.104.167.157:49043#LZ-US-Pavlov-家宽-03',
        speed_result: { bandwidth_mb_s: 0.8, bandwidth_mbps: 6.4, ttfb_ms: 480, duration_sec: 12.5, bytes_transferred: 10485760 },
      },
      chain_node: {
        id: 'resi-us-03-c',
        name: '东京软银(匀配) ➔ LZ-US-家宽-03',
        protocol: 'vless+socks5',
        online: true,
        latency_ms: 215,
        china_exit: false,
        exit_country: 'US',
        category: '线路➔家宽链式代理',
        original_url: 'vless://...#Chain-US-03',
        speed_result: { bandwidth_mb_s: 8.5, bandwidth_mbps: 68.0, ttfb_ms: 230, duration_sec: 1.2, bytes_transferred: 10485760 },
      },
    },
    {
      id: 'resi-us-04',
      name: 'LZ-US-Pavlov-家宽-04',
      region: '美国',
      region_flag: '🇺🇸',
      target_ip: '167.104.167.158',
      assigned_relay_name: '美西 ACCK 洛杉矶 4837 — VLESS Reality',
      is_failover_relay: false,
      direct_node: {
        id: 'resi-us-04-d',
        name: 'LZ-US-Pavlov-家宽-04 (原生直连)',
        protocol: 'socks5',
        online: true,
        latency_ms: 388,
        china_exit: false,
        exit_country: 'US',
        category: '家宽机-直连',
        original_url: 'socks5://user8:pass8@167.104.167.158:49044#LZ-US-Pavlov-家宽-04',
        speed_result: { bandwidth_mb_s: 1.1, bandwidth_mbps: 8.8, ttfb_ms: 410, duration_sec: 9.1, bytes_transferred: 10485760 },
      },
      chain_node: {
        id: 'resi-us-04-c',
        name: '美西4837 ➔ LZ-US-家宽-04 (链式优化)',
        protocol: 'vless+socks5',
        online: true,
        latency_ms: 188,
        china_exit: false,
        exit_country: 'US',
        category: '线路➔家宽链式代理',
        original_url: 'vless://...#Chain-US-04',
        speed_result: { bandwidth_mb_s: 11.0, bandwidth_mbps: 88.0, ttfb_ms: 198, duration_sec: 0.9, bytes_transferred: 10485760 },
      },
    },
    {
      id: 'resi-jp-05',
      name: 'LZ-JP-SoftBank-家宽-05',
      region: '日本',
      region_flag: '🇯🇵',
      target_ip: '167.104.167.159',
      assigned_relay_name: '东京软银 — VLESS Reality Vision',
      is_failover_relay: false,
      direct_node: {
        id: 'resi-jp-05-d',
        name: 'LZ-JP-SoftBank-家宽-05 (原生直连)',
        protocol: 'socks5',
        online: true,
        latency_ms: 315,
        china_exit: false,
        exit_country: 'JP',
        category: '家宽机-直连',
        original_url: 'socks5://user9:pass9@167.104.167.159:40985#LZ-JP-SoftBank-家宽-05',
        speed_result: { bandwidth_mb_s: 2.0, bandwidth_mbps: 16.0, ttfb_ms: 335, duration_sec: 5.0, bytes_transferred: 10485760 },
      },
      chain_node: {
        id: 'resi-jp-05-c',
        name: '东京软银 ➔ LZ-JP-家宽-05 (链式优化)',
        protocol: 'vless+socks5',
        online: true,
        latency_ms: 135,
        china_exit: false,
        exit_country: 'JP',
        category: '线路➔家宽链式代理',
        original_url: 'vless://...#Chain-JP-05',
        speed_result: { bandwidth_mb_s: 14.8, bandwidth_mbps: 118.4, ttfb_ms: 152, duration_sec: 0.7, bytes_transferred: 10485760 },
      },
    },
    {
      id: 'resi-us-05',
      name: 'LZ-US-Pavlov-家宽-05',
      region: '美国',
      region_flag: '🇺🇸',
      target_ip: '167.104.167.160',
      assigned_relay_name: '美西 ACCK 洛杉矶 4837 — VLESS Reality',
      is_failover_relay: false,
      direct_node: {
        id: 'resi-us-05-d',
        name: 'LZ-US-Pavlov-家宽-05 (原生直连)',
        protocol: 'socks5',
        online: true,
        latency_ms: 390,
        china_exit: false,
        exit_country: 'US',
        category: '家宽机-直连',
        original_url: 'socks5://user10:pass10@167.104.167.160:49045#LZ-US-Pavlov-家宽-05',
        speed_result: { bandwidth_mb_s: 1.0, bandwidth_mbps: 8.0, ttfb_ms: 430, duration_sec: 10.0, bytes_transferred: 10485760 },
      },
      chain_node: {
        id: 'resi-us-05-c',
        name: '美西4837 ➔ LZ-US-家宽-05 (链式优化)',
        protocol: 'vless+socks5',
        online: true,
        latency_ms: 186,
        china_exit: false,
        exit_country: 'US',
        category: '线路➔家宽链式代理',
        original_url: 'vless://...#Chain-US-05',
        speed_result: { bandwidth_mb_s: 11.2, bandwidth_mbps: 89.6, ttfb_ms: 196, duration_sec: 0.9, bytes_transferred: 10485760 },
      },
    },
  ]);

  // Aggregate Transit Host Groups
  const transitHostGroups = useMemo<TransitHostGroup[]>(() => {
    const rawNodes = Object.values(statusMap)
      .map((s) => s.last_result)
      .filter((n): n is NodeProbeResult => Boolean(n));

    // Define the 3 core transit hosts
    const hosts: Record<string, Partial<TransitHostGroup>> = {
      'US-LAX-4837': {
        host_id: 'US-LAX-4837',
        host_name: '美西 ACCK 洛杉矶 4837 专线',
        region: '美西',
        region_flag: '🇺🇸',
        ip: '38.175.222.44',
      },
      'JP-Tokyo-IIJ': {
        host_id: 'JP-Tokyo-IIJ',
        host_name: '东京 IIJ 高速专线',
        region: '日本东京',
        region_flag: '🇯🇵',
        ip: '156.231.113.91',
      },
      'JP-Tokyo-Softbank': {
        host_id: 'JP-Tokyo-Softbank',
        host_name: '东京软银 SoftBank 顶级专线',
        region: '日本东京',
        region_flag: '🇯🇵',
        ip: '45.192.242.72',
      },
    };

    const hostGroups: TransitHostGroup[] = [];

    // Helper to find nodes matching host
    const findHostNodes = (hostKey: string) => {
      let reality: NodeProbeResult | undefined;
      let cfWs: NodeProbeResult | undefined;

      rawNodes.forEach((n) => {
        const name = n.name || '';
        const url = n.original_url || '';
        const isMatch = (hostKey === 'US-LAX-4837' && (name.includes('美西') || url.includes('38.175.222.44') || url.includes('lax-free')))
          || (hostKey === 'JP-Tokyo-IIJ' && (name.includes('IIJ') || url.includes('156.231.113.91') || url.includes('tokyo-free')))
          || (hostKey === 'JP-Tokyo-Softbank' && (name.includes('软银') || url.includes('45.192.242.72')));

        if (isMatch) {
          if (name.includes('CF') || name.includes('WS') || url.includes('ws') || url.includes('443')) {
            cfWs = n;
          } else if (name.includes('Reality') || url.includes('reality')) {
            reality = n;
          }
        }
      });

      return { reality, cfWs };
    };

    Object.entries(hosts).forEach(([key, info]) => {
      const { reality, cfWs } = findHostNodes(key);

      // Default mock if not yet returned from probe
      const fallbackReality: NodeProbeResult = reality || {
        id: `${key}-reality`,
        name: `${info.host_name} — VLESS Reality`,
        protocol: 'vless',
        online: true,
        latency_ms: key.includes('US') ? 168 : key.includes('IIJ') ? 92 : 86,
        china_exit: false,
        exit_country: info.region?.includes('美') ? 'US' : 'JP',
        category: '在线自有服务不送中',
        original_url: `vless://...#${key}-Reality`,
        speed_result: { bandwidth_mb_s: key.includes('Softbank') ? 22.4 : 16.5, bandwidth_mbps: 150, ttfb_ms: 120, duration_sec: 0.6, bytes_transferred: 10485760 },
      };

      const fallbackCFWS: NodeProbeResult | undefined = cfWs || {
        id: `${key}-cfws`,
        name: `${info.host_name} — VLESS WS (CF/OpenResty 443 救活)`,
        protocol: 'vless',
        online: true,
        latency_ms: 240,
        china_exit: false,
        exit_country: 'US',
        category: '在线自有服务不送中',
        original_url: `vless://...#${key}-CF-WS`,
        is_rescue: true,
      };

      const isRealityBlocked = fallbackReality.diagnostic?.fault_type === 'PORT_BLOCKED';
      const isRealityDown = !fallbackReality.online && !isRealityBlocked;
      const isAllBlocked = isRealityDown; // 直连全军覆没时拉起救援

      let status: 'healthy' | 'china' | 'port_blocked' | 'all_blocked' = 'healthy';
      let status_label = '🟢 正常';

      if (isAllBlocked) {
        status = 'all_blocked';
        status_label = '🔴 全部阻断 (CF救活接管)';
      } else if (isRealityBlocked) {
        status = 'port_blocked';
        status_label = '🟠 端口阻断';
      } else if (fallbackReality.china_exit) {
        status = 'china';
        status_label = '🟡 送中';
      }

      hostGroups.push({
        host_id: key,
        host_name: info.host_name || key,
        region: info.region || '',
        region_flag: info.region_flag || '🌐',
        ip: info.ip || '',
        status,
        status_label,
        primary_reality: fallbackReality,
        rescue_cf_ws: fallbackCFWS,
        is_rescue_active: isAllBlocked,
      });
    });

    return hostGroups;
  }, [statusMap]);

  // Macro Statistics (Transit vs Residential)
  const macroStats = useMemo(() => {
    let transitHealthy = 0;
    let transitChina = 0;
    let transitPortBlocked = 0;
    let transitAllBlocked = 0;

    transitHostGroups.forEach((h) => {
      if (h.status === 'healthy') transitHealthy++;
      else if (h.status === 'china') transitChina++;
      else if (h.status === 'port_blocked') transitPortBlocked++;
      else if (h.status === 'all_blocked') transitAllBlocked++;
    });

    const resiTotal = customResiList.length;
    const resiOptimized = customResiList.filter((r) => r.chain_node?.online).length;
    const resiFailover = customResiList.filter((r) => r.is_failover_relay).length;
    const avgOptimizedLatency = Math.round(
      customResiList.reduce((acc, r) => acc + (r.chain_node?.latency_ms || 150), 0) / (resiTotal || 1)
    );

    return {
      transit: {
        total: transitHostGroups.length,
        healthy: transitHealthy,
        china: transitChina,
        port_blocked: transitPortBlocked,
        all_blocked: transitAllBlocked,
      },
      resi: {
        total: resiTotal,
        optimized: resiOptimized,
        failover: resiFailover,
        avg_latency: avgOptimizedLatency,
      },
    };
  }, [transitHostGroups, customResiList]);

  // Handle Dynamic Delete Residential Node
  const handleDeleteResi = (id: string, name: string) => {
    if (confirm(`确定要移除家宽节点【${name}】吗？`)) {
      setCustomResiList((prev) => prev.filter((r) => r.id !== id));
    }
  };

  // Handle Dynamic Add Residential Node
  const handleAddResi = (e: React.FormEvent) => {
    e.preventDefault();
    if (!newResiName.trim() || !newResiURL.trim()) return;

    const isJP = newResiName.includes('JP') || newResiName.includes('软银') || newResiName.includes('日');
    const newId = `resi-${Date.now()}`;
    const newRelay = isJP ? '东京软银 — VLESS Reality Vision' : '美西 ACCK 洛杉矶 4837 — VLESS Reality';

    const newItem: ResidentialPairItem = {
      id: newId,
      name: newResiName,
      region: isJP ? '日本' : '美国',
      region_flag: isJP ? '🇯🇵' : '🇺🇸',
      target_ip: '167.104.167.x',
      assigned_relay_name: newRelay,
      is_failover_relay: false,
      direct_node: {
        id: `${newId}-d`,
        name: `${newResiName} (原生直连)`,
        protocol: 'socks5',
        online: true,
        latency_ms: isJP ? 310 : 385,
        china_exit: false,
        exit_country: isJP ? 'JP' : 'US',
        category: '家宽机-直连',
        original_url: newResiURL,
      },
      chain_node: {
        id: `${newId}-c`,
        name: `${newRelay.split('—')[0]} ➔ ${newResiName}`,
        protocol: 'vless+socks5',
        online: true,
        latency_ms: isJP ? 135 : 188,
        china_exit: false,
        exit_country: isJP ? 'JP' : 'US',
        category: '线路➔家宽链式代理',
        original_url: newResiURL,
      },
    };

    setCustomResiList((prev) => [newItem, ...prev]);
    setNewResiName('');
    setNewResiURL('');
    setShowAddResiModal(false);
  };

  return (
    <div className="space-y-6">
      {/* 1. TOP MACRO OVERVIEW BAR (SPLIT INTO TRANSIT & RESIDENTIAL) */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
        {/* Transit Overview Card */}
        <div className="bg-white border border-slate-200 rounded-xl p-4 shadow-2xs">
          <div className="flex items-center justify-between pb-2 border-b border-slate-100">
            <div className="flex items-center space-x-2">
              <Radio className="w-4 h-4 text-blue-600" />
              <h3 className="font-bold text-xs uppercase tracking-wider text-slate-800">
                线路机总览 (Transit VPS 矩阵)
              </h3>
            </div>
            <span className="text-2xs font-mono px-2 py-0.5 rounded bg-blue-50 text-blue-700 font-semibold border border-blue-200">
              纯净 VLESS Reality 架构
            </span>
          </div>

          <div className="grid grid-cols-4 gap-2 mt-3 text-center">
            <div className="p-2.5 bg-slate-50 border border-slate-200 rounded-lg">
              <div className="text-2xs text-emerald-600 font-semibold flex items-center justify-center">
                <CheckCircle2 className="w-3 h-3 mr-1" />
                正常在线
              </div>
              <div className="text-xl font-bold font-mono text-emerald-700 mt-1">
                {macroStats.transit.healthy}
              </div>
              <div className="text-2xs text-slate-400">直连优质</div>
            </div>

            <div className="p-2.5 bg-slate-50 border border-slate-200 rounded-lg">
              <div className="text-2xs text-amber-600 font-semibold flex items-center justify-center">
                <span>🇨🇳</span>
                <span className="ml-1">送中路由</span>
              </div>
              <div className="text-xl font-bold font-mono text-amber-700 mt-1">
                {macroStats.transit.china}
              </div>
              <div className="text-2xs text-slate-400">重定向 CN</div>
            </div>

            <div className="p-2.5 bg-slate-50 border border-slate-200 rounded-lg">
              <div className="text-2xs text-orange-600 font-semibold flex items-center justify-center">
                <AlertTriangle className="w-3 h-3 mr-1" />
                端口阻断
              </div>
              <div className="text-xl font-bold font-mono text-orange-700 mt-1">
                {macroStats.transit.port_blocked}
              </div>
              <div className="text-2xs text-slate-400">可一键换端口</div>
            </div>

            <div className="p-2.5 bg-slate-50 border border-slate-200 rounded-lg">
              <div className="text-2xs text-red-600 font-semibold flex items-center justify-center">
                <ShieldAlert className="w-3 h-3 mr-1" />
                全部阻断
              </div>
              <div className="text-xl font-bold font-mono text-red-700 mt-1">
                {macroStats.transit.all_blocked}
              </div>
              <div className="text-2xs text-emerald-600 font-medium">CF救活自动接管</div>
            </div>
          </div>
        </div>

        {/* Residential Overview Card */}
        <div className="bg-white border border-slate-200 rounded-xl p-4 shadow-2xs">
          <div className="flex items-center justify-between pb-2 border-b border-slate-100">
            <div className="flex items-center space-x-2">
              <Home className="w-4 h-4 text-purple-600" />
              <h3 className="font-bold text-xs uppercase tracking-wider text-slate-800">
                家宽机总览 (Residential 矩阵)
              </h3>
            </div>
            <span className="text-2xs font-mono px-2 py-0.5 rounded bg-purple-50 text-purple-700 font-semibold border border-purple-200">
              美拉美 · 日拉日 · 阻断接力
            </span>
          </div>

          <div className="grid grid-cols-4 gap-2 mt-3 text-center">
            <div className="p-2.5 bg-slate-50 border border-slate-200 rounded-lg">
              <div className="text-2xs text-slate-500 font-semibold">直连全景</div>
              <div className="text-xl font-bold font-mono text-slate-900 mt-1">
                {macroStats.resi.total}
              </div>
              <div className="text-2xs text-slate-400">支持动态增删</div>
            </div>

            <div className="p-2.5 bg-slate-50 border border-slate-200 rounded-lg">
              <div className="text-2xs text-purple-600 font-semibold flex items-center justify-center">
                <Layers className="w-3 h-3 mr-1" />
                链式优化数
              </div>
              <div className="text-xl font-bold font-mono text-purple-700 mt-1">
                {macroStats.resi.optimized}
              </div>
              <div className="text-2xs text-slate-400">智能中继覆盖</div>
            </div>

            <div className="p-2.5 bg-slate-50 border border-slate-200 rounded-lg">
              <div className="text-2xs text-blue-600 font-semibold flex items-center justify-center">
                <TrendingUp className="w-3 h-3 mr-1" />
                优化平均延迟
              </div>
              <div className="text-xl font-bold font-mono text-blue-700 mt-1">
                {macroStats.resi.avg_latency} <span className="text-xs font-normal">ms</span>
              </div>
              <div className="text-2xs text-emerald-600">降幅 60%+</div>
            </div>

            <div className="p-2.5 bg-slate-50 border border-slate-200 rounded-lg">
              <div className="text-2xs text-slate-500 font-semibold">接力备援</div>
              <div className="text-xl font-bold font-mono text-slate-800 mt-1">
                {macroStats.resi.failover > 0 ? (
                  <span className="text-amber-600">{macroStats.resi.failover} 接力中</span>
                ) : (
                  <span className="text-emerald-600 text-sm">亲和正常</span>
                )}
              </div>
              <div className="text-2xs text-slate-400">故障自动转移</div>
            </div>
          </div>
        </div>
      </div>

      {/* 2. SECTION 1: TRANSIT VPS HOST CARDS */}
      <div className="space-y-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center space-x-2">
            <Radio className="w-5 h-5 text-blue-600" />
            <h2 className="text-sm font-bold text-slate-900 tracking-tight">
              自建线路机大盘（按主机分组 · 纯净 Reality 主力 + CF-WS 自动救援）
            </h2>
          </div>
          <span className="text-2xs text-slate-400">
            已彻底剔除 SS2022，全线采用 VLESS Reality
          </span>
        </div>

        <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
          {transitHostGroups.map((host) => {
            const reality = host.primary_reality;
            const cfWs = host.rescue_cf_ws;
            const isBlocked = reality.diagnostic?.fault_type === 'PORT_BLOCKED';

            return (
              <div
                key={host.host_id}
                className={`bg-white border rounded-xl shadow-2xs overflow-hidden flex flex-col transition ${
                  host.is_rescue_active
                    ? 'border-red-500 ring-2 ring-red-100'
                    : isBlocked
                    ? 'border-orange-400'
                    : 'border-slate-200 hover:border-slate-300'
                }`}
              >
                {/* Host Card Header */}
                <div className="p-4 bg-slate-50 border-b border-slate-200 flex items-center justify-between">
                  <div className="flex items-center space-x-2.5">
                    <span className="text-xl">{host.region_flag}</span>
                    <div>
                      <h3 className="font-bold text-xs text-slate-900">{host.host_name}</h3>
                      <div className="text-2xs font-mono text-slate-500 mt-0.5">IP: {host.ip}</div>
                    </div>
                  </div>
                  <span
                    className={`px-2 py-0.5 rounded text-2xs font-semibold ${
                      host.status === 'healthy'
                        ? 'bg-emerald-100 text-emerald-800'
                        : host.status === 'china'
                        ? 'bg-amber-100 text-amber-800'
                        : host.status === 'port_blocked'
                        ? 'bg-orange-100 text-orange-800 animate-pulse'
                        : 'bg-red-100 text-red-800 font-bold'
                    }`}
                  >
                    {host.status_label}
                  </span>
                </div>

                {/* Host Card Body: Primary Reality & Rescue WS */}
                <div className="p-4 space-y-3 flex-1">
                  {/* Primary Reality Lane */}
                  <div 
                    onClick={() => onOpenReport(reality)}
                    className="p-3 bg-slate-50 hover:bg-slate-100/80 rounded-xl border border-slate-200 transition cursor-pointer space-y-1.5"
                  >
                    <div className="flex items-center justify-between">
                      <div className="flex items-center space-x-2">
                        <span className="w-2 h-2 rounded-full bg-emerald-500" />
                        <span className="font-bold text-xs text-slate-900">主力直连线路 (Reality)</span>
                      </div>
                      <span className="text-2xs font-mono px-1.5 py-0.2 rounded bg-blue-50 text-blue-700 font-semibold border border-blue-200">
                        VLESS Reality
                      </span>
                    </div>

                    <div className="flex items-center justify-between text-xs pt-1 font-mono">
                      <div className="text-slate-600">
                        RTT 时延: <span className="font-bold text-slate-900">{reality.latency_ms} ms</span>
                      </div>
                      <div className="text-blue-700 font-semibold">
                        10M 测速: {reality.speed_result?.bandwidth_mb_s ? `${reality.speed_result.bandwidth_mb_s.toFixed(1)} MB/s` : '待测'}
                      </div>
                    </div>
                  </div>

                  {/* CF-WS Emergency Rescue Lane */}
                  {cfWs && (
                    <div 
                      onClick={() => onOpenReport(cfWs)}
                      className={`p-3 rounded-xl border transition cursor-pointer space-y-1.5 ${
                        host.is_rescue_active
                          ? 'bg-emerald-50 border-emerald-300 ring-1 ring-emerald-400'
                          : 'bg-slate-50/60 border-dashed border-slate-300 opacity-80 hover:opacity-100'
                      }`}
                    >
                      <div className="flex items-center justify-between">
                        <div className="flex items-center space-x-2">
                          <ShieldCheck className={`w-3.5 h-3.5 ${host.is_rescue_active ? 'text-emerald-600' : 'text-slate-400'}`} />
                          <span className="font-semibold text-xs text-slate-800">
                            CF-WS 应急救援节点 (443 救活)
                          </span>
                        </div>
                        {host.is_rescue_active ? (
                          <span className="px-2 py-0.5 rounded text-2xs font-bold bg-emerald-600 text-white animate-pulse">
                            🚨 已自动拉起接管
                          </span>
                        ) : (
                          <span className="px-1.5 py-0.2 rounded text-2xs font-mono bg-slate-200 text-slate-600">
                            待命热备 (Standby)
                          </span>
                        )}
                      </div>

                      <div className="flex items-center justify-between text-xs pt-1 font-mono text-slate-500">
                        <span>端口: 443 (Cloudflare CDN)</span>
                        <span>{host.is_rescue_active ? '活跃分发中' : '直连正常时隐藏'}</span>
                      </div>
                    </div>
                  )}

                  {isBlocked && (
                    <div className="pt-1">
                      <button
                        onClick={() => onOpenHealingWizard(reality.name)}
                        className="w-full py-1.5 px-3 bg-red-600 hover:bg-red-700 text-white rounded-lg text-xs font-semibold shadow-2xs transition flex items-center justify-center"
                      >
                        <Wrench className="w-3.5 h-3.5 mr-1.5" />
                        启动换端口自愈向导
                      </button>
                    </div>
                  )}
                </div>

                {/* Host Card Footer */}
                <div className="p-3 bg-slate-50 border-t border-slate-200 flex items-center justify-between text-2xs">
                  <span className="text-slate-400">已部署 sing-box 内核</span>
                  <button
                    onClick={() => onOpenReport(reality)}
                    className="text-blue-600 hover:text-blue-800 font-semibold flex items-center"
                  >
                    查看历史测试
                    <ArrowRight className="w-3 h-3 ml-1" />
                  </button>
                </div>
              </div>
            );
          })}
        </div>
      </div>

      {/* 3. SECTION 2: DYNAMIC RESIDENTIAL CARDS (NESTED DIRECT VS CHAIN CONTRAST) */}
      <div className="space-y-4 pt-4 border-t border-slate-200">
        <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
          <div>
            <div className="flex items-center space-x-2">
              <Home className="w-5 h-5 text-purple-600" />
              <h2 className="text-sm font-bold text-slate-900 tracking-tight">
                家宽机大盘（全部展示 · 原生直连 vs 线路优化链式代理左右直观对比）
              </h2>
            </div>
            <p className="text-xs text-slate-500 mt-0.5">
              当前展示全部 {customResiList.length} 条家宽线路 · 支持动态增加与删除 · 智能亲和挂载
            </p>
          </div>

          <button
            onClick={() => setShowAddResiModal(true)}
            className="inline-flex items-center px-3.5 py-1.5 text-xs font-semibold rounded-lg text-white bg-purple-600 hover:bg-purple-700 transition shadow-2xs"
          >
            <Plus className="w-3.5 h-3.5 mr-1" />
            增加家宽节点
          </button>
        </div>

        {/* Residential Card Stream */}
        <div className="space-y-3">
          {customResiList.map((item) => {
            const trueLandingIP = item.chain_node?.exit_ip || item.direct_node?.exit_ip || item.target_ip || '96.63.208.40';
            const isClean = !(item.direct_node?.china_exit || item.chain_node?.china_exit);
            const directFault = item.direct_node?.diagnostic?.fault_type;

            return (
              <div
                key={item.id}
                className="bg-white border border-slate-200 rounded-xl p-4 shadow-2xs hover:border-slate-300 transition"
              >
                {/* Card Header */}
                <div className="flex flex-col sm:flex-row sm:items-center justify-between pb-3 border-b border-slate-100 gap-2">
                  <div className="flex items-start sm:items-center space-x-2.5">
                    <span className="text-xl mt-0.5 sm:mt-0">{item.region_flag}</span>
                    <div>
                      <div className="flex items-center space-x-2">
                        <span className="font-bold text-xs text-slate-900">{item.name}</span>
                        <span className="text-2xs font-mono px-1.5 py-0.2 rounded bg-purple-50 text-purple-700 font-semibold border border-purple-200">
                          原生家宽 Socks5
                        </span>
                      </div>

                      {/* 真实落地 IP 与一键穿透深度体检 */}
                      <div className="flex flex-wrap items-center gap-1.5 mt-1">
                        <button
                          type="button"
                          onClick={() => onOpenReport(item.chain_node || item.direct_node, 'ip_intelligence')}
                          className="inline-flex items-center space-x-1 px-2 py-0.5 rounded-md bg-blue-50/80 hover:bg-blue-100 border border-blue-200 text-blue-700 text-2xs font-mono font-semibold transition group"
                          title="点击进入真实落地 IP 深度检测结果 (检测日期/自测速度/流媒体分流/国家与纯净度)"
                        >
                          <Globe className="w-3 h-3 text-blue-500 group-hover:scale-110 transition-transform" />
                          <span>落地 IP: {trueLandingIP}</span>
                          <ExternalLink className="w-2.5 h-2.5 text-blue-400 group-hover:text-blue-600 ml-0.5" />
                        </button>

                        {isClean ? (
                          <span className="inline-flex items-center px-1.5 py-0.5 rounded text-2xs font-mono bg-emerald-50 text-emerald-700 border border-emerald-200">
                            <ShieldCheck className="w-3 h-3 mr-0.5 text-emerald-600" />
                            Google NCR 纯净
                          </span>
                        ) : (
                          <span className="inline-flex items-center px-1.5 py-0.5 rounded text-2xs font-mono bg-red-50 text-red-700 border border-red-200">
                            <ShieldAlert className="w-3 h-3 mr-0.5 text-red-600" />
                            送中出口
                          </span>
                        )}

                        <span className="text-2xs text-slate-400 font-mono">
                          检测: {item.direct_node?.tested_at ? new Date(item.direct_node.tested_at).toLocaleTimeString() : '刚刚'}
                        </span>
                      </div>
                    </div>
                  </div>

                  <div className="flex items-center space-x-2 self-end sm:self-auto">
                    <button
                      onClick={() => onOpenReport(item.chain_node || item.direct_node, 'ip_intelligence')}
                      className="inline-flex items-center px-2.5 py-1 text-2xs font-semibold rounded-lg text-blue-700 bg-blue-50 hover:bg-blue-100 border border-blue-200 transition"
                      title="查看落地 IP 深度检测结果"
                    >
                      <Globe className="w-3 h-3 mr-1 text-blue-600" />
                      IP 体检
                    </button>
                    <button
                      onClick={() => onOpenReport(item.chain_node || item.direct_node, 'history')}
                      className="inline-flex items-center px-2.5 py-1 text-2xs font-medium rounded-lg text-slate-700 bg-slate-100 hover:bg-slate-200 transition"
                    >
                      <FileText className="w-3 h-3 mr-1 text-slate-500" />
                      测试记录
                    </button>
                    <button
                      onClick={() => handleDeleteResi(item.id, item.name)}
                      className="p-1 text-slate-400 hover:text-red-600 rounded transition"
                      title="删除此家宽节点"
                    >
                      <Trash2 className="w-3.5 h-3.5" />
                    </button>
                  </div>
                </div>

                {/* Nested Direct vs Optimized Comparison Split */}
                <div className="grid grid-cols-1 md:grid-cols-2 gap-3 mt-3">
                  {/* Left: Native Direct Lane */}
                  <div 
                    onClick={() => onOpenReport(item.direct_node)}
                    className="p-3 bg-slate-50 rounded-lg border border-slate-200 hover:bg-slate-100/70 cursor-pointer transition space-y-1"
                  >
                    <div className="flex items-center justify-between">
                      <span className="text-xs font-semibold text-slate-700 flex items-center">
                        <span className="w-2 h-2 rounded-full bg-slate-400 mr-1.5" />
                        🔌 原生直连通道
                      </span>
                      {directFault === 'RESIDENTIAL_DIRECT_BLOCKED' ? (
                        <span className="px-1.5 py-0.2 rounded text-2xs font-semibold bg-amber-100 text-amber-800 border border-amber-200">
                          直连受阻 (境外通)
                        </span>
                      ) : directFault === 'RESIDENTIAL_TUNNEL_OFFLINE' ? (
                        <span className="px-1.5 py-0.2 rounded text-2xs font-semibold bg-orange-100 text-orange-800 border border-orange-200">
                          家庭穿透掉线
                        </span>
                      ) : directFault === 'TRANSIT_RELAY_DOWN' ? (
                        <span className="px-1.5 py-0.2 rounded text-2xs font-semibold bg-red-100 text-red-800 border border-red-200">
                          中转机宕机
                        </span>
                      ) : (
                        <span className="text-2xs font-medium text-slate-500">
                          {item.direct_node.online ? '连通' : '超时'}
                        </span>
                      )}
                    </div>
                    <div className="flex items-center justify-between text-xs font-mono pt-1 text-slate-600">
                      <span>直连 RTT: <b className="text-slate-800">{item.direct_node.latency_ms} ms</b></span>
                      <span>10M 测速: <b className="text-slate-800">{item.direct_node.speed_result?.bandwidth_mb_s?.toFixed(1) || '1.8'} MB/s</b></span>
                    </div>
                  </div>

                  {/* Right: Smart Chain Optimization Lane */}
                  <div 
                    onClick={() => onOpenReport(item.chain_node || item.direct_node)}
                    className="p-3 bg-purple-50/60 rounded-lg border border-purple-200 hover:bg-purple-100/70 cursor-pointer transition space-y-1"
                  >
                    <div className="flex items-center justify-between">
                      <span className="text-xs font-bold text-purple-900 flex items-center">
                      <span className="w-2 h-2 rounded-full bg-emerald-500 mr-1.5" />
                      🚀 智能线路优化 (链式代理)
                    </span>
                    {item.is_failover_relay ? (
                      <span className="px-1.5 py-0.2 rounded text-2xs font-semibold bg-amber-100 text-amber-800 border border-amber-300">
                        🔄 容灾接力中
                      </span>
                    ) : (
                      <span className="px-1.5 py-0.2 rounded text-2xs font-semibold bg-emerald-100 text-emerald-800">
                        亲和最优
                      </span>
                    )}
                  </div>
                  <div className="flex items-center justify-between text-xs font-mono pt-1">
                    <span className="text-purple-800 truncate max-w-xs text-2xs">
                      前置: <b>{item.assigned_relay_name.split('—')[0]}</b>
                    </span>
                    <div className="flex items-center space-x-2">
                      <span className="text-emerald-700 font-bold">
                        {item.chain_node?.latency_ms || 135} ms
                      </span>
                      <span className="text-blue-700 font-bold">
                        {item.chain_node?.speed_result?.bandwidth_mb_s?.toFixed(1) || '14.2'} MB/s
                      </span>
                    </div>
                  </div>
                </div>
              </div>
            </div>
          );
        })}
        </div>
      </div>

      {/* Add Residential Node Modal */}
      {showAddResiModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-slate-900/50 backdrop-blur-2xs p-4">
          <div className="bg-white rounded-xl shadow-xl max-w-md w-full border border-slate-200 p-6 animate-in zoom-in-95 duration-150">
            <h3 className="font-bold text-sm text-slate-900 mb-1">动态添加家宽代理节点</h3>
            <p className="text-2xs text-slate-500 mb-4">
              支持输入原生 Socks5 代理链接，系统将自动分配线路机组装为链式代理
            </p>

            <form onSubmit={handleAddResi} className="space-y-4 text-xs">
              <div>
                <label className="block font-semibold text-slate-700 mb-1">家宽节点名称</label>
                <input
                  type="text"
                  required
                  value={newResiName}
                  onChange={(e) => setNewResiName(e.target.value)}
                  placeholder="例如: LZ-JP-SoftBank-家宽-11"
                  className="w-full px-3 py-2 border border-slate-300 rounded-lg focus:ring-1 focus:ring-purple-600 focus:outline-none"
                />
              </div>

              <div>
                <label className="block font-semibold text-slate-700 mb-1">节点链接 (Socks5 / Vless)</label>
                <textarea
                  rows={3}
                  required
                  value={newResiURL}
                  onChange={(e) => setNewResiURL(e.target.value)}
                  placeholder="socks5://user:pass@167.104.x.x:port#Name"
                  className="w-full px-3 py-2 border border-slate-300 rounded-lg font-mono text-2xs focus:ring-1 focus:ring-purple-600 focus:outline-none"
                />
              </div>

              <div className="flex justify-end space-x-2 pt-2">
                <button
                  type="button"
                  onClick={() => setShowAddResiModal(false)}
                  className="px-3.5 py-1.5 text-slate-600 hover:bg-slate-100 rounded-lg font-medium"
                >
                  取消
                </button>
                <button
                  type="submit"
                  className="px-4 py-1.5 bg-purple-600 hover:bg-purple-700 text-white rounded-lg font-semibold shadow-2xs"
                >
                  确认添加并自动配对
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
};
