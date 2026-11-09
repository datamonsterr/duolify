const commercialPatterns = [
  /\$\s?\d[\d,.]*/,
  /\bUSD\s?\d[\d,.]*/i,
  /\b\d[\d,.]*(?:\.\d+)?\s*(?:credits?|USD|\/\s?(?:month|year))\b/i,
  /\b(?:price|price_minor|amount_minor|credits|monthly_credits|daily_free_credits|concurrent_generation)\s*:\s*\d/i,
  /\b(?:price|price_minor|amount_minor|credits|monthly_credits|daily_free_credits|concurrent_generation)\s*=\s*\d/i,
  /\.credits\s*(?:\?\?|\|\||=)\s*\d/,
];

export function hasCommercialPricingLiteral(line: string) {
  if (line.includes("@noncommercial-number")) return false;
  return commercialPatterns.some((pattern) => pattern.test(line));
}
