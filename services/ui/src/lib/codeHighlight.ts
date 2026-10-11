import Prism from "prismjs";
import "prismjs/components/prism-clike";
import "prismjs/components/prism-javascript";
import "prismjs/components/prism-typescript";
import "prismjs/components/prism-bash";
import "prismjs/components/prism-go";
import "prismjs/components/prism-python";
import "prismjs/components/prism-hcl";
import "prismjs/components/prism-yaml";

// prism-react-renderer and the grammar files resolve the Prism global, so it must be set before they run.
(typeof globalThis !== "undefined" ? globalThis : window).Prism = Prism;

const LANGUAGE_ALIASES: Record<string, string> = {
  sh: "bash",
  shell: "bash",
  zsh: "bash",
  golang: "go",
  py: "python",
  js: "javascript",
  ts: "typescript",
  yml: "yaml",
  terraform: "hcl",
  tf: "hcl",
};

const SUPPORTED_LANGUAGES = new Set(["bash", "go", "python", "typescript", "javascript", "yaml", "hcl"]);

// Maps a fenced-code language name to a supported grammar, or null when it is not highlighted.
export function resolvePrismLanguage(language: string): string | null {
  const key = language.toLowerCase();
  const canonical = LANGUAGE_ALIASES[key] ?? key;
  return SUPPORTED_LANGUAGES.has(canonical) ? canonical : null;
}

export { Prism };
