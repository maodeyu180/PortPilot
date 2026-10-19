import React, { useLayoutEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { Copy, Trash2 } from 'lucide-react';

export default function NodeMenu({ anchor, label, onClose, onExport, onDelete }) {
  const menuRef = useRef(null);
  const [position, setPosition] = useState(null);
  const placed = position !== null;

  useLayoutEffect(() => {
    function place() {
      if (!anchor.isConnected) { onClose(); return; }
      const rect = anchor.getBoundingClientRect();
      const menu = menuRef.current;
      const margin = 8;
      const gap = 6;
      const width = document.documentElement.clientWidth;
      const height = window.innerHeight;
      const top = rect.bottom + gap + menu.offsetHeight <= height - margin
        ? rect.bottom + gap
        : rect.top - gap - menu.offsetHeight;
      setPosition({
        left: Math.max(margin, Math.min(rect.right - menu.offsetWidth, width - menu.offsetWidth - margin)),
        top: Math.max(margin, Math.min(top, height - menu.offsetHeight - margin)),
        maxHeight: height - margin * 2,
      });
    }
    function outside(event) {
      if (!menuRef.current.contains(event.target) && !anchor.contains(event.target)) onClose();
    }
    function scroll(event) {
      if (!menuRef.current.contains(event.target)) onClose();
    }
    place();
    document.addEventListener('pointerdown', outside, true);
    window.addEventListener('scroll', scroll, true);
    window.addEventListener('resize', place);
    return () => {
      document.removeEventListener('pointerdown', outside, true);
      window.removeEventListener('scroll', scroll, true);
      window.removeEventListener('resize', place);
    };
  }, [anchor, onClose]);

  useLayoutEffect(() => {
    if (placed) menuRef.current.querySelector('[role="menuitem"]')?.focus({ preventScroll: true });
  }, [placed]);

  function keyboard(event) {
    if (event.key === 'Escape' || event.key === 'Tab') {
      if (event.key === 'Escape') event.preventDefault();
      anchor.focus({ preventScroll: true }); onClose(); return;
    }
    const items = [...menuRef.current.querySelectorAll('[role="menuitem"]')];
    let index = items.indexOf(document.activeElement);
    if (event.key === 'ArrowDown') index = (index + 1) % items.length;
    else if (event.key === 'ArrowUp') index = (index - 1 + items.length) % items.length;
    else if (event.key === 'Home') index = 0;
    else if (event.key === 'End') index = items.length - 1;
    else return;
    event.preventDefault(); items[index].focus();
  }

  return createPortal(<div ref={menuRef} id="node-actions-menu" className="row-menu" role="menu" aria-label={`${label} 操作`} onKeyDown={keyboard} style={position || { visibility: 'hidden' }}>
    <button role="menuitem" onClick={() => { onClose(); onExport(); }}><Copy size={15} />导出地址</button>
    <button role="menuitem" className="danger-text" onClick={() => { onClose(); onDelete(); }}><Trash2 size={15} />删除节点</button>
  </div>, document.body);
}
