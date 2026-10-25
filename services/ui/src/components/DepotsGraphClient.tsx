"use client";

import * as React from "react";
import Box from "@mui/material/Box";
import Chip from "@mui/material/Chip";
import Autocomplete from "@mui/material/Autocomplete";
import TextField from "@mui/material/TextField";
import DepotGraph, { agentVersionItems, type AgentVersionMeta } from "@/components/DepotGraph";
import { DepotKindIcon } from "@/components/DepotKindIcon";
import type { BrowseDepotGraph, BrowseStorageConfig } from "@/lib/api";
import { KIND_CHIP_COLORS, KIND_CHIP_TEXT } from "@/lib/kindColors";


type DepotOption = {
	id: string;
	key: string;
	label: string;
};

interface DepotsGraphClientProps {
	graph: BrowseDepotGraph;
	moduleVersionsByKey: Record<string, string[]>;
	providerVersionsByKey: Record<string, string[]>;
	providerVersionMetaByKey: Record<string, Array<{ version: string; name?: string; fileName?: string; checksum?: string; lastScanned?: string }>>;
	moduleVersionMetaByKey: Record<string, Array<{ version: string; fileName?: string; checksum?: string; lastScanned?: string }>>;
	moduleDetailByKey: Record<string, { storageConfig?: BrowseStorageConfig; githubAuthenticated?: boolean }>;
	agentVersionMetaByKey?: Record<string, AgentVersionMeta[]>;
}

function makeDepotKey(namespace: string, name: string): string {
	return `${namespace}/${name}`;
}

export default function DepotsGraphClient({ graph, moduleVersionsByKey, providerVersionsByKey, providerVersionMetaByKey = {}, moduleVersionMetaByKey, moduleDetailByKey, agentVersionMetaByKey = {} }: DepotsGraphClientProps) {
	const depotOptions = React.useMemo<DepotOption[]>(
		() =>
			graph.depots.map((depot) => ({
				id: depot.id,
				key: makeDepotKey(depot.namespace, depot.name),
				label: `${depot.namespace} / ${depot.name}`,
			})),
		[graph.depots],
	);

	const [selectedDepotKeys, setSelectedDepotKeys] = React.useState<string[]>([]);

	const selectedDepotOptions = React.useMemo(
		() => depotOptions.filter((option) => selectedDepotKeys.includes(option.key)),
		[depotOptions, selectedDepotKeys],
	);

	const filteredGraph = React.useMemo<BrowseDepotGraph>(() => {
		if (selectedDepotKeys.length === 0) {
			return graph;
		}

		const selectedKeySet = new Set(selectedDepotKeys);
		const depots = graph.depots.filter((depot) => selectedKeySet.has(makeDepotKey(depot.namespace, depot.name)));

		const moduleKeySet = new Set<string>();
		const providerKeySet = new Set<string>();
		const skillKeySet = new Set<string>();
		const agentKeySet = new Set<string>();
		const moduleIdSet = new Set<string>();
		const providerIdSet = new Set<string>();

		depots.forEach((depot) => {
			(depot.managedModuleNames ?? []).forEach((name) => {
				moduleKeySet.add(makeDepotKey(depot.namespace, name));
			});
			(depot.managedProviderNames ?? []).forEach((name) => {
				providerKeySet.add(makeDepotKey(depot.namespace, name));
			});
			(depot.managedSkillNames ?? []).forEach((name) => {
				skillKeySet.add(makeDepotKey(depot.namespace, name));
			});
			(depot.managedAgentNames ?? []).forEach((name) => {
				agentKeySet.add(makeDepotKey(depot.namespace, name));
			});
		});

		const selectedDepotIds = new Set(depots.map((depot) => depot.id));
		graph.edges.forEach((edge) => {
			if (!selectedDepotIds.has(edge.source) && !selectedDepotIds.has(edge.target)) {
				return;
			}

			const graphModule = graph.modules.find((item) => item.id === edge.source || item.id === edge.target);
			if (graphModule) {
				moduleIdSet.add(graphModule.id);
			}

			const provider = graph.providers.find((item) => item.id === edge.source || item.id === edge.target);
			if (provider) {
				providerIdSet.add(provider.id);
			}
		});

		const modules = graph.modules.filter((module) => {
			const moduleKey = makeDepotKey(module.namespace, module.name);
			return moduleKeySet.has(moduleKey) || moduleIdSet.has(module.id);
		});

		const providers = graph.providers.filter((provider) => {
			const providerKey = makeDepotKey(provider.namespace, provider.name);
			return providerKeySet.has(providerKey) || providerIdSet.has(provider.id);
		});

		const skills = graph.skills.filter((skill) => skillKeySet.has(makeDepotKey(skill.namespace, skill.name)));
		const agents = graph.agents.filter((agent) => agentKeySet.has(makeDepotKey(agent.namespace, agent.name)));

		const allowedNodeIds = new Set<string>([
			...depots.map((depot) => depot.id),
			...modules.map((module) => module.id),
			...providers.map((provider) => provider.id),
			...skills.map((skill) => skill.id),
			...agents.map((agent) => agent.id),
		]);

		const edges = graph.edges.filter(
			(edge) => allowedNodeIds.has(edge.source) && allowedNodeIds.has(edge.target),
		);

		return {
			...graph,
			depots,
			modules,
			providers,
			skills,
			agents,
			edges,
			summary: {
				totalDepots: depots.length,
				totalModules: modules.length,
				totalProviders: providers.length,
				totalSkills: skills.length,
				totalAgents: agents.length,
			},
		};
	}, [graph, selectedDepotKeys]);

	const totalVersions = React.useMemo(() => {
		const moduleVersionTotal = filteredGraph.modules.reduce((sum, module) => {
			const key = makeDepotKey(module.namespace, module.name);
			const versionMeta = moduleVersionMetaByKey[key] ?? [];
			if (versionMeta.length > 0) {
				return sum + new Set(versionMeta.map((entry) => entry.version).filter(Boolean)).size;
			}
			const versions = moduleVersionsByKey[key] ?? (module.latestVersion ? [module.latestVersion] : []);
			return sum + new Set(versions.filter(Boolean)).size;
		}, 0);

		const providerVersionTotal = filteredGraph.providers.reduce((sum, provider) => {
			const key = makeDepotKey(provider.namespace, provider.name);
			const providerMeta = providerVersionMetaByKey[key] ?? [];
			if (providerMeta.length > 0) {
				return sum + new Set(providerMeta.map((entry) => `${entry.version}|${entry.name ?? ""}`)).size;
			}
			const versions = providerVersionsByKey[key] ?? [];
			return sum + new Set(versions.filter(Boolean)).size;
		}, 0);

		const agentVersionTotal = [
			...filteredGraph.skills.map((skill) => ({ item: skill, kind: "skill" as const })),
			...filteredGraph.agents.map((agent) => ({ item: agent, kind: "agent" as const })),
		].reduce((sum, { item, kind }) => sum + agentVersionItems(kind, item, agentVersionMetaByKey).length, 0);

		return moduleVersionTotal + providerVersionTotal + agentVersionTotal;
	}, [filteredGraph.agents, filteredGraph.modules, filteredGraph.providers, filteredGraph.skills, agentVersionMetaByKey, moduleVersionMetaByKey, moduleVersionsByKey, providerVersionMetaByKey, providerVersionsByKey]);

	return (
		<>
			<Box sx={{ display: { xs: "grid", sm: "inline-grid" }, width: { xs: "100%", sm: "auto" }, mb: 3 }}>
				<Box sx={{ display: "flex", gap: 1.5, alignItems: "center", mb: 2 }}>
					<Autocomplete
						multiple
						size="small"
						options={depotOptions}
						value={selectedDepotOptions}
						onChange={(_event, value) => {
							setSelectedDepotKeys(value.map((item) => item.key));
						}}
						isOptionEqualToValue={(option, value) => option.key === value.key}
						getOptionLabel={(option) => option.label}
						renderInput={(params) => (
							<TextField
								{...params}
								label="Filter Depots"
								placeholder="Select depot(s)"
							/>
						)}
						sx={{ flex: 1, minWidth: 0 }}
					/>
					{selectedDepotKeys.length > 0 && (
						<Chip
							label="Clear"
							onClick={() => setSelectedDepotKeys([])}
							variant="outlined"
							size="small"
							sx={{ borderColor: "divider", color: "text.secondary", flexShrink: 0 }}
						/>
					)}
				</Box>

				<Box sx={{ display: "flex", gap: 1, flexWrap: "wrap", "& .MuiChip-icon": { color: "inherit !important" } }}>
				<Chip
					label={`${filteredGraph.summary.totalDepots} depots`}
					icon={<DepotKindIcon kind="depot" sx={{ fontSize: 16 }} />}
					size="small"
					sx={{
						color: KIND_CHIP_TEXT,
						bgcolor: KIND_CHIP_COLORS.depot,
					}}
				/>
				<Chip
					label={`${filteredGraph.summary.totalModules} modules`}
					icon={<DepotKindIcon kind="module" sx={{ fontSize: 16 }} />}
					size="small"
					sx={{
						color: KIND_CHIP_TEXT,
						bgcolor: KIND_CHIP_COLORS.module,
					}}
				/>
				<Chip
					label={`${filteredGraph.summary.totalProviders} providers`}
					icon={<DepotKindIcon kind="provider" sx={{ fontSize: 16 }} />}
					size="small"
					sx={{
						color: KIND_CHIP_TEXT,
						bgcolor: KIND_CHIP_COLORS.provider,
					}}
				/>
				<Chip
					label={`${filteredGraph.summary.totalSkills} skills`}
					icon={<DepotKindIcon kind="skill" sx={{ fontSize: 16 }} />}
					size="small"
					sx={{
						color: KIND_CHIP_TEXT,
						bgcolor: KIND_CHIP_COLORS.skill,
					}}
				/>
				<Chip
					label={`${filteredGraph.summary.totalAgents} agents`}
					icon={<DepotKindIcon kind="agent" sx={{ fontSize: 16 }} />}
					size="small"
					sx={{
						color: KIND_CHIP_TEXT,
						bgcolor: KIND_CHIP_COLORS.agent,
					}}
				/>
				<Chip
					label={`${totalVersions} versions`}
					icon={<DepotKindIcon kind="version" sx={{ fontSize: 16 }} />}
					size="small"
					sx={{
						color: KIND_CHIP_TEXT,
						bgcolor: KIND_CHIP_COLORS.version,
					}}
				/>
			</Box>
			</Box>

			<DepotGraph
				graph={filteredGraph}
				moduleVersionsByKey={moduleVersionsByKey}
				providerVersionsByKey={providerVersionsByKey}
				providerVersionMetaByKey={providerVersionMetaByKey}
				moduleVersionMetaByKey={moduleVersionMetaByKey}
				moduleDetailByKey={moduleDetailByKey}
				agentVersionMetaByKey={agentVersionMetaByKey}
			/>
		</>
	);
}
