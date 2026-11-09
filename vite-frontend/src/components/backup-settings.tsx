import { useEffect, useState } from "react";
import { Button } from "@heroui/button";
import { Card, CardBody, CardHeader } from "@heroui/card";
import { Input } from "@heroui/input";
import { Divider } from "@heroui/divider";
import { Switch } from "@heroui/switch";
import { Select, SelectItem } from "@heroui/select";
import toast from "react-hot-toast";

import { NumberInput, isValidNumber } from "@/components/number-input";
import { getBackupConfig, saveBackupConfig, testBackupConfig, runBackupNow, listBackups } from "@/api";

interface BackupForm {
  enabled: boolean;
  endpoint: string;
  bucket: string;
  accessKey: string;
  secretKey: string;
  prefix: string;
  keep: number;
  mode: "daily" | "interval";
  time: string;
  hours: number;
}

interface BackupObject {
  key: string;
  lastModified: string;
  size: number;
}

const emptyForm: BackupForm = {
  enabled: false, endpoint: "", bucket: "", accessKey: "", secretKey: "", prefix: "", keep: 3, mode: "daily", time: "03:00", hours: 24,
};

const formatSize = (v: number) => {
  if (v < 1024) return v + " B";
  if (v < 1024 * 1024) return (v / 1024).toFixed(1) + " KB";
  return (v / 1024 / 1024).toFixed(2) + " MB";
};

const fmtTime = (ms?: number | null) => (ms ? new Date(ms).toLocaleString() : "-");

/** Cloudflare R2（或任意 S3 兼容存储）定时备份数据库，最多保留 N 份，循环滚动 */
export default function BackupSettings() {
  const [form, setForm] = useState<BackupForm>(emptyForm);
  const [hasSecret, setHasSecret] = useState(false);
  const [status, setStatus] = useState<{ lastTime?: number; lastStatus?: string; lastMessage?: string; nextRun?: number | null }>({});
  const [backups, setBackups] = useState<BackupObject[] | null>(null);
  const [busy, setBusy] = useState<"" | "save" | "test" | "run" | "list">("");

  const set = <K extends keyof BackupForm>(k: K, v: BackupForm[K]) => setForm(prev => ({ ...prev, [k]: v }));

  const load = async () => {
    try {
      const res = await getBackupConfig();
      if (res.code !== 0) return;
      const c = res.data.config || {};
      setForm({ ...emptyForm, ...c, secretKey: "" });
      setHasSecret(!!res.data.hasSecret);
      setStatus({ lastTime: res.data.lastTime, lastStatus: res.data.lastStatus, lastMessage: res.data.lastMessage, nextRun: res.data.nextRun });
      if (c.endpoint && c.bucket && c.accessKey && res.data.hasSecret) loadList();
    } catch {
      /* 忽略 */
    }
  };

  const loadList = async () => {
    setBusy("list");
    try {
      const res = await listBackups();
      if (res.code === 0) setBackups(res.data || []);
      else setBackups(null);
    } finally {
      setBusy("");
    }
  };

  useEffect(() => {
    load();
  }, []);

  const required = form.endpoint.trim() !== "" && form.bucket.trim() !== "" && form.accessKey.trim() !== "" && (form.secretKey.trim() !== "" || hasSecret);
  const numbersOk = isValidNumber(form.keep, 1, 30) && (form.mode === "daily" ? /^\d{2}:\d{2}$/.test(form.time) : isValidNumber(form.hours, 1, 720));
  const canSave = numbersOk && (!form.enabled || required);

  const save = async (quiet = false) => {
    setBusy("save");
    try {
      const res = await saveBackupConfig(form);
      if (res.code !== 0) {
        toast.error(res.msg || "保存失败");
        return false;
      }
      if (!quiet) toast.success("备份设置已保存");
      await load();
      return true;
    } catch {
      toast.error("保存失败");
      return false;
    } finally {
      setBusy("");
    }
  };

  const test = async () => {
    setBusy("test");
    try {
      const res = await testBackupConfig(form);
      if (res.code === 0) toast.success(res.data || "连接成功", { duration: 5000 });
      else toast.error(res.msg || "连接失败", { duration: 8000 });
    } catch {
      toast.error("连接失败");
    } finally {
      setBusy("");
    }
  };

  const runNow = async () => {
    if (!(await save(true))) return;
    setBusy("run");
    try {
      const res = await runBackupNow();
      if (res.code === 0) toast.success(res.data || "备份成功", { duration: 5000 });
      else toast.error(res.msg || "备份失败", { duration: 8000 });
    } catch {
      toast.error("备份失败");
    } finally {
      setBusy("");
      load();
    }
  };

  return (
    <Card className="shadow-md mt-6">
      <CardHeader className="pb-4">
        <div className="flex flex-col sm:flex-row sm:justify-between sm:items-center w-full gap-3">
          <div>
            <h2 className="text-xl font-semibold">云存储备份</h2>
            <p className="text-sm text-gray-600 dark:text-gray-400">
              定时把面板数据库备份到 Cloudflare R2 存储桶（兼容任意 S3 存储），只保留最新的几份，自动删除最旧的
            </p>
          </div>
          <Switch isSelected={form.enabled} onValueChange={v => set("enabled", v)}>
            自动备份
          </Switch>
        </div>
      </CardHeader>
      <Divider />
      <CardBody className="space-y-4 pt-6">
        <Input
          label="S3 终结点"
          placeholder="https://<账户ID>.r2.cloudflarestorage.com"
          description="R2 → 管理 API 令牌 → 创建后页面上“为 S3 客户端使用管辖权地特定的终结点”"
          value={form.endpoint}
          onValueChange={v => set("endpoint", v)}
          variant="bordered"
        />
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <Input
            label="存储桶名称"
            placeholder="例如 forwardz-backup"
            description="不存在时会自动创建"
            value={form.bucket}
            onValueChange={v => set("bucket", v)}
            variant="bordered"
          />
          <Input
            label="备份目录（可选）"
            placeholder="例如 forwardz-panel"
            description="存储桶里存放备份的文件夹，留空放在根目录"
            value={form.prefix}
            onValueChange={v => set("prefix", v)}
            variant="bordered"
          />
          <Input
            label="访问密钥 ID"
            placeholder="Access Key ID"
            value={form.accessKey}
            onValueChange={v => set("accessKey", v)}
            variant="bordered"
            autoComplete="off"
          />
          <Input
            label="机密访问密钥"
            placeholder={hasSecret ? "已保存，留空表示不修改" : "Secret Access Key"}
            type="password"
            value={form.secretKey}
            onValueChange={v => set("secretKey", v)}
            variant="bordered"
            autoComplete="new-password"
          />
        </div>

        <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
          <Select
            label="备份周期"
            selectedKeys={[form.mode]}
            onSelectionChange={keys => {
              const k = Array.from(keys)[0] as string;
              if (k) set("mode", k as BackupForm["mode"]);
            }}
            variant="bordered"
          >
            <SelectItem key="daily">每天固定时间</SelectItem>
            <SelectItem key="interval">每隔几小时</SelectItem>
          </Select>
          {form.mode === "daily" ? (
            <Input
              label="备份时间"
              type="time"
              value={form.time}
              onValueChange={v => set("time", v)}
              variant="bordered"
              isInvalid={!/^\d{2}:\d{2}$/.test(form.time)}
              errorMessage={!/^\d{2}:\d{2}$/.test(form.time) ? "请选择时间" : undefined}
            />
          ) : (
            <NumberInput label="间隔小时" value={form.hours} onValueChange={v => set("hours", v)} min={1} max={720} variant="bordered" />
          )}
          <NumberInput
            label="最多保留份数"
            value={form.keep}
            onValueChange={v => set("keep", v)}
            min={1}
            max={30}
            variant="bordered"
            description="超过后自动删除最旧的"
          />
        </div>

        <div className="rounded-lg bg-default-50 dark:bg-default-100/50 p-3 text-sm space-y-1">
          <p>
            <span className="text-default-500">上次备份：</span>
            {status.lastTime ? (
              <>
                {fmtTime(status.lastTime)}{" "}
                <span className={status.lastStatus === "success" ? "text-success-600" : "text-danger-600"}>
                  {status.lastStatus === "success" ? "成功" : "失败"}
                </span>
                {status.lastMessage && <span className="text-default-500 break-all"> · {status.lastMessage}</span>}
              </>
            ) : (
              "还没有备份过"
            )}
          </p>
          <p>
            <span className="text-default-500">下次自动备份：</span>
            {form.enabled ? fmtTime(status.nextRun) : "未开启"}
          </p>
        </div>

        <div className="flex flex-wrap gap-2 justify-end">
          <Button variant="flat" onPress={test} isLoading={busy === "test"} isDisabled={!required || !!busy}>
            测试连接
          </Button>
          <Button variant="flat" color="secondary" onPress={runNow} isLoading={busy === "run"} isDisabled={!required || !canSave || !!busy}>
            立即备份
          </Button>
          <Button color="primary" onPress={() => save()} isLoading={busy === "save"} isDisabled={!canSave || !!busy}>
            保存设置
          </Button>
        </div>

        {backups !== null && (
          <>
            <Divider />
            <div className="flex items-center justify-between">
              <h3 className="font-medium">存储桶中的备份（{backups.length}）</h3>
              <Button size="sm" variant="light" onPress={loadList} isLoading={busy === "list"}>
                刷新
              </Button>
            </div>
            {backups.length === 0 ? (
              <p className="text-sm text-default-500">暂无备份</p>
            ) : (
              <div className="space-y-2">
                {backups.map(b => (
                  <div key={b.key} className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-1 p-2 rounded-lg border border-default-200 text-sm">
                    <span className="font-mono break-all">{b.key}</span>
                    <span className="text-default-500 whitespace-nowrap">
                      {formatSize(b.size)} · {b.lastModified ? new Date(b.lastModified).toLocaleString() : ""}
                    </span>
                  </div>
                ))}
              </div>
            )}
          </>
        )}
      </CardBody>
    </Card>
  );
}
