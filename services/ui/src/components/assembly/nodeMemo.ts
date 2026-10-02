import type { NodeProps } from "reactflow";

function equalValue(previous: unknown, next: unknown): boolean {
  if (Object.is(previous, next) || typeof previous === "function" && typeof next === "function") return true;
  if (typeof previous !== typeof next || previous === null || next === null) return false;
  if (Array.isArray(previous) && Array.isArray(next)) {
    return previous.length === next.length && previous.every((value, index) => equalValue(value, next[index]));
  }
  if (typeof previous !== "object" || typeof next !== "object") return false;

  const previousRecord = previous as Record<string, unknown>;
  const nextRecord = next as Record<string, unknown>;
  const previousKeys = Object.keys(previousRecord);
  const nextKeys = Object.keys(nextRecord);
  return previousKeys.length === nextKeys.length
    && previousKeys.every((key) => key in nextRecord && equalValue(previousRecord[key], nextRecord[key]));
}

export function areAssemblyNodePropsEqual<T>(previous: NodeProps<T>, next: NodeProps<T>): boolean {
  const previousRecord = previous as Record<string, unknown>;
  const nextRecord = next as Record<string, unknown>;
  const previousKeys = Object.keys(previousRecord);
  const nextKeys = Object.keys(nextRecord);
  return previousKeys.length === nextKeys.length
    && previousKeys.every((key) => key in nextRecord && equalValue(previousRecord[key], nextRecord[key]));
}
