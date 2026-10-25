import type { SvgIconProps } from "@mui/material/SvgIcon";
import WarehouseIcon from "@mui/icons-material/Warehouse";
import ViewModuleOutlinedIcon from "@mui/icons-material/ViewModuleOutlined";
import CloudOutlinedIcon from "@mui/icons-material/CloudOutlined";
import ExtensionOutlinedIcon from "@mui/icons-material/ExtensionOutlined";
import SmartToyOutlinedIcon from "@mui/icons-material/SmartToyOutlined";
import LocalOfferOutlinedIcon from "@mui/icons-material/LocalOfferOutlined";

export type DepotKind = "depot" | "module" | "provider" | "skill" | "agent" | "version";

export function isDepotKind(kind: string): kind is DepotKind {
  return kind in KIND_ICONS;
}

const KIND_ICONS: Record<DepotKind, typeof WarehouseIcon> = {
  depot: WarehouseIcon,
  module: ViewModuleOutlinedIcon,
  provider: CloudOutlinedIcon,
  skill: ExtensionOutlinedIcon,
  agent: SmartToyOutlinedIcon,
  version: LocalOfferOutlinedIcon,
};

export function DepotKindIcon({ kind, ...props }: { kind: DepotKind } & SvgIconProps) {
  const Icon = KIND_ICONS[kind];
  return <Icon {...props} />;
}
