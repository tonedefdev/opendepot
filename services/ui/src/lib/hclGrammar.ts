import Prism from "prismjs";
import "prismjs/components/prism-hcl";

// Stock Prism only colors a block type when no label follows it, so `agent "name" {` lost its keyword color
// while `opendepot {` kept it. This rule matches the block type with any labels after it.
const hcl = Prism.languages.hcl;
const existingKeyword = hcl.keyword as Array<RegExp | Prism.TokenObject>;
hcl.keyword = [...existingKeyword, /[\w-]+(?=(?:\s+"(?:\\[\s\S]|[^\\"])*")*\s*\{)/];
