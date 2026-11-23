// JEV_INPUT_USD_PER_MILLION_TOKENS is Jev's current list price for input tokens. Jev does not bill output tokens.
export const JEV_INPUT_USD_PER_MILLION_TOKENS = 0.042;

const usdFormatter = new Intl.NumberFormat("en-US", {
  style: "currency",
  currency: "USD",
  minimumFractionDigits: 2,
  maximumFractionDigits: 6,
});

// formatJevInputCost estimates the cost of the given input tokens at the Jev list price.
export function formatJevInputCost(inputTokens: number): string {
  return usdFormatter.format((inputTokens * JEV_INPUT_USD_PER_MILLION_TOKENS) / 1_000_000);
}
