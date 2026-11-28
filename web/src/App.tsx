import React, { useState, useEffect, useRef, useCallback } from 'react';
import { api, getStoredToken, removeStoredToken } from './api/client';
import { MonitorStatusMap, NodeProbeResult } from './types';
import { LoginGate } from './components/LoginGate';
import { Header, TabType } from './components/Header';
import { OverviewWorkspace } from './components/OverviewWorkspace';
import { ReportDrawer } from './components/ReportDrawer';
import { HealingWizard } from './components/HealingWizard';
import { VPSCenter } from './components/VPSCenter';
import { PipelineHub } from './components/PipelineHub';
import { SettingsLogs } from './components/SettingsLogs';
import { ActivityConsoleDrawer } from './components/ActivityConsoleDrawer';
import { FeedbackProvider, useFeedback, WorkspaceTab } from './context/FeedbackContext';

const MainShell: React.FC<{
  token: string;
  onLogout: () => void;
  currentTab: TabType;
  focusSection: string | null;
  onTabChange: (tab: TabType, focusSection?: string) => void;
}> = ({ token, onLogout, currentTab, focusSection, onTabChange }) => {
  const { pushActivity, clearNodeError } = useFeedback();

  const [statusMap, setStatusMap] = useState<MonitorStatusMap>({});
  const [blockedNodes, setBlockedNodes] = useState<any[]>([]);
  const [speedTestCount, setSpeedTestCount] = useState<number>(0);
  const [lastSpeedTestTime, setLastSpeedTestTime] = useState<string | null>(null);
  const [splitCheckCount, setSplitCheckCount] = useState<number>(0);
  const [lastSplitCheckTime, setLastSplitCheckTime] = useState<string | null>(null);

  const [isInspecting, setIsInspecting] = useState(false);
  const [isSpeedTesting, setIsSpeedTesting] = useState(false);
  const [pollingInterval, setPollingInterval] = useState<number>(10);
  const pollingTimerRef = useRef<number | null>(null);

  const [selectedReportNode, setSelectedReportNode] = useState<NodeProbeResult | null>(null);
  const [reportInitialTab, setReportInitialTab] = useState<'overview' | 'ip_intelligence' | 'history' | 'logs'>('overview');
  const [healingWizardOpen, setHealingWizardOpen] = useState(false);
  const [healingInitialNode, setHealingInitialNode] = useState<string | undefined>(undefined);

  const fetchStatus = useCallback(async () => {
    if (!token) return;
    try {
      const [mStatus, blockedData, quotaData] = await Promise.all([
        api.getMonitorStatus().catch(() => null),
        api.getBlockedPorts().catch(() => ({ total: 0, nodes: [] })),
        api.getMonitorQuota().catch(() => null),
      ]);

      if (mStatus) setStatusMap(mStatus);
      if (blockedData && blockedData.nodes) setBlockedNodes(blockedData.nodes);
      if (quotaData) {
        setSpeedTestCount(quotaData.speed_test_count || 0);
        setSplitCheckCount(quotaData.split_check_count || 0);
      }
    } catch (err: any) {
      if (err.message && err.message.includes('AUTH_FAILED')) {
        onLogout();
      }
    }
  }, [token, onLogout]);

  useEffect(() => {
    if (token) fetchStatus();
  }, [token, fetchStatus]);

  useEffect(() => {
    if (!token || pollingInterval <= 0) {
      if (pollingTimerRef.current) window.clearInterval(pollingTimerRef.current);
      return;
    }
    const activeInterval = isInspecting || isSpeedTesting ? 2 : pollingInterval;
    if (pollingTimerRef.current) window.clearInterval(pollingTimerRef.current);
    pollingTimerRef.current = window.setInterval(() => {
      fetchStatus();
    }, activeInterval * 1000);
    return () => {
      if (pollingTimerRef.current) window.clearInterval(pollingTimerRef.current);
    };
  }, [token, pollingInterval, isInspecting, isSpeedTesting, fetchStatus]);

  const handleTriggerInspection = async (split: boolean) => {
    setIsInspecting(true);
    try {
      await api.triggerInspection(split);
      if (split) {
        setSplitCheckCount((prev) => prev + 1);
        setLastSplitCheckTime(new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }));
      }
      pushActivity({
        level: 'info',
        source: '巡检',
        title: split ? '已触发含业务分流全量巡检' : '已触发全量连通性巡检',
      });
      await fetchStatus();
      window.setTimeout(() => {
        setIsInspecting(false);
        fetchStatus();
      }, 7000);
    } catch (err: any) {
      pushActivity({
        level: 'error',
        source: '巡检',
        title: '触发巡检失败',
        detail: err.message,
      });
      setIsInspecting(false);
    }
  };

  const handleTriggerSpeedTest = async () => {
    setIsSpeedTesting(true);
    try {
      setSpeedTestCount((prev) => prev + 1);
      setLastSpeedTestTime(new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }));
      const res = await api.runSpeedTest(false, 15);
      await fetchStatus();
      pushActivity({
        level: 'success',
        source: '测速',
        title: `全量 10M 测速完成（${res.count || 0} 台在线节点）`,
      });
    } catch (err: any) {
      pushActivity({
        level: 'error',
        source: '测速',
        title: '全量 10M 测速失败',
        detail: err.message,
      });
    } finally {
      setIsSpeedTesting(false);
    }
  };

  const handleRetestSingleNode = async (node: NodeProbeResult) => {
    const nodeName = node.name || node.id;
    try {
      const resp = await api.probeBatch([node.original_url], node.is_self ?? true, false, true, 8, true);
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
        clearNodeError(nodeName);
        pushActivity({
          level: 'success',
          source: '单点重测',
          nodeName,
          title: `${nodeName} 重测完成`,
          detail: updated.online ? `在线 · ${updated.latency_ms}ms` : '探测离线',
        });
      }
    } catch (err: any) {
      pushActivity({
        level: 'error',
        source: '单点重测',
        nodeName,
        title: `${nodeName} 重测失败`,
        detail: err.message,
      });
    }
  };

  const handleSpeedTestSingleNode = async (node: NodeProbeResult) => {
    const targetName = node.name || node.id;
    try {
      const resp = await api.runSpeedTest(false, 15, targetName);
      if (resp.nodes && resp.nodes.length > 0) {
        setSelectedReportNode(resp.nodes[0]);
      }
      clearNodeError(targetName);
      await fetchStatus();
      pushActivity({
        level: 'success',
        source: '单节点测速',
        nodeName: targetName,
        title: `${targetName} 10M 测速完成`,
      });
    } catch (err: any) {
      pushActivity({
        level: 'error',
        source: '单节点测速',
        nodeName: targetName,
        title: `${targetName} 10M 测速失败`,
        detail: err.message,
      });
    }
  };

  const handleOpenHealingWizard = (initialNode?: string) => {
    setHealingInitialNode(initialNode);
    setHealingWizardOpen(true);
  };

  const allNodeNames = Object.values(statusMap).map((s) => s.config.name);

  return (
    <div className="min-h-screen bg-[#f6f8fa] flex flex-col font-sans pb-12">
      <Header
        currentTab={currentTab}
        onTabChange={(t) => onTabChange(t)}
        onLogout={onLogout}
        isInspecting={isInspecting}
      />

      <main className="flex-1 max-w-7xl w-full mx-auto px-4 sm:px-6 lg:px-8 py-5">
        {currentTab === 'overview' && (
          <OverviewWorkspace
            statusMap={statusMap}
            blockedNodes={blockedNodes}
            speedTestCount={speedTestCount}
            lastSpeedTestTime={lastSpeedTestTime}
            splitCheckCount={splitCheckCount}
            lastSplitCheckTime={lastSplitCheckTime}
            pollingInterval={pollingInterval}
            onPollingIntervalChange={setPollingInterval}
            onTriggerInspection={handleTriggerInspection}
            onTriggerSpeedTest={handleTriggerSpeedTest}
            isSpeedTesting={isSpeedTesting}
            isInspecting={isInspecting}
            onOpenReport={(node, initialTab = 'overview') => {
              setSelectedReportNode(node);
              setReportInitialTab(initialTab);
            }}
            onRetestNode={handleRetestSingleNode}
            onSpeedTestNode={handleSpeedTestSingleNode}
            onOpenHealingWizard={handleOpenHealingWizard}
          />
        )}

        {currentTab === 'pipeline' && <PipelineHub onRefreshGlobal={fetchStatus} />}
        {currentTab === 'vps' && <VPSCenter onOpenHealingWizard={handleOpenHealingWizard} />}
        {currentTab === 'settings' && <SettingsLogs focusSection={focusSection} />}
      </main>

      <ReportDrawer
        node={selectedReportNode}
        initialTab={reportInitialTab}
        onClose={() => setSelectedReportNode(null)}
        onRetest={handleRetestSingleNode}
        onSpeedTest={handleSpeedTestSingleNode}
        onHealPort={handleOpenHealingWizard}
        isTestingThisNode={false}
      />

      {healingWizardOpen && (
        <HealingWizard
          initialNodeName={healingInitialNode}
          availableNodes={allNodeNames}
          onClose={() => setHealingWizardOpen(false)}
          onSuccess={(res) => {
            pushActivity({
              level: 'success',
              source: '端口自愈',
              nodeName: res.node_name,
              title: `${res.node_name} 换端口自愈完成`,
              detail: res.new_port ? `新端口 :${res.new_port}` : res.message,
            });
            fetchStatus();
          }}
        />
      )}

      <ActivityConsoleDrawer />
    </div>
  );
};

export const App: React.FC = () => {
  const [token, setToken] = useState<string>(() => getStoredToken());
  const [currentTab, setCurrentTab] = useState<WorkspaceTab>('overview');
  const [focusSection, setFocusSection] = useState<string | null>(null);

  const handleLogout = useCallback(() => {
    removeStoredToken();
    setToken('');
  }, []);

  const handleNavigate = useCallback((tab: WorkspaceTab, section?: string) => {
    setCurrentTab(tab);
    setFocusSection(section || null);
  }, []);

  if (!token) {
    return <LoginGate onSuccess={(newToken) => setToken(newToken)} />;
  }

  return (
    <FeedbackProvider onNavigate={handleNavigate}>
      <MainShell
        token={token}
        onLogout={handleLogout}
        currentTab={currentTab}
        focusSection={focusSection}
        onTabChange={handleNavigate}
      />
    </FeedbackProvider>
  );
};

export default App;
