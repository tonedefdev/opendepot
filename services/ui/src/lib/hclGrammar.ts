import Prism from "prismjs";
import "prismjs/components/prism-hcl";

// Stock Prism only colors a block type when no label follows it, so `agent "name" {` lost its keyword color
// while `opendepot {` kept it. This rule matches the block type with any labels after it.
const hcl = Prism.languages.hcl;
const existingKeyword = hcl.keyword as Array<RegExp | Prism.TokenObject>;
// Each quoted label is unrolled so every character matches exactly one branch. The lookbehind starts the match at a
// word boundary, so a long identifier is not rescanned from each of its inner positions.
hcl.keyword = [...existingKeyword, /(?<![\w-])[\w-]+(?=(?:\s+"[^"\\]*(?:\\[\s\S][^"\\]*)*")*\s*\{)/];
