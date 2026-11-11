import React, { useState, useEffect } from 'react';
import { useNavigate, useLocation } from 'react-router-dom';

import { Logo } from '@/components/icons';
import { siteConfig } from '@/config/site';

interface TabItem {
  path: string;
  label: string;
  icon: React.ReactNode;
  adminOnly?: boolean;
}



export default function H5Layout({
  children,
}: {
  children: React.ReactNode;
}) {
  const navigate = useNavigate();
  const location = useLocation();
  const [isAdmin, setIsAdmin] = useState(false);

  // Tabbar配置
  const tabItems: TabItem[] = [
    {
      path: '/dashboard',
      label: '总览',
      icon: (
        <svg className="w-[22px] h-[22px]" fill="none" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.9} viewBox="0 0 24 24">
          <rect height="9" rx="1.5" width="7" x="3" y="3" /><rect height="5" rx="1.5" width="7" x="14" y="3" /><rect height="9" rx="1.5" width="7" x="14" y="12" /><rect height="5" rx="1.5" width="7" x="3" y="16" />
        </svg>
      ),
    },
    {
      path: '/forward',
      label: '转发',
      icon: (
        <svg className="w-[22px] h-[22px]" fill="none" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.9} viewBox="0 0 24 24">
          <path d="M8 3 4 7l4 4" /><path d="M4 7h16" /><path d="m16 21 4-4-4-4" /><path d="M20 17H4" />
        </svg>
      ),
    },
    {
      path: '/tunnel',
      label: '隧道',
      icon: (
        <svg className="w-[22px] h-[22px]" fill="none" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.9} viewBox="0 0 24 24">
          <path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71" /><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71" />
        </svg>
      ),
      adminOnly: true,
    },
    {
      path: '/node',
      label: '节点',
      icon: (
        <svg className="w-[22px] h-[22px]" fill="none" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.9} viewBox="0 0 24 24">
          <rect height="8" rx="2" width="20" x="2" y="2" /><rect height="8" rx="2" width="20" x="2" y="14" /><path d="M6 6h.01" /><path d="M6 18h.01" />
        </svg>
      ),
      adminOnly: true,
    },
    {
      path: '/profile',
      label: '我的',
      icon: (
        <svg className="w-[22px] h-[22px]" fill="none" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.9} viewBox="0 0 24 24">
          <circle cx="12" cy="8" r="4" /><path d="M4 21a8 8 0 0 1 16 0" />
        </svg>
      ),
    },
  ];

  useEffect(() => {

    // 兼容处理：如果没有admin字段，根据role_id判断（0为管理员）
    let adminFlag = localStorage.getItem('admin') === 'true';
    if (localStorage.getItem('admin') === null) {
      const roleId = parseInt(localStorage.getItem('role_id') || '1', 10);
      adminFlag = roleId === 0;
      // 补充设置admin字段，避免下次再次判断
      localStorage.setItem('admin', adminFlag.toString());
    }
    

    setIsAdmin(adminFlag);
  }, []);

  // Tab点击处理
  const handleTabClick = (path: string) => {
    navigate(path);
  };

  // 过滤tab项（根据权限）
  const filteredTabItems = tabItems.filter(item => 
    !item.adminOnly || isAdmin
  );

  // 路由切换时回到页面顶部，避免上一页的滚动位置遗留
  useEffect(() => {
    try {
      window.scrollTo({ top: 0, left: 0, behavior: 'auto' });
    } catch (e) {
      window.scrollTo(0, 0);
    }
    document.body.scrollTop = 0;
    document.documentElement.scrollTop = 0;
  }, [location.pathname]);

  return (
    <div className="fz-canvas flex flex-col min-h-screen">
      {/* 顶部导航栏 */}
      <header className="fz-bar sticky top-[calc(var(--safe-area-top)+8px)] mt-[calc(var(--safe-area-top)+8px)] mx-3 h-12 rounded-full flex-shrink-0 flex items-center justify-between px-4 z-20">
        <div className="flex items-center gap-2">
          <Logo size={22} />
          <h1 className="text-sm font-extrabold tracking-tight text-foreground">{siteConfig.name}</h1>
        </div>

        <div className="flex items-center gap-2">
        </div>
      </header>

      {/* 主内容区域 */}
      <main className="flex-1">
        {children}
      </main>

      {/* 用于给固定 Tabbar 腾出空间的占位元素 */}
      <div aria-hidden className="h-24 safe-bottom" />

      {/* 底部Tabbar */}
      <nav className="pointer-events-none fixed inset-x-0 bottom-0 z-30 flex justify-center px-3 pb-[calc(var(--safe-area-bottom)+12px)]">
        <div className="fz-bar fz-tabbar w-full max-w-md">
          {filteredTabItems.map((item) => {
            const isActive = location.pathname === item.path;

            return (
              <button
                key={item.path}
                className="fz-tab"
                data-active={isActive}
                onClick={() => handleTabClick(item.path)}
              >
                {item.icon}
                <span>{item.label}</span>
              </button>
            );
          })}
        </div>
      </nav>

    </div>
  );
}
