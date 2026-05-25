"use client";

import * as React from "react";
import Box from "@mui/material/Box";
import Tooltip from "@mui/material/Tooltip";

interface Props {
  provider: string;
  size?: number;
}

function AwsLogo({ size }: { size: number }) {
  return (
    <svg width={size} height={size * 0.6} viewBox="0 0 80 48" fill="none" xmlns="http://www.w3.org/2000/svg" aria-label="Amazon Web Services">
      <path d="M22.5 32.9c-5.5 3.2-13.6 4.9-20.5 4.9-.5 0-.5-.3-.1-.5 4.2-2.5 9.5-5.5 14.3-6.9.4-.1.3-.4-.1-.4C7.5 29.5 2.8 27 0 24.6c-.3-.2-.1-.6.3-.5 9 2.1 19 3.2 28.1 3.2 2.7 0 5.4-.1 8-.3.6-.1.6.5.1.8-4.7 2.2-8.4 4.2-14 5.1z" fill="#FF9900"/>
      <path d="M28.3 20.2c-2.1-2.7-4-5.6-5.3-8.7-.4-.9.3-1.9 1.3-1.9h8.2c.9 0 1.5.9 1.2 1.7l-3.3 9.5c-.3.8-1.4.8-2.1-.6z" fill="#FF9900"/>
      <path d="M48.3 31.5c3.2 1.2 6.6 1.9 10.1 1.9 5.5 0 10.6-1.7 14.9-4.5.4-.3.9.2.6.6-3.8 5.2-10 8.5-16.9 8.5-6.4 0-12.2-2.8-16.3-7.2-.3-.3.1-.8.5-.6l7.1 1.3z" fill="#FF9900"/>
      <path d="M56 9.6c2.1 0 3.8 1.7 3.8 3.8 0 2.1-1.7 3.8-3.8 3.8-2.1 0-3.8-1.7-3.8-3.8C52.2 11.3 53.9 9.6 56 9.6z" fill="#FF9900"/>
      <path d="M36 9.5h8.2c1 0 1.7 1 1.3 1.9-1.4 3.4-3.8 7.6-6.6 11.2-.6.8-1.9.6-2.2-.3l-2.2-11.4c-.2-.8.5-1.4 1.5-1.4z" fill="#FF9900"/>
    </svg>
  );
}

function AzureLogo({ size }: { size: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 96 96" fill="none" xmlns="http://www.w3.org/2000/svg" aria-label="Microsoft Azure">
      <defs>
        <linearGradient id="azure-a" x1="0.958" y1="0.341" x2="0.427" y2="0.677" gradientUnits="objectBoundingBox">
          <stop offset="0" stopColor="#114a8b"/>
          <stop offset="1" stopColor="#0669bc"/>
        </linearGradient>
        <linearGradient id="azure-b" x1="0.1" y1="0.778" x2="0.776" y2="0.231" gradientUnits="objectBoundingBox">
          <stop offset="0" stopColor="#0078d4"/>
          <stop offset="0.883" stopColor="#2d87c3" stopOpacity="0"/>
        </linearGradient>
        <linearGradient id="azure-c" x1="0.305" y1="0.838" x2="0.735" y2="0.06" gradientUnits="objectBoundingBox">
          <stop offset="0" stopColor="#1e3a5f" stopOpacity="0.7"/>
          <stop offset="1" stopColor="#28a8e0" stopOpacity="0"/>
        </linearGradient>
        <linearGradient id="azure-d" x1="0.188" y1="0.133" x2="0.919" y2="0.862" gradientUnits="objectBoundingBox">
          <stop offset="0" stopColor="#35b8f1"/>
          <stop offset="1" stopColor="#28a8e0"/>
        </linearGradient>
      </defs>
      <path d="M33 4H57.7L31.9 80.3H7.2L33 4Z" fill="url(#azure-a)"/>
      <path d="M57.5 55.8L68.5 14.1H88.8L65.2 83.7C64.3 86.5 61.7 88.4 58.7 88.4H14.2L33 75.7H56.3C57.2 75.7 57.9 74.8 57.5 74L46.3 56.5" fill="url(#azure-d)"/>
    </svg>
  );
}

function GcpLogo({ size }: { size: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 64 64" fill="none" xmlns="http://www.w3.org/2000/svg" aria-label="Google Cloud Platform">
      <path d="M32 8C18.7 8 8 18.7 8 32s10.7 24 24 24 24-10.7 24-24S45.3 8 32 8z" fill="#4285F4" fillOpacity="0.15"/>
      <path d="M39.4 22.6H37v-2.2c0-.5-.4-.9-.9-.9h-8.2c-.5 0-.9.4-.9.9v2.2h-2.4c-1.3 0-2.4 1.1-2.4 2.4v4.6h19.6V25c0-1.3-1.1-2.4-2.4-2.4z" fill="#4285F4"/>
      <path d="M44 29.6H20c-.6 0-1 .4-1 1v11.8c0 .6.4 1 1 1h24c.6 0 1-.4 1-1V30.6c0-.5-.4-1-1-1z" fill="#34A853"/>
      <path d="M28 35.6a2 2 0 100 4 2 2 0 000-4zm8 0a2 2 0 100 4 2 2 0 000-4z" fill="#FBBC04"/>
    </svg>
  );
}

function KubernetesLogo({ size }: { size: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 32 32" fill="none" xmlns="http://www.w3.org/2000/svg" aria-label="Kubernetes">
      <circle cx="16" cy="16" r="14" fill="#326CE5" fillOpacity="0.15" stroke="#326CE5" strokeWidth="1.5"/>
      <path d="M16 6.5L8.5 11.5v9l7.5 5 7.5-5v-9L16 6.5z" stroke="#326CE5" strokeWidth="1.5" fill="none"/>
      <circle cx="16" cy="16" r="2.5" fill="#326CE5"/>
    </svg>
  );
}

function HelmLogo({ size }: { size: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 32 32" fill="none" xmlns="http://www.w3.org/2000/svg" aria-label="Helm">
      <circle cx="16" cy="16" r="14" fill="#0F1689" fillOpacity="0.2" stroke="#0F1689" strokeWidth="1.5"/>
      <path d="M10 12h12M10 16h12M10 20h12" stroke="#277FC9" strokeWidth="1.8" strokeLinecap="round"/>
    </svg>
  );
}

function VSphereIcon({ size }: { size: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 32 32" fill="none" xmlns="http://www.w3.org/2000/svg" aria-label="vSphere">
      <circle cx="16" cy="16" r="14" fill="#607078" fillOpacity="0.15" stroke="#607078" strokeWidth="1.5"/>
      <path d="M10 16a6 6 0 1012 0 6 6 0 00-12 0z" stroke="#8EBE4F" strokeWidth="1.5" fill="none"/>
      <circle cx="16" cy="16" r="2.5" fill="#8EBE4F"/>
    </svg>
  );
}

function GenericCloudIcon({ size, label }: { size: number; label: string }) {
  const letter = label.charAt(0).toUpperCase();
  return (
    <Box
      sx={{
        width: size,
        height: size,
        borderRadius: "6px",
        background: "linear-gradient(135deg, rgba(4,125,241,0.2), rgba(3,222,184,0.15))",
        border: "1px solid rgba(4,125,241,0.3)",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        fontSize: size * 0.45,
        fontWeight: 700,
        color: "#047df1",
        fontFamily: "monospace",
        flexShrink: 0,
      }}
    >
      {letter}
    </Box>
  );
}

const KNOWN_PROVIDERS: Record<string, (size: number) => React.ReactNode> = {
  aws: (s) => <AwsLogo size={s} />,
  azure: (s) => <AzureLogo size={s} />,
  azurerm: (s) => <AzureLogo size={s} />,
  google: (s) => <GcpLogo size={s} />,
  gcp: (s) => <GcpLogo size={s} />,
  googlecloud: (s) => <GcpLogo size={s} />,
  kubernetes: (s) => <KubernetesLogo size={s} />,
  k8s: (s) => <KubernetesLogo size={s} />,
  helm: (s) => <HelmLogo size={s} />,
  vsphere: (s) => <VSphereIcon size={s} />,
};

export default function ProviderLogo({ provider, size = 28 }: Props) {
  const key = provider.toLowerCase().replace(/[^a-z0-9]/g, "");
  const render = KNOWN_PROVIDERS[key];

  return (
    <Tooltip title={provider} placement="top">
      <Box sx={{ display: "inline-flex", alignItems: "center", flexShrink: 0 }}>
        {render ? (
          render(size)
        ) : (
          <GenericCloudIcon size={size} label={provider} />
        )}
      </Box>
    </Tooltip>
  );
}
