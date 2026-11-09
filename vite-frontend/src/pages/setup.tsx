import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { Button } from "@heroui/button";
import { Input } from "@heroui/input";
import { Card, CardBody, CardHeader } from "@heroui/card";
import toast from "react-hot-toast";

import DefaultLayout from "@/layouts/default";
import { title } from "@/components/primitives";
import { initSetup } from "@/api";

/** 首次部署：设置管理员用户名和密码（复杂度不限），完成后直接进入面板 */
export default function SetupPage() {
  const navigate = useNavigate();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [loading, setLoading] = useState(false);

  const mismatch = confirm !== "" && password !== confirm;
  const canSubmit = username.trim() !== "" && password !== "" && password === confirm;

  const submit = async () => {
    if (!canSubmit || loading) return;
    setLoading(true);
    try {
      const res = await initSetup({ username: username.trim(), password });
      if (res.code !== 0) {
        toast.error(res.msg || "初始化失败");
        return;
      }
      localStorage.setItem("token", res.data.token);
      localStorage.setItem("role_id", res.data.role_id.toString());
      localStorage.setItem("name", res.data.name);
      localStorage.setItem("admin", (res.data.role_id === 0).toString());
      toast.success("管理员账号已设置");
      navigate("/dashboard", { replace: true });
    } catch {
      toast.error("网络错误，请稍后重试");
    } finally {
      setLoading(false);
    }
  };

  const onKey = (e: React.KeyboardEvent) => {
    if (e.key === "Enter") submit();
  };

  return (
    <DefaultLayout>
      <section className="flex flex-col items-center justify-center gap-4 py-4 sm:py-8 md:py-10 pb-20 min-h-[calc(100dvh-120px)] sm:min-h-[calc(100dvh-200px)]">
        <div className="w-full max-w-md px-4 sm:px-0">
          <Card className="w-full">
            <CardHeader className="pb-0 pt-6 px-6 flex-col items-center">
              <h1 className={title({ size: "sm" })}>初始化面板</h1>
              <p className="text-small text-default-500 mt-2 text-center">
                首次使用，请设置管理员用户名和密码
              </p>
            </CardHeader>
            <CardBody className="px-6 py-6">
              <div className="flex flex-col gap-4">
                <Input
                  label="管理员用户名"
                  placeholder="请输入用户名"
                  value={username}
                  onValueChange={setUsername}
                  onKeyDown={onKey}
                  variant="bordered"
                  isDisabled={loading}
                  autoFocus
                />
                <Input
                  label="密码"
                  placeholder="请输入密码"
                  type="password"
                  value={password}
                  onValueChange={setPassword}
                  onKeyDown={onKey}
                  variant="bordered"
                  isDisabled={loading}
                />
                <Input
                  label="确认密码"
                  placeholder="请再次输入密码"
                  type="password"
                  value={confirm}
                  onValueChange={setConfirm}
                  onKeyDown={onKey}
                  variant="bordered"
                  isDisabled={loading}
                  isInvalid={mismatch}
                  errorMessage={mismatch ? "两次输入的密码不一致" : undefined}
                />
                <Button
                  color="primary"
                  size="lg"
                  className="mt-2"
                  onPress={submit}
                  isLoading={loading}
                  isDisabled={!canSubmit}
                >
                  确定，进入面板
                </Button>
              </div>
            </CardBody>
          </Card>
        </div>
      </section>
    </DefaultLayout>
  );
}
