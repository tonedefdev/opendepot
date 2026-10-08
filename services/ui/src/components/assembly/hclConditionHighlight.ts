import rehypePrismGenerator from "rehype-prism-plus/generator";
import type { Grammar } from "prismjs";
import type { Pluggable } from "unified";
import { refractor } from "refractor";
import hcl from "refractor/lang/hcl";

export const builtInFunctions = {
  abs: { hint: "Returns the absolute value of a number." },
  abspath: { hint: "Converts a path to an absolute filesystem path." },
  alltrue: { hint: "Returns true when every element in a collection is true." },
  anytrue: { hint: "Returns true when at least one element in a collection is true." },
  assumeequal: { hint: "Asserts that two values are equal and returns the expected value.", docsSlug: "assume_family#assumeequal-function" },
  assumelistlength: { hint: "Asserts that a list has an exact length.", docsSlug: "assume_family#assumelistlength-function" },
  assumelistlengthmax: { hint: "Asserts that a list does not exceed a maximum length.", docsSlug: "assume_family#assumelistlengthmax-function" },
  assumelistlengthmin: { hint: "Asserts that a list has at least a minimum length.", docsSlug: "assume_family#assumelistlengthmin-function" },
  assumemaplength: { hint: "Asserts that a map has an exact number of elements.", docsSlug: "assume_family#assumemaplength-function" },
  assumemaplengthmax: { hint: "Asserts that a map does not exceed a maximum number of elements.", docsSlug: "assume_family#assumemaplengthmax-function" },
  assumemaplengthmin: { hint: "Asserts that a map has at least a minimum number of elements.", docsSlug: "assume_family#assumemaplengthmin-function" },
  assumenotnull: { hint: "Asserts that a value is not null.", docsSlug: "assume_family#assumenotnull-function" },
  assumesetlength: { hint: "Asserts that a set has an exact number of elements.", docsSlug: "assume_family#assumesetlength-function" },
  assumesetlengthmax: { hint: "Asserts that a set does not exceed a maximum number of elements.", docsSlug: "assume_family#assumesetlengthmax-function" },
  assumesetlengthmin: { hint: "Asserts that a set has at least a minimum number of elements.", docsSlug: "assume_family#assumesetlengthmin-function" },
  assumestringprefix: { hint: "Asserts that a string begins with the given prefix.", docsSlug: "assume_family#assumestringprefix-function" },
  base64decode: { hint: "Decodes a Base64-encoded string." },
  base64encode: { hint: "Encodes a string as Base64." },
  base64gunzip: { hint: "Decodes Base64 text and decompresses the result with gzip." },
  base64gzip: { hint: "Compresses a string with gzip and encodes the result as Base64." },
  base64sha256: { hint: "Returns the SHA-256 hash of a string encoded as Base64." },
  base64sha512: { hint: "Returns the SHA-512 hash of a string encoded as Base64." },
  basename: { hint: "Returns the final component of a filesystem path." },
  bcrypt: { hint: "Hashes a string using the bcrypt password-hashing algorithm." },
  can: { hint: "Tests whether an expression can be evaluated without an error." },
  ceil: { hint: "Rounds a number up to the nearest integer." },
  chomp: { hint: "Removes newline characters from the end of a string." },
  chunklist: { hint: "Splits a list into fixed-size sublists." },
  cidrcontains: { hint: "Tests whether an IP address or CIDR prefix is within a network prefix." },
  cidrhost: { hint: "Returns a host IP address from a CIDR network and host number." },
  cidrnetmask: { hint: "Returns the network mask for an IPv4 CIDR range." },
  cidrsubnet: { hint: "Calculates a subnet CIDR range within a parent network." },
  cidrsubnets: { hint: "Calculates consecutive subnet CIDR ranges within a parent network." },
  coalesce: { hint: "Returns the first argument that is neither null nor an empty string." },
  coalescelist: { hint: "Returns the first non-empty list." },
  compact: { hint: "Removes empty strings from a list of strings." },
  concat: { hint: "Combines multiple lists into one list." },
  contains: { hint: "Tests whether a collection contains a given value." },
  convert: { hint: "Explicitly converts a value to a requested type." },
  csvdecode: { hint: "Decodes CSV text into a list of objects." },
  dirname: { hint: "Returns the directory portion of a filesystem path." },
  distinct: { hint: "Removes duplicate elements from a list." },
  element: { hint: "Returns the list element at the given index." },
  endswith: { hint: "Tests whether a string ends with a specified suffix." },
  ephemeralasnull: { hint: "Replaces ephemeral values within a value with null." },
  file: { hint: "Reads a UTF-8 text file from the configuration filesystem." },
  filebase64: { hint: "Reads a file and returns its contents as Base64." },
  filebase64sha256: { hint: "Returns the Base64-encoded SHA-256 hash of a file's contents." },
  filebase64sha512: { hint: "Returns the Base64-encoded SHA-512 hash of a file's contents." },
  fileexists: { hint: "Tests whether a file exists at the given path." },
  filemd5: { hint: "Returns the hexadecimal MD5 hash of a file's contents." },
  fileset: { hint: "Returns the regular file names matching a path pattern." },
  filesha1: { hint: "Returns the hexadecimal SHA-1 hash of a file's contents." },
  filesha256: { hint: "Returns the hexadecimal SHA-256 hash of a file's contents." },
  filesha512: { hint: "Returns the hexadecimal SHA-512 hash of a file's contents." },
  flatten: { hint: "Flattens nested lists into a single list." },
  floor: { hint: "Rounds a number down to the nearest integer." },
  format: { hint: "Formats values using a printf-style format string." },
  formatdate: { hint: "Formats a timestamp using the requested date and time layout." },
  formatlist: { hint: "Formats each element of a list using a printf-style format string." },
  index: { hint: "Returns the index of a value in a list.", docsSlug: "index_function" },
  indent: { hint: "Adds indentation to the lines of a string." },
  issensitive: { hint: "Tests whether a value is marked as sensitive." },
  join: { hint: "Joins a list of strings with a separator." },
  jsondecode: { hint: "Parses a JSON string into a Terraform value." },
  jsonencode: { hint: "Encodes a Terraform value as a JSON string." },
  keys: { hint: "Returns the keys of a map in lexicographical order." },
  length: { hint: "Returns the number of elements in a collection or characters in a string." },
  log: { hint: "Returns the logarithm of a number for a specified base." },
  lookup: { hint: "Gets a map value by key, with an optional default." },
  lower: { hint: "Converts all letters in a string to lowercase." },
  matchkeys: { hint: "Selects list elements whose corresponding keys occur in another list." },
  max: { hint: "Returns the greatest number from the given values." },
  md5: { hint: "Returns the hexadecimal MD5 hash of a string." },
  merge: { hint: "Combines maps or objects into one value." },
  min: { hint: "Returns the smallest number from the given values." },
  nonsensitive: { hint: "Removes the sensitive marking from a value." },
  one: { hint: "Returns the only element of a collection, or null when it is empty." },
  parseint: { hint: "Parses a string as an integer using the specified base." },
  pathexpand: { hint: "Expands a leading tilde to the current user's home directory." },
  plantimestamp: { hint: "Returns the timestamp captured for the current plan." },
  pow: { hint: "Raises a number to a specified power." },
  range: { hint: "Generates a list of numbers in a sequence." },
  regex: { hint: "Returns the first regular-expression match and captured groups." },
  regexall: { hint: "Returns all regular-expression matches and captured groups." },
  replace: { hint: "Replaces matching text in a string." },
  reverse: { hint: "Reverses the elements of a list or characters of a string." },
  rsadecrypt: { hint: "Decrypts a Base64-encoded RSA-encrypted message." },
  sensitive: { hint: "Marks a value as sensitive to hide it from ordinary output." },
  setintersection: { hint: "Returns elements shared by all input sets." },
  setproduct: { hint: "Returns the Cartesian product of the input collections." },
  setsubtract: { hint: "Returns elements from the first set that are absent from the others." },
  setunion: { hint: "Returns the union of all input sets." },
  sha1: { hint: "Returns the SHA-1 hash of a string." },
  sha256: { hint: "Returns the SHA-256 hash of a string." },
  sha512: { hint: "Returns the hexadecimal SHA-512 hash of a string." },
  signum: { hint: "Returns -1, 0, or 1 according to the sign of a number." },
  slice: { hint: "Returns a portion of a list between two indexes." },
  sort: { hint: "Sorts a list of strings in lexicographical order." },
  split: { hint: "Splits a string wherever the separator occurs." },
  startswith: { hint: "Tests whether a string begins with a specified prefix." },
  strcontains: { hint: "Tests whether a string contains a specified substring." },
  strrev: { hint: "Reverses the characters in a string." },
  substr: { hint: "Returns a substring starting at the given character index." },
  sum: { hint: "Returns the total of the numbers in a list." },
  templatefile: { hint: "Reads and renders a template file using the supplied variables." },
  templatestring: { hint: "Renders a string as a template using the supplied variables." },
  textdecodebase64: { hint: "Decodes Base64 text and interprets it using a character encoding." },
  textencodebase64: { hint: "Encodes text using a character encoding, then returns Base64." },
  timeadd: { hint: "Adds a duration to an RFC 3339 timestamp." },
  timecmp: { hint: "Compares two timestamps and returns their ordering." },
  timestamp: { hint: "Returns the current date and time as a string." },
  title: { hint: "Capitalizes the first letter of each word in a string." },
  tobool: { hint: "Converts a value to a boolean." },
  tomap: { hint: "Converts a value to a map." },
  tonumber: { hint: "Converts a value to a number." },
  tolist: { hint: "Converts a value to a list." },
  toset: { hint: "Converts a value to a set, removing duplicates." },
  tostring: { hint: "Converts a value to a string." },
  transpose: { hint: "Swaps the keys and values of a map of lists of strings." },
  trim: { hint: "Removes specified characters from both ends of a string." },
  trimprefix: { hint: "Removes a prefix from a string when it is present." },
  trimsuffix: { hint: "Removes a suffix from a string when it is present." },
  trimspace: { hint: "Removes whitespace from both ends of a string." },
  try: { hint: "Returns the first expression that does not produce an error." },
  type: { hint: "Returns the type of a value." },
  upper: { hint: "Converts all letters in a string to uppercase." },
  urldecode: { hint: "Decodes URL-escaped characters in a string." },
  urlencode: { hint: "Escapes a string for use in a URL." },
  uuid: { hint: "Generates a universally unique identifier." },
  uuidv5: { hint: "Generates a version 5 UUID from a namespace and name." },
  values: { hint: "Returns map values in the order of their corresponding keys." },
  yamldecode: { hint: "Decodes a YAML string into an OpenTofu value." },
  yamlencode: { hint: "Encodes an OpenTofu value as a YAML string." },
  zipmap: { hint: "Creates a map by pairing keys and values from two lists." },
} as const;

export function getHclFunctionHint(source: string, caretPosition: number) {
  const calls = /\b([A-Za-z_][\w]*)\s*\(/g;
  let selected: { name: string; hint: string; docsUrl: string; start: number } | undefined;
  let match: RegExpExecArray | null;

  while ((match = calls.exec(source)) !== null) {
    const name = match[1].toLowerCase() as keyof typeof builtInFunctions;
    const metadata = builtInFunctions[name];
    if (!metadata) continue;

    const openParen = calls.lastIndex - 1;
    let depth = 1;
    let inString = false;
    let escaped = false;
    let end = source.length;
    for (let index = openParen + 1; index < source.length; index += 1) {
      const character = source[index];
      if (inString) {
        if (escaped) escaped = false;
        else if (character === "\\") escaped = true;
        else if (character === '"') inString = false;
        continue;
      }
      if (character === '"') inString = true;
      else if (character === "(") depth += 1;
      else if (character === ")" && --depth === 0) {
        end = index;
        break;
      }
    }

    if (caretPosition < match.index || caretPosition > end + 1) continue;
    if (selected && selected.start > match.index) continue;
    const docsPath = "docsSlug" in metadata ? metadata.docsSlug : name;
    const [slug, anchor] = docsPath.split("#");
    selected = {
      name,
      hint: metadata.hint,
      docsUrl: `https://opentofu.org/docs/language/functions/${slug}/${anchor ? `#${anchor}` : ""}`,
      start: match.index,
    };
  }

  return selected;
}

export function extendHclGrammar(grammar: Grammar): void {
  type NestedGrammarToken = { inside?: Record<string, unknown> };
  const grammarTokens = grammar as unknown as Record<string, unknown>;
  grammarTokens.variable = /\b(?:var|local|each|module|data|path|terraform|self|count)\b(?:\.[A-Za-z_][\w-]*)+/;
  grammarTokens.function = new RegExp(`\\b(?:${Object.keys(builtInFunctions).join("|")})\\b(?=\\s*\\()`, "i");
  grammarTokens.expressionKeyword = { pattern: /\b(?:for|in|if|null)\b/, alias: "keyword" };
  grammarTokens.operator = /(?:==|!=|<=|>=|&&|\|\||[=<>!+*/%?-])/;

  const stringToken = grammarTokens.string as NestedGrammarToken | undefined;
  const interpolation = stringToken?.inside?.interpolation as NestedGrammarToken | undefined;
  if (interpolation?.inside) {
    interpolation.inside.type = {
      pattern: /\b(?:count|data|each|local|module|path|self|terraform|var)\b(?:\.[\w*]+)+/i,
      alias: "variable",
    };
  }
}

if (!refractor.registered("hcl")) {
  refractor.register(hcl);
}

extendHclGrammar(refractor.languages.hcl as Grammar);

export const hclConditionPlugins: Pluggable[] = [[rehypePrismGenerator(refractor), { ignoreMissing: true }]];
export { refractor as hclConditionRefractor };

export function hclEditorColorVariables(mode: "dark" | "light") {
  return {
    "--color-prettylights-syntax-sublimelinter-gutter-mark": mode === "dark" ? "#b1bac4" : "#57606a",
    "--color-prettylights-syntax-string-regexp": mode === "dark" ? "#d2a8ff" : "#8250df",
  };
}