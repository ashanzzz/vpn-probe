import React, { useState, useEffect, useRef, useCallback } from 'react';
import { api, getStoredToken, removeStoredToken } from './api/client';
import { 
  MonitorStatusMap, 
  NodeProbeResult 
} from './types';
import { LoginGate } from './components/LoginGate';
import { Header, TabType } from './components/Header';
import { QuotaBar } from './components/QuotaBar';
import { AlertBanner } from './components/AlertBanner';
import { MonitorDashboard } from './components/MonitorDashboard';
import { ReportDrawer } from './components/ReportDrawer';
import { HealingWizard } from './components/HealingWizard';
import { VPSCenter } from './components/VPSCenter';
import { PipelineHub } from './components/PipelineHub';
import { QuickProbe } from './components/QuickProbe';
import { SettingsLogs } from './components/SettingsLogs';

export const App: React.FC = () => {
  const [token, setToken] = useState<string>(() => getStoredToken());
  const [currentTab, setCurrentTab] = useState<TabType>('dashboard');
  const [statusMap, setStatusMap] = useState<MonitorStatusMap>({});
  const [blockedNodes, setBlockedNodes] = useState<any[]>([]);

  // Daily quota tracking (speedtest 2/day, split 1/day)
  const [speedTestCount, setSpeedTestCount] = useState<number>(0);
  const [lastSpeedTestTime, setLastSpeedTestTime] = useState<string | null>(null);
  const [splitCheckCount, setSplitCheckCount] = useState<number>(0);
  const [lastSplitCheckTime, setLastSplitCheckTime] = useState<string | null>(null);

  // Inspection & Speedtest states
  const [isInspecting, setIsInspecting] = useState(false);
  const [isSpeedTesting, setIsSpeedTesting] = useState(false);

  // Polling Interval in seconds (default 10s, bursts to 2s)
  const [pollingInterval, setPollingInterval] = useState<number>(10);
  const pollingTimerRef = useRef<any>(null);

  // Modal / Drawer states
  const [selectedReportNode, setSelectedReportNode] = useState<NodeProbeResult | null>(null);
  const [reportInitialTab, setReportInitialTab] = useState<'overview' | 'ip_intelligence' | 'history' | 'logs'>('overview');
  const [healingWizardOpen, setHealingWizardOpen] = useState(false);
  const [healingInitialNode, setHealingInitialNode] = useState<string | undefined>(undefined);

  // Fetch monitor & blocked node status
  const fetchStatus = useCallback(async () => {
    if (!token) return;
    try {
      const [mStatus, blockedData] = await Promise.all([
        api.getMonitorStatus().catch(() => null),
        api.getBlockedPorts().catch(() => ({ total: 0, nodes: [] })),
      ]);

      if (mStatus) {
        setStatusMap(mStatus);
      }
      if (blockedData && blockedData.nodes) {
        setBlockedNodes(blockedData.nodes);
      }
    } catch (err: any) {
      if (err.message && err.message.includes('AUTH_FAILED')) {
        handleLogout();
      }
    }
  }, [token]);

  // Initial load
  useEffect(() => {
    if (token) {
      fetchStatus();
    }
  }, [token, fetchStatus]);

  // Adaptive polling loop
  useEffect(() => {
    if (!token || pollingInterval <= 0) {
      if (pollingTimerRef.current) clearInterval(pollingTimerRef.current);
      return;
    }

    // If an inspection or speedtest is active, temporarily burst to 2s
    const activeInterval = isInspecting || isSpeedTesting ? 2 : pollingInterval;

    if (pollingTimerRef.current) clearInterval(pollingTimerRef.current);
    pollingTimerRef.current = setInterval(() => {
      fetchStatus();
    }, activeInterval * 1000);

    return () => {
      if (pollingTimerRef.current) clearInterval(pollingTimerRef.current);
    };
  }, [token, pollingInterval, isInspecting, isSpeedTesting, fetchStatus]);

  // Handle Logout
  const handleLogout = () => {
    removeStoredToken();
    setToken('');
    setStatusMap({});
    setSelectedReportNode(null);
  };

  // Trigger Manual Inspection
  const handleTriggerInspection = async (split: boolean) => {
    setIsInspecting(true);
    try {
      await api.triggerInspection(split);
      if (split) {
        setSplitCheckCount((prev) => prev + 1);
        setLastSplitCheckTime(new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }));
      }
      // Burst poll immediately
      await fetchStatus();
      // Allow inspection to run for ~6-8s before resetting animated state
      setTimeout(() => {
        setIsInspecting(false);
        fetchStatus();
      }, 7000);
    } catch (err: any) {
      alert(`触发巡检失败: ${err.message}`);
      setIsInspecting(false);
    }
  };

  // Trigger Manual Custom 10MB Speedtest Queue
  const handleTriggerSpeedTest = async () => {
    setIsSpeedTesting(true);
    try {
      setSpeedTestCount((prev) => prev + 1);
      setLastSpeedTestTime(new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }));
      await api.runSpeedTest(false, 15);
      await fetchStatus();
    } catch (err: any) {
      alert(`10MB 吞吐量平滑测速失败: ${err.message}`);
    } finally {
      setIsSpeedTesting(false);
    }
  };

  // Retest Single Node
  const handleRetestSingleNode = async (node: NodeProbeResult) => {
    try {
      const resp = await api.probeBatch([node.original_url], node.is_self ?? true, false, true, 8);
      if (resp.results && resp.results.length > 0) {
        const updated = resp.results[0];
        setSelectedReportNode(updated);
        setStatusMap((prev) => ({
          ...prev,
          [node.id || node.name]: {
            ...prev[node.id || node.name],
            last_result: updated,
            last_checked: new Date().toISOString(),
          },
        }));
      }
    } catch (err: any) {
      alert(`重测节点失败: ${err.message}`);
    }
  };

  // Speedtest Single Node (Custom 10M)
  const handleSpeedTestSingleNode = async (node: NodeProbeResult) => {
    try {
      await api.runSpeedTest(false, 15);
      await fetchStatus();
    } catch (err: any) {
      alert(`单节点 10M 测速失败: ${err.message}`);
    }
  };

  // Open Healing Wizard
  const handleOpenHealingWizard = (initialNode?: string) => {
    setHealingInitialNode(initialNode);
    setHealingWizardOpen(true);
  };

  // If not logged in, render Login Gate
  if (!token) {
    return <LoginGate onSuccess={(newToken) => setToken(newToken)} />;
  }

  const allNodeNames = Object.values(statusMap).map((s) => s.config.name);

  return (
    <div className="min-h-screen bg-[#f6f8fa] flex flex-col font-sans">
      {/* 1. Header Navigation */}
      <Header
        currentTab={currentTab}
        onTabChange={setCurrentTab}
        onLogout={handleLogout}
        isInspecting={isInspecting}
      />

      {/* 2. Global Blocked Alert Banner */}
      <AlertBanner
        blockedNodes={blockedNodes}
        onOpenHealingWizard={handleOpenHealingWizard}
      />

      {/* 3. Daily Quota & Schedule Status Bar */}
      <QuotaBar
        speedTestCount={speedTestCount}
        maxSpeedTestCount={2}
        lastSpeedTestTime={lastSpeedTestTime}
        splitCheckCount={splitCheckCount}
        maxSplitCheckCount={1}
        lastSplitCheckTime={lastSplitCheckTime}
        pollingInterval={pollingInterval}
        onPollingIntervalChange={setPollingInterval}
        onTriggerInspection={handleTriggerInspection}
        onTriggerSpeedTest={handleTriggerSpeedTest}
        isSpeedTesting={isSpeedTesting}
        isInspecting={isInspecting}
      />

      {/* 4. Active Workspace Content */}
      <main className="flex-1 max-w-7xl w-full mx-auto px-4 sm:px-6 lg:px-8 py-6">
        {currentTab === 'dashboard' && (
          <MonitorDashboard
            statusMap={statusMap}
            onOpenReport={(node, initialTab = 'overview') => {
              setSelectedReportNode(node);
              setReportInitialTab(initialTab);
            }}
            onRetestNode={handleRetestSingleNode}
            onSpeedTestNode={handleSpeedTestSingleNode}
            onOpenHealingWizard={handleOpenHealingWizard}
            isInspecting={isInspecting}
          />
        )}

        {currentTab === 'pipeline' && (
          <PipelineHub onRefreshGlobal={fetchStatus} />
        )}

        {currentTab === 'vps' && (
          <VPSCenter onOpenHealingWizard={handleOpenHealingWizard} />
        )}

        {currentTab === 'probe' && (
          <QuickProbe />
        )}

        {currentTab === 'settings' && (
          <SettingsLogs />
        )}
      </main>

      {/* 5. Per-Node Diagnostic Report Drawer (Slide-over) */}
      <ReportDrawer
        node={selectedReportNode}
        initialTab={reportInitialTab}
        onClose={() => setSelectedReportNode(null)}
        onRetest={handleRetestSingleNode}
        onSpeedTest={handleSpeedTestSingleNode}
        onHealPort={handleOpenHealingWizard}
        isTestingThisNode={false}
      />

      {/* 6. Step-by-Step Port Healing Wizard Modal */}
      {healingWizardOpen && (
        <HealingWizard
          initialNodeName={healingInitialNode}
          availableNodes={allNodeNames}
          onClose={() => setHealingWizardOpen(false)}
          onSuccess={() => {
            fetchStatus();
          }}
        />
      )}
    </div>
  );
};

export default App;
