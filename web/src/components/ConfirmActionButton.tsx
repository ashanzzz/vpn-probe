import React, { useState, useEffect, useRef } from 'react';
import { Check, X } from 'lucide-react';

interface ConfirmActionButtonProps {
  onConfirm: () => void | Promise<void>;
  confirmLabel?: string;
  disabled?: boolean;
  title?: string;
  className?: string;
  children: React.ReactNode;
}

/**
 * 原位双步确认按钮：彻底替代阻塞式 window.confirm()
 * 第一次点击在原位切为“确定 / 取消”微控件，5 秒无操作自动复原。
 */
export const ConfirmActionButton: React.FC<ConfirmActionButtonProps> = ({
  onConfirm,
  confirmLabel = '确认?',
  disabled = false,
  title,
  className = '',
  children,
}) => {
  const [armed, setArmed] = useState(false);
  const [busy, setBusy] = useState(false);
  const timerRef = useRef<number | null>(null);

  useEffect(() => {
    return () => {
      if (timerRef.current) window.clearTimeout(timerRef.current);
    };
  }, []);

  const arm = (e: React.MouseEvent) => {
    e.stopPropagation();
    if (disabled || busy) return;
    setArmed(true);
    if (timerRef.current) window.clearTimeout(timerRef.current);
    timerRef.current = window.setTimeout(() => setArmed(false), 5000);
  };

  const cancel = (e: React.MouseEvent) => {
    e.stopPropagation();
    if (timerRef.current) window.clearTimeout(timerRef.current);
    setArmed(false);
  };

  const confirm = async (e: React.MouseEvent) => {
    e.stopPropagation();
    if (timerRef.current) window.clearTimeout(timerRef.current);
    setArmed(false);
    setBusy(true);
    try {
      await onConfirm();
    } finally {
      setBusy(false);
    }
  };

  if (armed) {
    return (
      <span
        onClick={(e) => e.stopPropagation()}
        className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded-lg bg-rose-50 border border-rose-300 text-[11px] font-semibold text-rose-800 shadow-2xs"
      >
        <span className="px-1">{confirmLabel}</span>
        <button
          type="button"
          onClick={confirm}
          disabled={busy}
          className="inline-flex items-center px-1.5 py-0.5 rounded bg-rose-600 hover:bg-rose-700 text-white text-[10px]"
        >
          <Check className="w-3 h-3 mr-0.5" />
          确定
        </button>
        <button
          type="button"
          onClick={cancel}
          className="inline-flex items-center px-1 py-0.5 rounded bg-white border border-slate-200 hover:bg-slate-100 text-slate-600 text-[10px]"
        >
          <X className="w-3 h-3" />
        </button>
      </span>
    );
  }

  return (
    <button
      type="button"
      onClick={arm}
      disabled={disabled || busy}
      title={title}
      className={className}
    >
      {children}
    </button>
  );
};
