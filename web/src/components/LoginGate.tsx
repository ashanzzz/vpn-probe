import React, { useState } from 'react';
import { Shield, Key, ArrowRight, AlertCircle, Loader2 } from 'lucide-react';
import { api, setStoredToken } from '../api/client';

interface LoginGateProps {
  onSuccess: (token: string) => void;
}

export const LoginGate: React.FC<LoginGateProps> = ({ onSuccess }) => {
  const [token, setToken] = useState('');
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const cleanToken = token.trim();
    if (!cleanToken) {
      setError('请输入 Admin 鉴权 Token');
      return;
    }

    setLoading(true);
    setError(null);

    try {
      const ok = await api.verifyAuth(cleanToken);
      if (ok) {
        setStoredToken(cleanToken);
        onSuccess(cleanToken);
      } else {
        setError('Token 验证失败：权限不足或密钥错误');
      }
    } catch (err: any) {
      setError(`验证错误: ${err.message || '网络连接失败'}`);
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="min-h-screen bg-[#f6f8fa] flex flex-col justify-center items-center px-4 sm:px-6 lg:px-8">
      <div className="sm:mx-auto sm:w-full sm:max-w-md">
        <div className="flex justify-center mb-4">
          <div className="w-14 h-14 bg-blue-600 rounded-xl shadow-md flex items-center justify-center text-white">
            <Shield className="w-8 h-8" />
          </div>
        </div>
        <h2 className="text-center text-2xl font-bold tracking-tight text-slate-900">
          vpn-probe 管理控制台
        </h2>
        <p className="mt-2 text-center text-sm text-slate-500">
          自包含代理检测 · 双模 MCP · SubStore 中枢 · VPS 智能换端口自愈
        </p>
      </div>

      <div className="mt-8 sm:mx-auto sm:w-full sm:max-w-md">
        <div className="bg-white py-8 px-6 shadow-sm border border-slate-200 rounded-xl sm:px-10">
          <form className="space-y-6" onSubmit={handleSubmit}>
            <div>
              <label htmlFor="token" className="block text-sm font-medium text-slate-700">
                Admin 鉴权 Token
              </label>
              <div className="mt-1 relative rounded-md shadow-sm">
                <div className="absolute inset-y-0 left-0 pl-3 flex items-center pointer-events-none text-slate-400">
                  <Key className="w-4 h-4" />
                </div>
                <input
                  id="token"
                  name="token"
                  type="password"
                  autoComplete="current-password"
                  required
                  value={token}
                  onChange={(e) => setToken(e.target.value)}
                  placeholder="请输入配置文件中的 auth_token"
                  className="block w-full pl-10 pr-3 py-2.5 border border-slate-300 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-blue-600 focus:border-blue-600 font-mono"
                />
              </div>
              <p className="mt-2 text-xs text-slate-400">
                🔒 成功验证后将永久记忆并自动解锁所有节点明文链接与自愈控制权限。
              </p>
            </div>

            {error && (
              <div className="rounded-lg bg-red-50 p-3 flex items-start space-x-2 border border-red-200 text-sm text-red-700">
                <AlertCircle className="w-5 h-5 flex-shrink-0 text-red-500 mt-0.5" />
                <span>{error}</span>
              </div>
            )}

            <div>
              <button
                type="submit"
                disabled={loading}
                className="w-full flex justify-center items-center py-2.5 px-4 border border-transparent rounded-lg shadow-sm text-sm font-medium text-white bg-blue-600 hover:bg-blue-700 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-blue-500 disabled:opacity-60 transition"
              >
                {loading ? (
                  <>
                    <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                    验证凭证中...
                  </>
                ) : (
                  <>
                    进入工作台
                    <ArrowRight className="w-4 h-4 ml-2" />
                  </>
                )}
              </button>
            </div>
          </form>
        </div>

        <div className="mt-6 text-center text-xs text-slate-400">
          vpn-probe Modular Monolith · v1.0.0
        </div>
      </div>
    </div>
  );
};
