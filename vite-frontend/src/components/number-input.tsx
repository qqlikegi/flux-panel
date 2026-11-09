import { useEffect, useState } from "react";
import { Input, InputProps } from "@heroui/input";

/**
 * 数字输入框：输入过程中保留原始文本，可以清空、逐位删除、输入小数点。
 * - 内容为空或不合法时，value 回传 NaN（可选字段回传 null），由表单禁用“确定”按钮；
 * - 不在输入时强行改值（旧逻辑会把清空变成 0/1，导致无法逐位修改）。
 */
export interface NumberInputProps
  extends Omit<InputProps, "value" | "onChange" | "onValueChange" | "type" | "min" | "max"> {
  value: number | null;
  onValueChange: (value: number) => void;
  min?: number;
  max?: number;
  /** 是否允许小数，默认只允许整数 */
  decimal?: boolean;
  /** 是否允许留空（留空时回传 NaN 之外的 null 语义由调用方处理），默认不允许 */
  allowEmpty?: boolean;
}

const toText = (v: number | null) => (v === null || Number.isNaN(v) ? "" : String(v));

const parse = (text: string, decimal: boolean) => {
  const t = text.trim();
  if (t === "") return NaN;
  if (!(decimal ? /^\d+(\.\d+)?$/ : /^\d+$/).test(t)) return NaN;
  return decimal ? parseFloat(t) : parseInt(t, 10);
};

/** 判断数字是否合法（表单提交前用来禁用按钮） */
export const isValidNumber = (v: number | null | undefined, min?: number, max?: number) =>
  v !== null && v !== undefined && !Number.isNaN(v) && (min === undefined || v >= min) && (max === undefined || v <= max);

export function NumberInput({
  value,
  onValueChange,
  min,
  max,
  decimal = false,
  allowEmpty = false,
  isInvalid,
  errorMessage,
  ...rest
}: NumberInputProps) {
  const [text, setText] = useState(toText(value));

  // 外部改值（打开编辑弹窗、重置表单）时同步显示；输入过程中值相同则不打断
  useEffect(() => {
    const cur = parse(text, decimal);
    const same = (Number.isNaN(cur) && (value === null || Number.isNaN(value))) || cur === value;
    if (!same) setText(toText(value));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [value]);

  const n = parse(text, decimal);
  let localError = "";
  if (text.trim() === "") {
    if (!allowEmpty) localError = "不能为空";
  } else if (Number.isNaN(n)) {
    localError = decimal ? "请输入有效的数字" : "请输入整数";
  } else if (min !== undefined && n < min) {
    localError = `不能小于 ${min}`;
  } else if (max !== undefined && n > max) {
    localError = `不能大于 ${max}`;
  }

  return (
    <Input
      {...rest}
      type="text"
      inputMode={decimal ? "decimal" : "numeric"}
      value={text}
      onValueChange={(t) => {
        // 只允许输入数字（和一个小数点），其它字符直接忽略
        const cleaned = decimal ? t.replace(/[^\d.]/g, "").replace(/(\..*)\./g, "$1") : t.replace(/\D/g, "");
        setText(cleaned);
        onValueChange(parse(cleaned, decimal));
      }}
      isInvalid={!!localError || isInvalid}
      errorMessage={localError || errorMessage}
    />
  );
}

export default NumberInput;
