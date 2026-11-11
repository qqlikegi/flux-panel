import { useMemo } from "react";
import { Button } from "@heroui/button";
import { Select, SelectItem } from "@heroui/select";

export interface UserFilterOption {
  id: number | string;
  name: string;
}

// 中文等宽字符按 2 个字符宽度估算
const textWidth = (s: string) =>
  Array.from(s).reduce((n, c) => n + (c.charCodeAt(0) > 255 ? 2 : 1), 0);

/**
 * 用户列表筛选：下拉里勾选一个或多个用户，不勾选表示全部。
 * 宽度按最长用户名自适应，不会撑得很宽。
 */
export function UserFilter({
  users,
  picked,
  onChange,
}: {
  users: UserFilterOption[];
  picked: Set<string>;
  onChange: (next: Set<string>) => void;
}) {
  const sorted = useMemo(
    () => [...users].sort((a, b) => a.name.localeCompare(b.name)),
    [users],
  );
  // “已选 99 个用户”约 14 个字符宽，作为下限
  const chars = Math.min(28, Math.max(14, ...sorted.map((u) => textWidth(u.name))));

  return (
    <div className="flex items-center gap-2 flex-shrink-0">
      <div style={{ width: `calc(${chars}ch + 4.25rem)` }}>
        <Select
          aria-label="筛选用户"
          placeholder="全部用户"
          renderValue={(items) =>
            items.length === 1 ? items[0].textValue : `已选 ${items.length} 个用户`
          }
          selectedKeys={picked}
          selectionMode="multiple"
          size="sm"
          onSelectionChange={(keys) =>
            onChange(
              keys === "all"
                ? new Set(sorted.map((u) => String(u.id)))
                : new Set(Array.from(keys).map(String)),
            )
          }
        >
          {sorted.map((u) => (
            <SelectItem key={String(u.id)} textValue={u.name}>
              {u.name}
            </SelectItem>
          ))}
        </Select>
      </div>
      {picked.size > 0 && (
        <Button size="sm" variant="flat" onPress={() => onChange(new Set())}>
          全部
        </Button>
      )}
    </div>
  );
}
