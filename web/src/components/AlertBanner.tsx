import React from 'react';
import { ShieldAlert, ArrowRight } from 'lucide-react';

interface BlockedNodeInfo {
  name: string;
  blocked_port: number;
  status_text?: string;
  target_ip?: string;
}

interface AlertBannerProps {
  blockedNodes: BlockedNodeInfo[];
  onOpenHealingWizard: (initialNode?: string) => void;
}

export const AlertBanner: React.FC<AlertBannerProps> = ({
  blockedNodes,
  onOpenHealingWizard,
}) => {
  if (!blockedNodes || blockedNodes.length === 0) {
    return null;
  }

  return (
    <div className="bg-red-50 border-b border-red-200 px-4 py-3 sm:px-6 lg:px-8">
      <div className="max-w-7xl mx-auto flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
        <div className="flex items-start sm:items-center space-x-3">
          <div className="p-1.5 bg-red-100 rounded-lg text-red-600 flex-shrink-0 mt-0.5 sm:mt-0">
            <ShieldAlert className="w-5 h-5" />
          </div>
          <div>
            <h4 className="text-sm font-semibold text-red-900 flex items-center">
              <span>发现 {blockedNodes.length} 个节点端口疑似遭 GFW 阻断</span>
              <span className="ml-2 inline-flex items-center px-2 py-0.5 rounded text-2xs font-mono font-medium bg-red-200 text-red-800">
                PORT_BLOCKED
              </span>
            </h4>
            <p className="text-xs text-red-700 mt-0.5">
              海外握手正常但国内 TCP 握手失败，受损节点：
              {blockedNodes.map((n, idx) => (
                <span key={idx} className="font-mono font-semibold ml-1 bg-red-100 px-1 py-0.5 rounded text-2xs">
                  {n.name} (端口: {n.blocked_port})
                </span>
              ))}
            </p>
          </div>
        </div>

        <div className="flex-shrink-0">
          <button
            onClick={() => onOpenHealingWizard(blockedNodes[0]?.name)}
            className="w-full sm:w-auto inline-flex items-center justify-center px-4 py-2 text-xs font-semibold rounded-lg text-white bg-red-600 hover:bg-red-700 shadow-xs transition"
          >
            一键启动自愈向导
            <ArrowRight className="w-3.5 h-3.5 ml-1.5" />
          </button>
        </div>
      </div>
    </div>
  );
};
