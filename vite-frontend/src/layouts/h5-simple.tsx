import React from 'react';
import { useNavigate, useLocation } from 'react-router-dom';
import { Button } from "@heroui/button";

import { Logo } from '@/components/icons';
import { siteConfig } from '@/config/site';

export default function H5SimpleLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const navigate = useNavigate();
  const location = useLocation();
  
  // 路由切换时回到顶部，避免上一页滚动位置保留
  React.useEffect(() => {
    try {
      window.scrollTo({ top: 0, left: 0, behavior: 'auto' });
    } catch (e) {
      window.scrollTo(0, 0);
    }
    document.body.scrollTop = 0;
    document.documentElement.scrollTop = 0;
  }, [location.pathname]);

  const handleBack = () => {
    navigate('/profile');
  };

  return (
    <div className="fz-canvas flex flex-col min-h-screen">
      {/* 顶部导航栏 */}
      <header className="fz-bar sticky top-[calc(var(--safe-area-top)+8px)] mt-[calc(var(--safe-area-top)+8px)] mx-3 h-12 rounded-full flex-shrink-0 flex items-center justify-between px-4 z-20">
        <div className="flex items-center gap-2">
          <Button
            isIconOnly
            variant="light"
            size="sm"
            onPress={handleBack}
          >
            <svg className="w-5 h-5" fill="none" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.9} viewBox="0 0 24 24">
              <path d="M15 18l-6-6 6-6" />
            </svg>
          </Button>
          <Logo size={22} />
          <h1 className="text-sm font-extrabold tracking-tight text-foreground">{siteConfig.name}</h1>
        </div>

        <div className="flex items-center gap-2">
        </div>
      </header>

      {/* 主内容区域 */}
      <main className="flex-1 pb-0">
        {children}
      </main>
    </div>
  );
}
