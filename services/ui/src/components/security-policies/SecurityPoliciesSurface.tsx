"use client";

import * as React from "react";
import Alert from "@mui/material/Alert";
import Box from "@mui/material/Box";
import Breadcrumbs from "@mui/material/Breadcrumbs";
import Button from "@mui/material/Button";
import Card from "@mui/material/Card";
import CardActionArea from "@mui/material/CardActionArea";
import CardContent from "@mui/material/CardContent";
import Checkbox from "@mui/material/Checkbox";
import Chip from "@mui/material/Chip";
import CircularProgress from "@mui/material/CircularProgress";
import Container from "@mui/material/Container";
import Dialog from "@mui/material/Dialog";
import DialogActions from "@mui/material/DialogActions";
import DialogContent from "@mui/material/DialogContent";
import DialogTitle from "@mui/material/DialogTitle";
import Divider from "@mui/material/Divider";
import Grid from "@mui/material/Grid";
import IconButton from "@mui/material/IconButton";
import InputAdornment from "@mui/material/InputAdornment";
import ListItemText from "@mui/material/ListItemText";
import MenuItem from "@mui/material/MenuItem";
import Select from "@mui/material/Select";
import Skeleton from "@mui/material/Skeleton";
import Stack from "@mui/material/Stack";
import TextField from "@mui/material/TextField";
import Tooltip from "@mui/material/Tooltip";
import Typography from "@mui/material/Typography";
import { useColorScheme } from "@mui/material/styles";
import AddIcon from "@mui/icons-material/Add";
import BadgeOutlinedIcon from "@mui/icons-material/BadgeOutlined";
import CodeIcon from "@mui/icons-material/Code";
import DeleteOutlineIcon from "@mui/icons-material/DeleteOutline";
import PolicyOutlinedIcon from "@mui/icons-material/PolicyOutlined";
import RefreshIcon from "@mui/icons-material/Refresh";
import RemoveModeratorOutlinedIcon from "@mui/icons-material/RemoveModeratorOutlined";
import SearchIcon from "@mui/icons-material/Search";
import TrackChangesIcon from "@mui/icons-material/TrackChanges";
import TuneIcon from "@mui/icons-material/Tune";
import VerifiedUserOutlinedIcon from "@mui/icons-material/VerifiedUserOutlined";
import WarningAmberIcon from "@mui/icons-material/WarningAmber";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { dump as dumpYaml } from "js-yaml";
import dayjs from "dayjs";
import utc from "dayjs/plugin/utc";
import { AdapterDayjs } from "@mui/x-date-pickers/AdapterDayjs";
import { DatePicker } from "@mui/x-date-pickers/DatePicker";
import { LocalizationProvider } from "@mui/x-date-pickers/LocalizationProvider";
import { Highlight, type Language, type PrismTheme, themes } from "prism-react-renderer";
import Prism from "prismjs";
import "prismjs/components/prism-yaml";
import type {
  ScanPolicy,
  ScanPolicyCapabilities,
  ScanPolicyCatalog,
  ScanPolicyCatalogItem,
  ScanPolicyExemption,
  ScanPolicyMutation,
  ScanPolicyPreview,
  ScanPolicySpec,
  ScanPolicyTargetRef,
  ScanSeverityThreshold,
  ScanType,
} from "@/lib/api";
import { validateScanPolicy } from "./securityPolicyValidation";
import PageHeader from "@/components/PageHeader";
import CopyButton from "@/components/CopyButton";
import ResourceListControls, { PAGE_SIZE_OPTIONS } from "@/components/ResourceListControls";
import StatCard from "@/components/StatCard";

(typeof globalThis !== "undefined" ? globalThis : window).Prism = Prism;
dayjs.extend(utc);
type PrismGrammarWithKey = Prism.Grammar & { key: Prism.TokenObject };
const yamlGrammar = Prism.languages.yaml as PrismGrammarWithKey;
const ymlGrammar = Prism.languages.yml as PrismGrammarWithKey;
yamlGrammar.key.alias = "property";
ymlGrammar.key.alias = "property";

const thresholds: ScanSeverityThreshold[] = ["CRITICAL", "HIGH", "MEDIUM", "LOW", "NONE"];
const scanTypes: ScanType[] = ["binary", "source", "module", "agent"];
const exemptionSeverities = ["CRITICAL", "HIGH", "MEDIUM", "LOW", "UNKNOWN"];
const emptySpec: ScanPolicySpec = { priority: 0, severityThreshold: "HIGH", exemptions: [] };
const thresholdColors: Record<ScanSeverityThreshold, "error" | "warning" | "info" | "success" | "default"> = {
  CRITICAL: "error",
  HIGH: "warning",
  MEDIUM: "info",
  LOW: "success",
  NONE: "default",
};
const accent = { primary: "#047df1", mint: "#03deb8", teal: "#04cfd0", amber: "#f59e0b" };

function emptyMutation(namespace = ""): ScanPolicyMutation {
  return {
    apiVersion: "opendepot.defdev.io/v1alpha1",
    kind: "ScanPolicy",
    metadata: { name: "", namespace },
    spec: structuredClone(emptySpec),
  };
}

function splitLines(value: string): string[] {
  return value.split(/\r?\n|,/).map((entry) => entry.trim()).filter(Boolean);
}

function policyToMutation(policy: ScanPolicy): ScanPolicyMutation {
  return {
    apiVersion: policy.apiVersion,
    kind: policy.kind,
    metadata: { name: policy.metadata.name, namespace: policy.metadata.namespace, resourceVersion: policy.metadata.resourceVersion },
    spec: {
      priority: policy.spec.priority ?? 0,
      selector: policy.spec.selector ? structuredClone(policy.spec.selector) : undefined,
      targetRefs: policy.spec.targetRefs?.map((ref) => ({ ...ref })),
      severityThreshold: policy.spec.severityThreshold,
      exemptions: policy.spec.exemptions?.map((item) => ({ ...item })),
    },
  };
}

function policyToYaml(policy: ScanPolicyMutation): string {
  const { resourceVersion: _resourceVersion, ...metadata } = policy.metadata;
  const spec = Object.fromEntries(Object.entries(policy.spec).filter(([, value]) => !(Array.isArray(value) && value.length === 0)));
  return dumpYaml({ ...policy, metadata, spec }, { lineWidth: -1, noRefs: true, skipInvalid: true });
}

async function readResponse<T>(response: Response): Promise<T> {
  const body = (await response.json().catch(() => ({}))) as T;
  if (!response.ok) {
    const error = new Error((body as { message?: string }).message ?? `Request failed (${response.status})`);
    Object.assign(error, { status: response.status, body });
    throw error;
  }
  return body;
}

interface SurfaceProps {
  mode: "list" | "create" | "edit";
  namespace?: string;
  name?: string;
  catalog: ScanPolicyCatalog;
}

export default function SecurityPoliciesSurface({ mode, namespace, name, catalog }: SurfaceProps) {
  const router = useRouter();
  const { mode: colorMode, systemMode } = useColorScheme();
  const resolvedMode = colorMode === "system" ? systemMode : colorMode;
  const prismTheme = resolvedMode === "light" ? themes.github : themes.nightOwl;
  const defaultNamespace = catalog.items.some((item) => item.namespace === namespace)
    ? namespace ?? ""
    : catalog.items[0]?.namespace ?? "";
  const catalogItem = catalog.items.find((item) => item.namespace === (mode === "list" ? defaultNamespace : namespace));
  const [policies, setPolicies] = React.useState<ScanPolicy[]>([]);
  const [selectedNamespace, setSelectedNamespace] = React.useState(defaultNamespace);
  const [policyQuery, setPolicyQuery] = React.useState("");
  const [listPage, setListPage] = React.useState(1);
  const [listPageSize, setListPageSize] = React.useState(PAGE_SIZE_OPTIONS[0]);
  const [policy, setPolicy] = React.useState<ScanPolicy | null>(null);
  const [form, setForm] = React.useState<ScanPolicyMutation>(() => emptyMutation(defaultNamespace));
  const [errors, setErrors] = React.useState<Record<string, string>>({});
  const [loading, setLoading] = React.useState(true);
  const [saving, setSaving] = React.useState(false);
  const [capabilities, setCapabilities] = React.useState<ScanPolicyCapabilities | null>(null);
  const [validationResult, setValidationResult] = React.useState<{ valid: boolean } | null>(null);
  const [canWrite, setCanWrite] = React.useState(false);
  const [message, setMessage] = React.useState<{ severity: "error" | "success" | "warning"; text: string } | null>(null);
  const [deleteOpen, setDeleteOpen] = React.useState(false);

  const load = React.useCallback(async () => {
    const activeNamespace = mode === "list" ? selectedNamespace : namespace;
    if (!activeNamespace) {
      setLoading(false);
      return;
    }
    setLoading(true);
    setCapabilities(null);
    setCanWrite(false);
    setMessage(null);
    try {
      const [listResponse, capabilitiesResponse] = await Promise.all([
        fetch(`/api/security-policies/${encodeURIComponent(activeNamespace)}`, { cache: "no-store" }),
        fetch(`/api/security-policies/${encodeURIComponent(activeNamespace)}/capabilities`, { cache: "no-store" }),
      ]);
      const list = await readResponse<{ items: ScanPolicy[] }>(listResponse);
      const loadedCapabilities = await readResponse<ScanPolicyCapabilities>(capabilitiesResponse);
      setCapabilities(loadedCapabilities);
      const activeCatalogItem = catalog.items.find((item) => item.namespace === activeNamespace);
      setCanWrite(Boolean(catalog.writesEnabled && activeCatalogItem?.canWrite && loadedCapabilities.writesEnabled && loadedCapabilities.canWrite));
      setPolicies(list.items ?? []);
      if (mode !== "list" && name) {
        const detail = await readResponse<ScanPolicy>(
          await fetch(`/api/security-policies/${encodeURIComponent(activeNamespace)}/${encodeURIComponent(name)}`, { cache: "no-store" }),
        );
        setPolicy(detail);
        setForm(policyToMutation(detail));
      }
    } catch (error) {
      setMessage({ severity: "error", text: error instanceof Error ? error.message : "Unable to load security policies." });
    } finally {
      setLoading(false);
    }
  }, [catalog, mode, name, namespace, selectedNamespace]);

  React.useEffect(() => { void load(); }, [load]);

  const updateSpec = (changes: Partial<ScanPolicySpec>) => setForm((current) => ({ ...current, spec: { ...current.spec, ...changes } }));
  const isEdit = mode === "edit";
  const namespaceWideAllowed = Boolean(catalogItem?.canManageNamespaceWidePolicies && capabilities?.canManageNamespaceWidePolicies);
  const filteredPolicies = policies.filter((item) => {
    const query = policyQuery.trim().toLowerCase();
    if (!query) return true;
    return [item.metadata.name, item.metadata.namespace, item.spec.severityThreshold ?? "", item.status?.supersededBy ?? ""]
      .join(" ")
      .toLowerCase()
      .includes(query);
  });
  const currentListPage = Math.min(listPage, Math.max(1, Math.ceil(filteredPolicies.length / listPageSize)));
  const pagedPolicies = filteredPolicies.slice((currentListPage - 1) * listPageSize, currentListPage * listPageSize);

  React.useEffect(() => setListPage(1), [policyQuery, selectedNamespace]);

  const handleWriteError = (error: unknown) => {
    const status = (error as { status?: number }).status;
    if (status === 401 || status === 403) {
      setCanWrite(false);
      setMessage({ severity: "warning", text: "Your account is not authorized to manage ScanPolicies." });
    } else {
      setMessage({
        severity: status === 409 ? "warning" : "error",
        text: status === 409
          ? "This policy changed on the server. The current draft is preserved; reload the latest version before saving again."
          : error instanceof Error ? error.message : "Unable to save policy.",
      });
    }
  };

  const save = async (event: React.FormEvent) => {
    event.preventDefault();
    const validation = validateScanPolicy(form);
    if (!namespaceWideAllowed && !form.spec.targetRefs?.length) {
      validation.targets = "At least one authorized target is required for this namespace.";
    }
    setErrors(validation);
    if (Object.keys(validation).length > 0 || !canWrite) return;
    setSaving(true);
    setMessage(null);
    try {
      const targetNamespace = form.metadata.namespace;
      const response = await fetch(
        isEdit
          ? `/api/security-policies/${encodeURIComponent(targetNamespace)}/${encodeURIComponent(form.metadata.name)}`
          : `/api/security-policies/${encodeURIComponent(targetNamespace)}`,
        {
          method: isEdit ? "PUT" : "POST",
          headers: {
            "Content-Type": "application/json",
            ...(isEdit && form.metadata.resourceVersion ? { "If-Match": form.metadata.resourceVersion } : {}),
          },
          body: JSON.stringify(form),
        },
      );
      const saved = await readResponse<ScanPolicy>(response);
      setMessage({ severity: "success", text: `Policy ${saved.metadata.name} saved.` });
      router.push(`/security-policies/${encodeURIComponent(saved.metadata.namespace)}/${encodeURIComponent(saved.metadata.name)}`);
      router.refresh();
    } catch (error) {
      handleWriteError(error);
    } finally {
      setSaving(false);
    }
  };

  React.useEffect(() => setValidationResult(null), [form]);

  const runValidation = async () => {
    if (!capabilities?.supportsPreview || !form.metadata.namespace) return;
    try {
      const response = await fetch(`/api/security-policies/${encodeURIComponent(form.metadata.namespace)}/preview`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(form),
      });
      const result = await readResponse<ScanPolicyPreview>(response);
      setValidationResult({ valid: result.valid });
    } catch (error) {
      setMessage({ severity: "error", text: error instanceof Error ? error.message : "Policy validation failed." });
    }
  };

  const remove = async () => {
    if (!policy || !canWrite) return;
    setSaving(true);
    try {
      const response = await fetch(`/api/security-policies/${encodeURIComponent(policy.metadata.namespace)}/${encodeURIComponent(policy.metadata.name)}`, {
        method: "DELETE",
        headers: policy.metadata.resourceVersion ? { "If-Match": policy.metadata.resourceVersion } : undefined,
      });
      await readResponse<void>(response);
      router.push("/security-policies");
      router.refresh();
    } catch (error) {
      handleWriteError(error);
    } finally {
      setSaving(false);
      setDeleteOpen(false);
    }
  };

  if (mode === "list") {
    return (
      <Box>
        <PageHeader icon={<PolicyOutlinedIcon color="primary" fontSize="small" />} title="Security Policies" description="Manage ScanPolicy rules and exemptions." actions={<Tooltip title="Refresh"><IconButton aria-label="Refresh policies" onClick={() => void load()}><RefreshIcon /></IconButton></Tooltip>} mobileOnly />
        <Container maxWidth="xl" sx={{ py: 4 }}>
          <Stack direction={{ xs: "column", sm: "row" }} justifyContent="space-between" alignItems={{ xs: "stretch", sm: "flex-end" }} spacing={2} sx={{ mb: 3 }}>
            <Box sx={{ display: { xs: "none", sm: "block" } }}>
              <Box display="flex" alignItems="center" gap={1}>
                <Typography variant="h4" component="h1">Security Policies</Typography>
                <Tooltip title="Refresh"><IconButton aria-label="Refresh policies" onClick={() => void load()}><RefreshIcon /></IconButton></Tooltip>
              </Box>
              <Typography variant="body1" color="text.secondary" mt={1}>Manage ScanPolicy rules and exemptions.</Typography>
            </Box>
            <Stack direction="row" spacing={1.5} alignItems="center">
              {catalog.items.length > 0 && <TextField select size="small" label="Namespace" value={selectedNamespace} onChange={(event) => setSelectedNamespace(event.target.value)} sx={{ flex: { xs: 1, sm: "none" }, width: { sm: 240 } }}>{catalog.items.map((item) => <MenuItem key={item.namespace} value={item.namespace}>{item.namespace}</MenuItem>)}</TextField>}
              {canWrite && <Button component={Link} href={`/security-policies/new${selectedNamespace ? `?namespace=${encodeURIComponent(selectedNamespace)}` : ""}`} variant="contained" startIcon={<AddIcon />} sx={{ whiteSpace: "nowrap" }}>Create policy</Button>}
            </Stack>
          </Stack>
          {message && <Alert severity={message.severity} sx={{ mb: 2 }}>{message.text}</Alert>}
          <Grid container spacing={2}>
            <Grid size={{ xs: 6, md: 3 }}><StatCard loading={loading} label="Non-superseded" value={policies.filter((item) => !item.status?.supersededBy).length} sub={`of ${plural(policies.length, "policy", "policies")}`} icon={<VerifiedUserOutlinedIcon fontSize="small" />} accentColor={accent.mint} /></Grid>
            <Grid size={{ xs: 6, md: 3 }}><StatCard loading={loading} label="Matching resources" value={policies.filter((item) => (item.status?.matchedVersions ?? 0) > 0).length} sub={`of ${plural(policies.length, "policy", "policies")}`} icon={<TrackChangesIcon fontSize="small" />} accentColor={accent.teal} /></Grid>
            <Grid size={{ xs: 6, md: 3 }}><StatCard loading={loading} label="Active exemptions" value={policies.reduce((total, item) => total + (item.status?.activeExemptions ?? 0), 0)} icon={<RemoveModeratorOutlinedIcon fontSize="small" />} accentColor={accent.primary} /></Grid>
            <Grid size={{ xs: 6, md: 3 }}><StatCard loading={loading} label="Expired exemptions" value={policies.reduce((total, item) => total + (item.status?.expiredExemptions ?? 0), 0)} icon={<WarningAmberIcon fontSize="small" />} accentColor="#ef6c00" /></Grid>
          </Grid>
          <Stack direction={{ xs: "column", sm: "row" }} justifyContent="space-between" alignItems={{ xs: "stretch", sm: "center" }} spacing={1.5} sx={{ mt: 5, mb: 2, pb: 1.5, borderBottom: "1px solid", borderColor: "divider" }}>
            <Stack direction="row" spacing={1} alignItems="center">
              <SectionTitle icon={<PolicyOutlinedIcon fontSize="small" />} title="Policies" mb={0} />
              {!loading && selectedNamespace && <Chip size="small" label={filteredPolicies.length} />}
            </Stack>
            <TextField size="small" label="Search policies" placeholder="Name, severity, or status" value={policyQuery} onChange={(event) => setPolicyQuery(event.target.value)} InputProps={{ startAdornment: <InputAdornment position="start"><SearchIcon fontSize="small" color="action" /></InputAdornment> }} sx={{ width: { xs: "100%", sm: 300 } }} />
          </Stack>
          {!selectedNamespace ? <Alert severity="info">Select a namespace to view its policies.</Alert> : loading ? <PolicyListSkeleton /> : policies.length === 0 ? (
            <Card><CardContent sx={{ py: 6, display: "flex", flexDirection: "column", alignItems: "center", textAlign: "center" }}><PolicyIconBadge size={56} /><Typography variant="h6" sx={{ mt: 2 }}>No policies in {selectedNamespace}</Typography><Typography color="text.secondary" sx={{ mb: 2, maxWidth: 420 }}>Create a guided ScanPolicy to set severity thresholds and finding exemptions for this namespace.</Typography>{canWrite && <Button component={Link} href={`/security-policies/new?namespace=${encodeURIComponent(selectedNamespace)}`} variant="outlined" startIcon={<AddIcon />}>Create your first policy</Button>}</CardContent></Card>
          ) : filteredPolicies.length === 0 ? <Alert severity="info">No policies match “{policyQuery}”.</Alert> : <><Grid container spacing={2}>{pagedPolicies.map((item) => <Grid key={`${item.metadata.namespace}/${item.metadata.name}`} size={{ xs: 12, sm: 6, lg: 4 }}><PolicyCard policy={item} darkMode={resolvedMode === "dark"} /></Grid>)}</Grid><ResourceListControls totalCount={filteredPolicies.length} page={currentListPage} pageSize={listPageSize} noun={["policy", "policies"]} onChange={(nextPage, nextPageSize) => { setListPage(nextPage); setListPageSize(nextPageSize); }} /></>}
        </Container>
      </Box>
    );
  }

  return (
    <Box>
      <PageHeader icon={<PolicyOutlinedIcon color="primary" fontSize="small" />} title={isEdit ? `Edit ${name}` : "Create Security Policy"} description="Guided ScanPolicy configuration." mobileOnly />
      <Container maxWidth="xl" sx={{ py: 4 }}>
      <Box sx={{ mb: 3, display: { xs: "none", sm: "block" } }}>
        <Breadcrumbs sx={{ mb: 2, "& a": { textDecoration: "none", color: "text.secondary", "&:hover": { color: "primary.main" } } }}>
          <Link href="/security-policies">Security Policies</Link>
          {form.metadata.namespace && <Typography variant="body2" color="text.secondary" sx={{ fontFamily: "monospace" }}>{form.metadata.namespace}</Typography>}
          <Typography variant="body2" color="text.primary" fontWeight={600}>{isEdit ? name : "New policy"}</Typography>
        </Breadcrumbs>
        <Typography variant="h4" component="h1">{isEdit ? "Edit Security Policy" : "Create Security Policy"}</Typography>
        <Typography variant="body1" color="text.secondary" mt={1}>Guided ScanPolicy configuration.</Typography>
      </Box>
      <Grid container spacing={3} alignItems="flex-start">
      <Grid size={{ xs: 12, lg: 7 }}>
      {isEdit && policy && <Box sx={{ mb: 3 }}><Grid container spacing={2}><Grid size={{ xs: 6 }}><StatCard label="Matched versions" value={policy.status?.matchedVersions ?? 0} icon={<TrackChangesIcon fontSize="small" />} accentColor={accent.teal} /></Grid><Grid size={{ xs: 6 }}><StatCard label="Active exemptions" value={policy.status?.activeExemptions ?? 0} sub={`of ${policy.spec.exemptions?.length ?? 0} configured`} icon={<RemoveModeratorOutlinedIcon fontSize="small" />} accentColor={accent.amber} /></Grid></Grid><Stack direction="row" flexWrap="wrap" gap={1} sx={{ mt: 1.5 }}>{policy.status?.expiredExemptions ? <Chip label={`${policy.status.expiredExemptions} expired exemptions`} size="small" color="warning" /> : null}{policy.status?.supersededBy && <Chip label={`Superseded by ${policy.status.supersededBy}`} size="small" color="warning" sx={{ color: resolvedMode === "dark" ? "#fff" : undefined }} />}</Stack></Box>}
      <Box component="form" onSubmit={save}>
        {message && <Alert severity={message.severity} sx={{ mb: 2 }}>{message.text}</Alert>}
        {loading && isEdit ? <PolicyFormSkeleton /> : <Stack spacing={3}>
          <Card><CardContent><SectionTitle icon={<BadgeOutlinedIcon fontSize="small" />} title="Policy identity" /><Grid container spacing={2}><Grid size={12}><TextField fullWidth required label="Name" slotProps={{ inputLabel: { shrink: true } }} value={form.metadata.name} disabled={isEdit} error={Boolean(errors.name)} helperText={errors.name ?? "Lowercase DNS-compatible name."} onChange={(event) => setForm({ ...form, metadata: { ...form.metadata, name: event.target.value } })} /></Grid><Grid size={{ xs: 12, sm: 6 }}><TextField fullWidth required select label="Namespace" slotProps={{ inputLabel: { shrink: true } }} value={form.metadata.namespace} disabled={isEdit} error={Boolean(errors.namespace)} helperText={errors.namespace ?? "Select an authorized policy namespace."} onChange={(event) => setForm({ ...form, metadata: { ...form.metadata, namespace: event.target.value } })}>{catalog.items.map((item) => <MenuItem key={item.namespace} value={item.namespace}>{item.namespace}</MenuItem>)}</TextField></Grid><Grid size={{ xs: 12, sm: 6 }}><TextField fullWidth type="number" label="Priority" slotProps={{ inputLabel: { shrink: true } }} value={form.spec.priority ?? 0} helperText="Higher priority wins outright when policies overlap." onChange={(event) => updateSpec({ priority: Number(event.target.value) })} /></Grid></Grid></CardContent></Card>
          <Card><CardContent><SectionTitle icon={<TuneIcon fontSize="small" />} title="Matching and scan threshold" /><Stack spacing={2}><TextField select fullWidth label="Severity threshold" value={form.spec.severityThreshold ?? ""} helperText="Leave empty to use the controller baseline; NONE disables blocking." onChange={(event) => updateSpec({ severityThreshold: (event.target.value || undefined) as ScanSeverityThreshold | undefined })}><MenuItem value="">Controller baseline</MenuItem>{thresholds.map((threshold) => <MenuItem key={threshold} value={threshold}>{threshold}</MenuItem>)}</TextField>{namespaceWideAllowed ? <TextField fullWidth label="Target labels (JSON object)" placeholder='{"environment":"sandbox"}' value={JSON.stringify(form.spec.selector?.matchLabels ?? {}) === "{}" ? "" : JSON.stringify(form.spec.selector?.matchLabels)} error={Boolean(errors.selector)} helperText={errors.selector ?? "Optional exact label matches, for example {\"environment\":\"sandbox\"}."} onChange={(event) => { try { const matchLabels = event.target.value.trim() ? JSON.parse(event.target.value) as Record<string, string> : undefined; updateSpec({ selector: matchLabels ? { matchLabels } : undefined }); } catch { setErrors((current) => ({ ...current, selector: "Enter a valid JSON object of string labels." })); } }} /> : capabilities && <Alert severity="info">This binding permits only policies targeted at authorized onboarded resources.</Alert>}<PolicyTargets value={form.spec.targetRefs ?? []} onChange={(targetRefs) => updateSpec({ targetRefs: targetRefs.length ? targetRefs : undefined })} errors={errors} catalogItem={catalogItem} /></Stack></CardContent></Card>
          <ExemptionEditor value={form.spec.exemptions ?? []} onChange={(exemptions) => updateSpec({ exemptions: exemptions.length ? exemptions : undefined })} errors={errors} />
          {validationResult && <Alert severity={validationResult.valid ? "success" : "warning"} aria-live="polite" onClose={() => setValidationResult(null)}>{validationResult.valid ? "Policy is valid. Nothing was saved." : "Policy is invalid according to the server. Nothing was saved."}</Alert>}
          <Stack direction={{ xs: "column-reverse", sm: "row" }} justifyContent="space-between" spacing={1}><Button component={Link} href="/security-policies">Cancel</Button><Stack direction={{ xs: "column", sm: "row" }} spacing={1}>{isEdit && policy && <Button type="button" color="error" variant="outlined" startIcon={<DeleteOutlineIcon />} disabled={!canWrite || saving} onClick={() => setDeleteOpen(true)}>Delete policy</Button>}<Button type="button" variant="outlined" disabled={!capabilities?.supportsPreview || saving} onClick={() => void runValidation()}>Validate policy</Button><Button type="submit" variant="contained" disabled={saving || !canWrite}>{saving ? <CircularProgress size={20} /> : isEdit ? "Save changes" : "Create policy"}</Button></Stack></Stack>
        </Stack>}
      </Box>
      </Grid>
      <Grid size={{ lg: 5 }} sx={{ display: { xs: "none", lg: "block" }, position: "sticky", top: 24 }}>
        {loading && isEdit ? <Skeleton variant="rounded" height="calc(100vh - 48px)" /> : <LiveYamlPanel code={policyToYaml(form)} fileName={`${form.metadata.name || "scanpolicy"}.yaml`} theme={prismTheme} />}
      </Grid>
      </Grid>
      </Container>
      <Dialog open={deleteOpen} onClose={() => setDeleteOpen(false)} aria-labelledby="delete-policy-title"><DialogTitle id="delete-policy-title">Delete {policy?.metadata.name}?</DialogTitle><DialogContent><Typography>This permanently removes the policy and its exemptions. This action cannot be undone.</Typography></DialogContent><DialogActions><Button onClick={() => setDeleteOpen(false)}>Cancel</Button><Button color="error" variant="contained" onClick={() => void remove()} disabled={saving}>Delete policy</Button></DialogActions></Dialog>
    </Box>
  );
}

function PolicyFormSkeleton() {
  const sectionTitle = <Stack direction="row" spacing={1} alignItems="center" sx={{ mb: 2 }}><Skeleton variant="circular" width={20} height={20} /><Skeleton variant="text" width={180} height={24} /></Stack>;
  return (
    <Stack role="status" aria-label="Loading policy" spacing={3}>
      <Grid container spacing={2}><Grid size={{ xs: 6 }}><StatCard loading label="Matched versions" value={0} icon={<TrackChangesIcon fontSize="small" />} accentColor={accent.teal} /></Grid><Grid size={{ xs: 6 }}><StatCard loading label="Active exemptions" value={0} sub=" " icon={<RemoveModeratorOutlinedIcon fontSize="small" />} accentColor={accent.amber} /></Grid></Grid>
      <Card><CardContent>{sectionTitle}<Stack spacing={2}><Skeleton variant="rounded" height={56} /><Stack direction={{ xs: "column", sm: "row" }} spacing={2}><Skeleton variant="rounded" height={56} sx={{ flex: 1 }} /><Skeleton variant="rounded" height={56} sx={{ flex: 1 }} /></Stack></Stack></CardContent></Card>
      <Card><CardContent>{sectionTitle}<Stack spacing={2}><Skeleton variant="rounded" height={56} /><Skeleton variant="text" width="40%" /><Skeleton variant="rounded" height={56} /></Stack></CardContent></Card>
      <Card><CardContent>{sectionTitle}<Skeleton variant="text" width="70%" /><Skeleton variant="rounded" height={120} sx={{ mt: 2 }} /></CardContent></Card>
      <Stack direction="row" justifyContent="space-between"><Skeleton variant="rounded" width={80} height={36} /><Stack direction="row" spacing={1}><Skeleton variant="rounded" width={130} height={36} /><Skeleton variant="rounded" width={130} height={36} /></Stack></Stack>
    </Stack>
  );
}

function MultiSelectField<T extends string>({ label, options, value, onChange }: { label: string; options: readonly T[]; value: T[]; onChange: (value: T[]) => void }) {
  const allOptions = [...options, ...value.filter((item) => !options.includes(item))];
  return (
    <TextField
      select
      fullWidth
      label={label}
      value={value}
      slotProps={{
        inputLabel: { shrink: true },
        select: {
          multiple: true,
          displayEmpty: true,
          renderValue: (selected) => (selected as T[]).length ? (selected as T[]).join(", ") : <Typography component="span" color="text.secondary">All</Typography>,
        },
      }}
      onChange={(event) => {
        const selected = event.target.value as unknown as T[] | string;
        onChange((typeof selected === "string" ? selected.split(",") : selected) as T[]);
      }}
    >
      {allOptions.map((option) => (
        <MenuItem key={option} value={option} sx={{ py: 0.25 }}>
          <Checkbox size="small" checked={value.includes(option)} sx={{ ml: -1 }} />
          <ListItemText primary={option} />
        </MenuItem>
      ))}
    </TextField>
  );
}

function ExpiryPicker({ value, onChange }: { value?: string; onChange: (value: string | undefined) => void }) {
  const parsed = value ? dayjs.utc(value) : null;
  return (
    <LocalizationProvider dateAdapter={AdapterDayjs}>
      <DatePicker
        label="Expires"
        timezone="UTC"
        value={parsed?.isValid() ? parsed : null}
        onChange={(next) => onChange(next?.isValid() ? next.format("YYYY-MM-DD[T00:00:00Z]") : undefined)}
        slotProps={{ textField: { fullWidth: true }, field: { clearable: true } }}
      />
    </LocalizationProvider>
  );
}

function LiveYamlPanel({ code, fileName, theme }: { code: string; fileName: string; theme: PrismTheme }) {
  return (
    <Card aria-label="Live YAML" sx={{ height: "calc(100vh - 48px)", display: "flex", flexDirection: "column", overflow: "hidden" }}>
      <Box sx={{ display: "flex", alignItems: "center", gap: 1, px: 2, py: 1, borderBottom: "1px solid", borderColor: "divider" }}>
        <CodeIcon fontSize="small" color="primary" />
        <Typography variant="body2" fontWeight={600} sx={{ fontFamily: "monospace", minWidth: 0 }} noWrap>{fileName}</Typography>
        <Typography variant="caption" color="text.secondary" sx={{ ml: "auto", whiteSpace: "nowrap" }}>Live preview</Typography>
        <CopyButton value={code} />
      </Box>
      <Highlight prism={Prism as typeof Prism} theme={theme} code={code} language={"yaml" as Language}>
        {({ className, style, tokens, getLineProps, getTokenProps }) => (
          <Box component="pre" className={className} sx={{ m: 0, p: 2, flex: 1, overflow: "auto", fontFamily: "monospace", fontSize: "0.8125rem", lineHeight: 1.65, whiteSpace: "pre", ...style, backgroundColor: "transparent" }}>
            {tokens.map((line, i) => (
              <div key={i} {...getLineProps({ line })}>
                {line.map((token, key) => <span key={key} {...getTokenProps({ token })} />)}
              </div>
            ))}
          </Box>
        )}
      </Highlight>
    </Card>
  );
}

function SectionTitle({ icon, title, mb = 2 }: { icon: React.ReactNode; title: string; mb?: number }) {
  return <Box display="flex" alignItems="center" gap={1} mb={mb}><Box sx={{ color: "primary.main", display: "flex" }}>{icon}</Box><Typography variant="h6" sx={{ fontSize: "0.9375rem", fontWeight: 600 }}>{title}</Typography></Box>;
}

function PolicyIconBadge({ size }: { size: number }) {
  return <Box sx={{ width: size, height: size, flexShrink: 0, borderRadius: 1.5, display: "flex", alignItems: "center", justifyContent: "center", bgcolor: "rgba(var(--mui-palette-primary-mainChannel) / 0.1)", color: "primary.main", "& svg": { fontSize: size * 0.55 } }}><PolicyOutlinedIcon /></Box>;
}

function plural(count: number, noun: string, pluralNoun = `${noun}s`): string {
  return `${count} ${count === 1 ? noun : pluralNoun}`;
}

function PolicyCard({ policy, darkMode }: { policy: ScanPolicy; darkMode: boolean }) {
  const threshold = policy.spec.severityThreshold;
  const exemptions = policy.status?.activeExemptions ?? 0;
  return (
    <Card sx={{ height: "100%" }}>
      <CardActionArea component={Link} href={`/security-policies/${encodeURIComponent(policy.metadata.namespace)}/${encodeURIComponent(policy.metadata.name)}`} sx={{ height: "100%", display: "flex", alignItems: "flex-start" }}>
        <CardContent sx={{ width: "100%", pb: "16px !important" }}>
          <Stack direction="row" spacing={1.5} alignItems="center" sx={{ mb: 1.5 }}>
            <PolicyIconBadge size={36} />
            <Box sx={{ minWidth: 0, flex: 1 }}>
              <Typography noWrap sx={{ fontSize: "0.9375rem", fontWeight: 700, lineHeight: 1.3 }}>{policy.metadata.name}</Typography>
              <Typography variant="caption" color="text.secondary">Priority {policy.spec.priority ?? 0}</Typography>
            </Box>
            <Tooltip title="Severity threshold">
              {threshold
                ? <Chip label={threshold} size="small" color={thresholdColors[threshold]} sx={threshold === "NONE" ? undefined : { color: "#fff" }} />
                : <Chip label="Baseline" size="small" variant="outlined" />}
            </Tooltip>
          </Stack>
          <Stack direction="row" gap={0.75} flexWrap="wrap">
            <Chip size="small" variant="outlined" label={plural(policy.status?.matchedVersions ?? 0, "matched version")} />
            {exemptions > 0 && <Chip size="small" color="secondary" sx={{ color: "#fff" }} label={plural(exemptions, "exemption")} />}
            {policy.status?.supersededBy && <Chip size="small" color="warning" sx={{ color: darkMode ? "#fff" : undefined }} label={`Superseded by ${policy.status.supersededBy}`} />}
          </Stack>
        </CardContent>
      </CardActionArea>
    </Card>
  );
}

function PolicyListSkeleton() {
  return <Grid container spacing={2} role="status" aria-label="Loading policies">{Array.from({ length: 3 }, (_, index) => <Grid key={index} size={{ xs: 12, sm: 6, lg: 4 }}><Card><CardContent sx={{ pb: "16px !important" }}><Stack direction="row" spacing={1.5} alignItems="center" sx={{ mb: 1.5 }}><Skeleton variant="rounded" width={36} height={36} /><Box sx={{ flex: 1 }}><Skeleton variant="text" width="60%" height={22} /><Skeleton variant="text" width="30%" height={16} /></Box><Skeleton variant="rounded" width={72} height={24} /></Stack><Stack direction="row" spacing={0.75}><Skeleton variant="rounded" width={120} height={24} /><Skeleton variant="rounded" width={90} height={24} /></Stack></CardContent></Card></Grid>)}</Grid>;
}

function PolicyTargets({ value, onChange, errors, catalogItem }: { value: ScanPolicyTargetRef[]; onChange: (value: ScanPolicyTargetRef[]) => void; errors: Record<string, string>; catalogItem?: ScanPolicyCatalogItem }) {
  const resourcesFor = (kind: ScanPolicyTargetRef["kind"]) => {
    switch (kind) {
      case "Module": return catalogItem?.modules ?? [];
      case "Skill": return catalogItem?.skills ?? [];
      case "Agent": return catalogItem?.agents ?? [];
      default: return catalogItem?.providers ?? [];
    }
  };
  const firstTarget = (): ScanPolicyTargetRef | null => {
    const kinds: ScanPolicyTargetRef["kind"][] = ["Module", "Provider", "Skill", "Agent"];
    for (const kind of kinds) {
      const resource = resourcesFor(kind)[0];
      if (resource) {
        return { kind, name: resource.name };
      }
    }

    return null;
  };

  return <Box><Stack direction="row" justifyContent="space-between" alignItems="center"><Box><Typography variant="subtitle2" fontWeight={600}>Target resources</Typography><Typography variant="body2" color="text.secondary">Select from authorized onboarded Module, Provider, Skill, and Agent resources.</Typography></Box><Button startIcon={<AddIcon />} disabled={!firstTarget()} sx={{ flexShrink: 0, whiteSpace: "nowrap" }} onClick={() => { const target = firstTarget(); if (target) onChange([...value, target]); }}>Add target</Button></Stack>{errors.targets && <Alert severity="error" sx={{ mt: 1 }}>{errors.targets}</Alert>}{value.length === 0 ? <Typography color="text.secondary" sx={{ mt: 1 }}>No explicit targets.</Typography> : <Stack divider={<Divider />} spacing={2} sx={{ mt: 1 }}>{value.map((target, index) => { const resources = resourcesFor(target.kind); return <Grid container spacing={1} key={`${index}-${target.name}`} alignItems="start"><Grid size={{ xs: 12, sm: 3 }}><Select fullWidth aria-label={`Target kind ${index + 1}`} value={target.kind} onChange={(event) => { const kind = event.target.value as ScanPolicyTargetRef["kind"]; const next = [...value]; next[index] = { ...target, kind, name: resourcesFor(kind)[0]?.name ?? "" }; onChange(next); }}><MenuItem value="Module" disabled={!catalogItem?.modules.length}>Module</MenuItem><MenuItem value="Provider" disabled={!catalogItem?.providers.length}>Provider</MenuItem><MenuItem value="Skill" disabled={!catalogItem?.skills.length}>Skill</MenuItem><MenuItem value="Agent" disabled={!catalogItem?.agents.length}>Agent</MenuItem></Select></Grid><Grid size={{ xs: 12, sm: 4 }}><TextField select fullWidth required label="Resource name" value={target.name} error={Boolean(errors[`target-${index}`])} helperText={errors[`target-${index}`]} onChange={(event) => { const next = [...value]; next[index] = { ...target, name: event.target.value }; onChange(next); }}>{resources.map((resource) => <MenuItem key={resource.name} value={resource.name}>{resource.name}</MenuItem>)}</TextField></Grid><Grid size={{ xs: 10, sm: 4 }}><TextField fullWidth label="Version constraint" placeholder=">= 1.0.0, < 2.0.0" value={target.versions ?? ""} onChange={(event) => { const next = [...value]; next[index] = { ...target, versions: event.target.value || undefined }; onChange(next); }} /></Grid><Grid size={{ xs: 2, sm: 1 }}><IconButton aria-label={`Remove target ${index + 1}`} onClick={() => onChange(value.filter((_, itemIndex) => itemIndex !== index))}><DeleteOutlineIcon /></IconButton></Grid></Grid>; })}</Stack>}</Box>;
}

function ExemptionEditor({ value, onChange, errors }: { value: ScanPolicyExemption[]; onChange: (value: ScanPolicyExemption[]) => void; errors: Record<string, string> }) {
  const update = (index: number, changes: Partial<ScanPolicyExemption>) => { const next = [...value]; next[index] = { ...next[index], ...changes }; onChange(next); };
  return <Card><CardContent><Stack direction={{ xs: "column", sm: "row" }} justifyContent="space-between" alignItems={{ sm: "center" }} spacing={1}><Box><SectionTitle icon={<RemoveModeratorOutlinedIcon fontSize="small" />} title="Finding exemptions" mb={0.5} /><Typography variant="body2" color="text.secondary">Every exemption requires a reason. Leave the expiry empty for an indefinite exemption.</Typography></Box><Button startIcon={<AddIcon />} sx={{ flexShrink: 0, whiteSpace: "nowrap", alignSelf: { xs: "flex-start", sm: "center" } }} onClick={() => onChange([...value, { reason: "" }])}>Add exemption</Button></Stack>{value.length === 0 ? <Typography color="text.secondary" sx={{ mt: 2 }}>No exemptions configured.</Typography> : <Stack divider={<Divider />} spacing={2} sx={{ mt: 2 }}>{value.map((exemption, index) => <Box key={index}><Grid container spacing={2}><Grid size={{ xs: 12, sm: 6 }}><TextField fullWidth label="Vulnerability IDs" placeholder="CVE-2026-0001, aws-0057, or *" value={exemption.vulnerabilityIDs?.join(", ") ?? ""} onChange={(event) => update(index, { vulnerabilityIDs: splitLines(event.target.value) })} /></Grid><Grid size={{ xs: 12, sm: 6 }}><TextField fullWidth label="Package names" placeholder="stdlib, golang.org/x/net, or *" value={exemption.pkgNames?.join(", ") ?? ""} onChange={(event) => update(index, { pkgNames: splitLines(event.target.value) })} /></Grid><Grid size={{ xs: 12, sm: 6 }}><MultiSelectField label="Severities" options={exemptionSeverities} value={exemption.severities ?? []} onChange={(severities) => update(index, { severities: severities.length ? severities : undefined })} /></Grid><Grid size={{ xs: 12, sm: 6 }}><MultiSelectField label="Scan types" options={scanTypes} value={exemption.scanTypes ?? []} onChange={(selected) => update(index, { scanTypes: selected.length ? selected : undefined })} /></Grid><Grid size={{ xs: 12, sm: 7 }}><TextField fullWidth required label="Reason" value={exemption.reason} error={Boolean(errors[`exemption-${index}`])} helperText={errors[`exemption-${index}`]} onChange={(event) => update(index, { reason: event.target.value })} /></Grid><Grid size={{ xs: 10, sm: 4 }}><ExpiryPicker value={exemption.expires} onChange={(expires) => update(index, { expires })} /></Grid><Grid size={{ xs: 2, sm: 1 }}><IconButton aria-label={`Remove exemption ${index + 1}`} onClick={() => onChange(value.filter((_, itemIndex) => itemIndex !== index))}><DeleteOutlineIcon /></IconButton></Grid></Grid></Box>)}</Stack>}</CardContent></Card>;
}
