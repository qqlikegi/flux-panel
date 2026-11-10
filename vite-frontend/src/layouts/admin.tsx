import React, { useState, useEffect } from 'react';
import { useNavigate, useLocation } from 'react-router-dom';
import { Button } from "@heroui/button";
import { Dropdown, DropdownTrigger, DropdownMenu, DropdownItem } from "@heroui/dropdown";
import { Modal, ModalContent, ModalHeader, ModalBody, ModalFooter, useDisclosure } from "@heroui/modal";
import { Input } from "@heroui/input";
import { toast } from 'react-hot-toast';

import { Logo } from '@/components/icons';
import { updatePassword } from '@/api';
import { safeLogout } from '@/utils/logout';
import { siteConfig } from '@/config/site';

interface MenuItem {
  path: string;
  label: string;
  icon: React.ReactNode;
  adminOnly?: boolean;
}

interface MenuGroup {
  title?: string;
  items: MenuItem[];
}

interface PasswordForm {
  newUsername: string;
  currentPassword: string;
  newPassword: string;
  confirmPassword: string;
}

// 线性图标（24x24，描边）
const NavIcon = ({ children }: { children: React.ReactNode }) => (
  <svg
    className="w-[18px] h-[18px] flex-shrink-0"
    fill="none"
    stroke="currentColor"
    strokeLinecap="round"
    strokeLinejoin="round"
    strokeWidth={1.8}
    viewBox="0 0 24 24"
  >
    {children}
  </svg>
);

const SIDEBAR_KEY = 'fz_sidebar_collapsed';

// 菜单分组配置
const menuGroups: MenuGroup[] = [
  {
    items: [
      {
        path: '/dashboard',
        label: '数据总览',
        icon: (
          <NavIcon>
            <rect height="9" rx="1.5" width="7" x="3" y="3" />
            <rect height="5" rx="1.5" width="7" x="14" y="3" />
            <rect height="9" rx="1.5" width="7" x="14" y="12" />
            <rect height="5" rx="1.5" width="7" x="3" y="16" />
          </NavIcon>
        ),
      },
    ],
  },
  {
    title: '转发',
    items: [
      {
        path: '/forward',
        label: '转发管理',
        icon: (
          <NavIcon>
            <path d="M8 3 4 7l4 4" />
            <path d="M4 7h16" />
            <path d="m16 21 4-4-4-4" />
            <path d="M20 17H4" />
          </NavIcon>
        ),
      },
      {
        path: '/tunnel',
        label: '隧道管理',
        adminOnly: true,
        icon: (
          <NavIcon>
            <path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71" />
            <path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71" />
          </NavIcon>
        ),
      },
    ],
  },
  {
    title: '节点',
    items: [
      {
        path: '/node',
        label: '节点监控',
        adminOnly: true,
        icon: (
          <NavIcon>
            <rect height="8" rx="2" width="20" x="2" y="2" />
            <rect height="8" rx="2" width="20" x="2" y="14" />
            <path d="M6 6h.01" />
            <path d="M6 18h.01" />
          </NavIcon>
        ),
      },
      {
        path: '/limit',
        label: '限速管理',
        adminOnly: true,
        icon: (
          <NavIcon>
            <path d="m12 14 4-4" />
            <path d="M3.34 19a10 10 0 1 1 17.32 0" />
          </NavIcon>
        ),
      },
    ],
  },
  {
    title: '系统',
    items: [
      {
        path: '/user',
        label: '用户管理',
        adminOnly: true,
        icon: (
          <NavIcon>
            <path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2" />
            <circle cx="9" cy="7" r="4" />
            <path d="M22 21v-2a4 4 0 0 0-3-3.87" />
            <path d="M16 3.13a4 4 0 0 1 0 7.75" />
          </NavIcon>
        ),
      },
      {
        path: '/config',
        label: '系统配置',
        adminOnly: true,
        icon: (
          <NavIcon>
            <path d="M4 21v-7" />
            <path d="M4 10V3" />
            <path d="M12 21v-9" />
            <path d="M12 8V3" />
            <path d="M20 21v-5" />
            <path d="M20 12V3" />
            <path d="M2 14h4" />
            <path d="M10 8h4" />
            <path d="M18 16h4" />
          </NavIcon>
        ),
      },
    ],
  },
];

export default function AdminLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const navigate = useNavigate();
  const location = useLocation();
  const { isOpen, onOpen, onOpenChange } = useDisclosure();

  const [isMobile, setIsMobile] = useState(false);
  const [mobileMenuVisible, setMobileMenuVisible] = useState(false);
  const [collapsed, setCollapsed] = useState(() => {
    try {
      return localStorage.getItem(SIDEBAR_KEY) === '1';
    } catch {
      return false;
    }
  });
  const [username, setUsername] = useState('');
  const [isAdmin, setIsAdmin] = useState(false);
  const [passwordLoading, setPasswordLoading] = useState(false);
  const [passwordForm, setPasswordForm] = useState<PasswordForm>({
    newUsername: '',
    currentPassword: '',
    newPassword: '',
    confirmPassword: ''
  });

  // 检查移动端
  const checkMobile = () => {
    setIsMobile(window.innerWidth <= 768);
    if (window.innerWidth > 768) {
      setMobileMenuVisible(false);
    }
  };

  useEffect(() => {
    // 获取用户信息
    const name = localStorage.getItem('name') || 'Admin';

    // 兼容处理：如果没有admin字段，根据role_id判断（0为管理员）
    let adminFlag = localStorage.getItem('admin') === 'true';
    if (localStorage.getItem('admin') === null) {
      const roleId = parseInt(localStorage.getItem('role_id') || '1', 10);
      adminFlag = roleId === 0;
      // 补充设置admin字段，避免下次再次判断
      localStorage.setItem('admin', adminFlag.toString());
    }

    setUsername(name);
    setIsAdmin(adminFlag);

    // 响应式检查
    checkMobile();
    window.addEventListener('resize', checkMobile);

    return () => {
      window.removeEventListener('resize', checkMobile);
    };
  }, []);

  // 退出登录
  const handleLogout = () => {
    safeLogout();
    navigate('/');
  };

  // 切换移动端菜单
  const toggleMobileMenu = () => {
    setMobileMenuVisible(!mobileMenuVisible);
  };

  // 隐藏移动端菜单
  const hideMobileMenu = () => {
    setMobileMenuVisible(false);
  };

  // 收起 / 展开侧栏
  const toggleCollapsed = () => {
    setCollapsed((prev) => {
      const next = !prev;
      try {
        localStorage.setItem(SIDEBAR_KEY, next ? '1' : '0');
      } catch {
        // 忽略存储失败
      }
      return next;
    });
  };

  // 菜单点击处理
  const handleMenuClick = (path: string) => {
    navigate(path);
    if (isMobile) {
      hideMobileMenu();
    }
  };

  // 密码表单验证
  const validatePasswordForm = (): boolean => {
    if (!passwordForm.newUsername.trim()) {
      toast.error('请输入新用户名');
      return false;
    }
    if (passwordForm.newUsername.length < 3) {
      toast.error('用户名长度至少3位');
      return false;
    }
    if (!passwordForm.currentPassword) {
      toast.error('请输入当前密码');
      return false;
    }
    if (!passwordForm.newPassword) {
      toast.error('请输入新密码');
      return false;
    }
    if (passwordForm.newPassword.length < 6) {
      toast.error('新密码长度不能少于6位');
      return false;
    }
    if (passwordForm.newPassword !== passwordForm.confirmPassword) {
      toast.error('两次输入密码不一致');
      return false;
    }
    return true;
  };

  // 提交密码修改
  const handlePasswordSubmit = async () => {
    if (!validatePasswordForm()) return;

    setPasswordLoading(true);
    try {
      const response = await updatePassword(passwordForm);
      if (response.code === 0) {
        toast.success('密码修改成功，请重新登录');
        onOpenChange();
        handleLogout();
      } else {
        toast.error(response.msg || '密码修改失败');
      }
    } catch (error) {
      toast.error('修改密码时发生错误');
      console.error('修改密码错误:', error);
    } finally {
      setPasswordLoading(false);
    }
  };

  // 重置密码表单
  const resetPasswordForm = () => {
    setPasswordForm({
      newUsername: '',
      currentPassword: '',
      newPassword: '',
      confirmPassword: ''
    });
  };

  // 过滤菜单项（根据权限），空分组不显示
  const visibleGroups = menuGroups
    .map((group) => ({
      ...group,
      items: group.items.filter((item) => !item.adminOnly || isAdmin),
    }))
    .filter((group) => group.items.length > 0);

  const currentItem = menuGroups
    .flatMap((group) => group.items)
    .find((item) => item.path === location.pathname);
  const pageTitle = currentItem?.label || '';

  // 移动端抽屉始终展开显示文字
  const isCollapsed = collapsed && !isMobile;

  return (
    <div className={`fz-canvas flex ${isMobile ? 'min-h-screen' : 'h-screen'}`}>
      {/* 移动端遮罩层 */}
      {isMobile && mobileMenuVisible && (
        <div
          className="fixed inset-0 backdrop-blur-sm bg-white/50 dark:bg-black/40 z-40"
          onClick={hideMobileMenu}
        />
      )}

      {/* 左侧菜单栏 */}
      <aside
        className={`
          fz-chrome
          ${isMobile ? 'fixed top-0 left-0 h-screen' : 'relative h-full'}
          ${isMobile && !mobileMenuVisible ? '-translate-x-full' : 'translate-x-0'}
          ${isCollapsed ? 'w-[68px]' : 'w-[248px]'}
          z-50 flex flex-col flex-shrink-0
          transition-[width,transform] duration-300 ease-in-out
        `}
      >
        {/* Logo 区域 */}
        <div className={`h-16 flex items-center flex-shrink-0 ${isCollapsed ? 'justify-center px-2' : 'justify-between pl-5 pr-3'}`}>
          {!isCollapsed && (
            <div className="flex items-center gap-2 min-w-0">
              <Logo size={26} />
              <span className="text-[15px] font-extrabold tracking-tight text-foreground truncate">
                {siteConfig.name}
              </span>
            </div>
          )}
          {!isMobile && (
            <button
              aria-label={isCollapsed ? '展开侧栏' : '收起侧栏'}
              className="w-8 h-8 rounded-[10px] flex items-center justify-center text-default-500 hover:text-foreground hover:bg-[var(--fz-hover)] transition-colors"
              title={isCollapsed ? '展开侧栏' : '收起侧栏'}
              onClick={toggleCollapsed}
            >
              <NavIcon>
                <rect height="18" rx="3" width="18" x="3" y="3" />
                <path d="M9 3v18" />
              </NavIcon>
            </button>
          )}
        </div>

        {/* 菜单导航 */}
        <nav className="flex-1 px-3.5 pb-4 overflow-y-auto overflow-x-hidden">
          {visibleGroups.map((group, index) => (
            <div key={group.title || index}>
              {group.title && !isCollapsed && (
                <div className="fz-nav-group">{group.title}</div>
              )}
              {group.title && isCollapsed && (
                <div className="my-2 mx-2 h-px bg-[var(--fz-hairline)]" />
              )}
              <ul className="space-y-0.5">
                {group.items.map((item) => {
                  const isActive = location.pathname === item.path;

                  return (
                    <li key={item.path}>
                      <button
                        className={`fz-nav-item ${isCollapsed ? 'justify-center !px-0' : ''}`}
                        data-active={isActive}
                        title={isCollapsed ? item.label : undefined}
                        onClick={() => handleMenuClick(item.path)}
                      >
                        {item.icon}
                        {!isCollapsed && <span>{item.label}</span>}
                      </button>
                    </li>
                  );
                })}
              </ul>
            </div>
          ))}
        </nav>

        {/* 底部版本信息 */}
        {!isCollapsed && (
          <div className="px-5 py-4 flex-shrink-0 text-xs text-default-400 flex items-center justify-between">
            <a
              className="hover:text-foreground transition-colors"
              href="https://github.com/qqlikegi/forwardz"
              rel="noopener noreferrer"
              target="_blank"
            >
              ForwardZ
            </a>
            <span>v{siteConfig.version}</span>
          </div>
        )}
      </aside>

      {/* 主内容区域 */}
      <div className={`flex flex-col flex-1 min-w-0 ${isMobile ? 'min-h-0' : 'h-full overflow-hidden'}`}>
        {/* 顶部栏 */}
        <header className="h-16 flex-shrink-0 flex items-center justify-between px-4 lg:px-8 relative z-10">
          <div className="flex items-center gap-3 min-w-0">
            {/* 移动端菜单按钮 */}
            {isMobile && (
              <Button isIconOnly variant="light" onPress={toggleMobileMenu}>
                <svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path d="M4 6h16M4 12h16M4 18h16" strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} />
                </svg>
              </Button>
            )}
            <span className="text-sm font-semibold text-foreground truncate">{pageTitle}</span>
          </div>

          <div className="flex items-center gap-3">
            {/* 用户菜单 */}
            <Dropdown placement="bottom-end">
              <DropdownTrigger>
                <button className="fz-pill h-10 pl-1.5 pr-3 flex items-center gap-2 text-sm font-medium text-foreground outline-none transition-transform active:scale-[0.98]">
                  <span className="w-7 h-7 rounded-full bg-default-100 text-default-600 flex items-center justify-center text-xs font-bold uppercase">
                    {(username || 'A').slice(0, 1)}
                  </span>
                  <span className="max-w-[140px] truncate">{username}</span>
                  <svg className="w-4 h-4 text-default-500" fill="none" stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} viewBox="0 0 24 24">
                    <path d="m6 9 6 6 6-6" />
                  </svg>
                </button>
              </DropdownTrigger>
              <DropdownMenu aria-label="用户菜单">
                <DropdownItem
                  key="change-password"
                  startContent={
                    <NavIcon>
                      <circle cx="7.5" cy="15.5" r="5.5" />
                      <path d="m21 2-9.6 9.6" />
                      <path d="m15.5 7.5 3 3L22 7l-3-3" />
                    </NavIcon>
                  }
                  onPress={onOpen}
                >
                  修改密码
                </DropdownItem>
                <DropdownItem
                  key="logout"
                  className="text-danger"
                  color="danger"
                  startContent={
                    <NavIcon>
                      <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" />
                      <path d="m16 17 5-5-5-5" />
                      <path d="M21 12H9" />
                    </NavIcon>
                  }
                  onPress={handleLogout}
                >
                  退出登录
                </DropdownItem>
              </DropdownMenu>
            </Dropdown>
          </div>
        </header>

        {/* 主内容 */}
        <main className={`flex-1 ${isMobile ? '' : 'overflow-y-auto'}`}>
          <div className="mx-auto w-full max-w-[1400px] lg:px-4">
            {pageTitle && (
              <h1 className="fz-page-title px-3 lg:px-6 pt-6">{pageTitle}</h1>
            )}
            {children}
          </div>
        </main>
      </div>

      {/* 修改密码弹窗 */}
      <Modal
        backdrop="blur"
        isOpen={isOpen}
        placement="center"
        scrollBehavior="outside"
        size="2xl"
        onOpenChange={() => {
          onOpenChange();
          resetPasswordForm();
        }}
      >
        <ModalContent>
          {(onClose: () => void) => (
            <>
              <ModalHeader className="flex flex-col gap-1">修改密码</ModalHeader>
              <ModalBody>
                <div className="space-y-4">
                  <Input
                    label="新用户名"
                    placeholder="请输入新用户名（至少3位）"
                    value={passwordForm.newUsername}
                    variant="bordered"
                    onChange={(e: React.ChangeEvent<HTMLInputElement>) => setPasswordForm(prev => ({ ...prev, newUsername: e.target.value }))}
                  />
                  <Input
                    label="当前密码"
                    placeholder="请输入当前密码"
                    type="password"
                    value={passwordForm.currentPassword}
                    variant="bordered"
                    onChange={(e: React.ChangeEvent<HTMLInputElement>) => setPasswordForm(prev => ({ ...prev, currentPassword: e.target.value }))}
                  />
                  <Input
                    label="新密码"
                    placeholder="请输入新密码（至少6位）"
                    type="password"
                    value={passwordForm.newPassword}
                    variant="bordered"
                    onChange={(e: React.ChangeEvent<HTMLInputElement>) => setPasswordForm(prev => ({ ...prev, newPassword: e.target.value }))}
                  />
                  <Input
                    label="确认密码"
                    placeholder="请再次输入新密码"
                    type="password"
                    value={passwordForm.confirmPassword}
                    variant="bordered"
                    onChange={(e: React.ChangeEvent<HTMLInputElement>) => setPasswordForm(prev => ({ ...prev, confirmPassword: e.target.value }))}
                  />
                </div>
              </ModalBody>
              <ModalFooter>
                <Button color="default" variant="light" onPress={onClose}>
                  取消
                </Button>
                <Button
                  color="primary"
                  isLoading={passwordLoading}
                  onPress={handlePasswordSubmit}
                >
                  确定
                </Button>
              </ModalFooter>
            </>
          )}
        </ModalContent>
      </Modal>
    </div>
  );
}
