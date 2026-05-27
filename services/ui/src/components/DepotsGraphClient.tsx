"use client";

import * as React from "react";
import Box from "@mui/material/Box";
import Chip from "@mui/material/Chip";
import Autocomplete from "@mui/material/Autocomplete";
import TextField from "@mui/material/TextField";
import DepotGraph from "@/components/DepotGraph";
import type { BrowseDepotGraph, BrowseStorageConfig } from "@/lib/api";

const DEPOT_COLOR = "#04cfd0";
const MODULE_COLOR = "#03deb8";
const PROVIDER_COLOR = "#047df1";
const VERSION_COLOR = "#8b949e";

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
}

function makeDepotKey(namespace: string, name: string): string {
	return `${namespace}/${name}`;
}

export default function DepotsGraphClient({ graph, moduleVersionsByKey, providerVersionsByKey, providerVersionMetaByKey = {}, moduleVersionMetaByKey, moduleDetailByKey }: DepotsGraphClientProps) {
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
		const moduleIdSet = new Set<string>();
		const providerIdSet = new Set<string>();

		depots.forEach((depot) => {
			(depot.managedModuleNames ?? []).forEach((name) => {
				moduleKeySet.add(makeDepotKey(depot.namespace, name));
			});
			(depot.managedProviderNames ?? []).forEach((name) => {
				providerKeySet.add(makeDepotKey(depot.namespace, name));
			});
		});

		const selectedDepotIds = new Set(depots.map((depot) => depot.id));
		graph.edges.forEach((edge) => {
			if (!selectedDepotIds.has(edge.source) && !selectedDepotIds.has(edge.target)) {
				return;
			}

			const module = graph.modules.find((item) => item.id === edge.source || item.id === edge.target);
			if (module) {
				moduleIdSet.add(module.id);
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

		const allowedNodeIds = new Set<string>([
			...depots.map((depot) => depot.id),
			...modules.map((module) => module.id),
			...providers.map((provider) => provider.id),
		]);

		const edges = graph.edges.filter(
			(edge) => allowedNodeIds.has(edge.source) && allowedNodeIds.has(edge.target),
		);

		return {
			...graph,
			depots,
			modules,
			providers,
			edges,
			summary: {
				totalDepots: depots.length,
				totalModules: modules.length,
				totalProviders: providers.length,
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

		return moduleVersionTotal + providerVersionTotal;
	}, [filteredGraph.modules, filteredGraph.providers, moduleVersionMetaByKey, moduleVersionsByKey, providerVersionMetaByKey, providerVersionsByKey]);

	return (
		<>
			<Box sx={{ display: "flex", gap: 1.5, flexWrap: "wrap", alignItems: "center", mb: 2 }}>
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
					sx={{ width: { xs: "100%", sm: 280 }, maxWidth: 280 }}
				/>
				{selectedDepotKeys.length > 0 && (
					<Chip
						label="Clear"
						onClick={() => setSelectedDepotKeys([])}
						variant="outlined"
						size="small"
						sx={{ borderColor: "rgba(240,246,252,0.3)", color: "text.secondary" }}
					/>
				)}
			</Box>

			<Box sx={{ display: "flex", gap: 1, mb: 3, flexWrap: "wrap" }}>
				<Chip
					label={`${filteredGraph.summary.totalDepots} depots`}
					size="small"
					variant="outlined"
					sx={{ color: DEPOT_COLOR, borderColor: DEPOT_COLOR }}
				/>
				<Chip
					label={`${filteredGraph.summary.totalModules} modules`}
					size="small"
					variant="outlined"
					sx={{ color: MODULE_COLOR, borderColor: MODULE_COLOR }}
				/>
				<Chip
					label={`${filteredGraph.summary.totalProviders} providers`}
					size="small"
					variant="outlined"
					sx={{ color: PROVIDER_COLOR, borderColor: PROVIDER_COLOR }}
				/>
				<Chip
					label={`${totalVersions} versions`}
					size="small"
					variant="outlined"
					sx={{ color: VERSION_COLOR, borderColor: VERSION_COLOR }}
				/>
			</Box>

			<DepotGraph
				graph={filteredGraph}
				moduleVersionsByKey={moduleVersionsByKey}
				providerVersionsByKey={providerVersionsByKey}
				providerVersionMetaByKey={providerVersionMetaByKey}
				moduleVersionMetaByKey={moduleVersionMetaByKey}
				moduleDetailByKey={moduleDetailByKey}
			/>
		</>
	);
}
