export function formatCredits(value: number, locale?: string) {
  return new Intl.NumberFormat(locale).format(value);
}

export function formatMoney(
  amountMinor: number,
  currency: string,
  locale?: string,
) {
  return new Intl.NumberFormat(locale, {
    style: "currency",
    currency,
  }).format(amountMinor / 100);
}
