//! Catalogue economics and payment mapping verification.
#[cfg(test)]
mod tests;

use anyhow::{Result, bail};
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Offer {
    pub sku: String,
    pub currency: String,
    pub amount_minor: u32,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct StripePrice {
    pub id: String,
    pub active: bool,
    pub currency: String,
    pub amount_minor: u32,
    pub recurring_type: String, // "one_time" or "recurring"
    pub interval: Option<String>,
    pub interval_count: Option<u32>,
    pub environment: String,
}

impl StripePrice {
    pub fn matches(
        &self,
        target_env: &str,
        expected: &Offer,
        expected_interval: &str,
    ) -> Result<()> {
        if !self.active {
            bail!("Stripe price {} is inactive", self.id);
        }
        if !self.environment.eq_ignore_ascii_case(target_env) {
            bail!(
                "Stripe price {} environment mismatch: expected {}, got {}",
                self.id,
                target_env,
                self.environment
            );
        }
        if !self.currency.eq_ignore_ascii_case(&expected.currency) {
            bail!(
                "Stripe price {} currency {} does not match catalogue currency {}",
                self.id,
                self.currency,
                expected.currency
            );
        }
        if self.amount_minor != expected.amount_minor {
            bail!(
                "Stripe price {} amount {} does not match catalogue amount {}",
                self.id,
                self.amount_minor,
                expected.amount_minor
            );
        }
        if expected_interval == "one_time" {
            if self.recurring_type == "recurring" {
                bail!("Stripe price {} is recurring; expected one-time", self.id);
            }
        } else {
            if self.recurring_type != "recurring" {
                bail!("Stripe price {} is one-time; expected recurring", self.id);
            }
            if let Some(ref intv) = self.interval {
                if !intv.eq_ignore_ascii_case(expected_interval) {
                    bail!(
                        "Stripe price {} interval {} does not match expected {}",
                        self.id,
                        intv,
                        expected_interval
                    );
                }
            } else {
                bail!("Stripe price {} missing recurring interval", self.id);
            }
        }
        Ok(())
    }
}

#[derive(Debug, Clone)]
pub struct StandardActionCost {
    pub action: String,
    pub credits: u32,
    pub protected_cost_usd: f64,
}

impl StandardActionCost {
    pub fn cost_per_credit(&self) -> f64 {
        if self.credits == 0 {
            return f64::INFINITY;
        }
        self.protected_cost_usd / f64::from(self.credits)
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ProviderRateConfig {
    pub pack_fee_rate: f64,
    pub pack_fixed_fee_usd: f64,
    pub subscription_fee_rate: f64,
    pub subscription_fixed_fee_usd: f64,
    pub refund_reserve_rate: f64,
}

impl Default for ProviderRateConfig {
    fn default() -> Self {
        Self {
            pack_fee_rate: 0.029,
            pack_fixed_fee_usd: 0.30,
            subscription_fee_rate: 0.036,
            subscription_fixed_fee_usd: 0.30,
            refund_reserve_rate: 0.020,
        }
    }
}

#[derive(Debug, Clone)]
pub struct SkuEconomics {
    pub sku: String,
    pub sale_price_usd: f64,
    pub credits: u32,
    pub fee_rate: f64,
    pub fixed_fee_usd: f64,
    pub refund_rate: f64,
}

impl SkuEconomics {
    pub fn calculate_margin(&self, cost_ceiling_usd_per_credit: f64) -> f64 {
        let net_receipts =
            self.sale_price_usd * (1.0 - self.fee_rate - self.refund_rate) - self.fixed_fee_usd;
        if net_receipts <= 0.0 {
            return -100.0;
        }
        let protected_cost = f64::from(self.credits) * cost_ceiling_usd_per_credit;
        100.0 * (net_receipts - protected_cost) / net_receipts
    }
}

pub fn verify_daily_allowance_sizing(
    daily_credits: u32,
    quick_note_credits: u32,
    tutor_short_credits: u32,
    tutor_long_credits: u32,
) -> Result<()> {
    let expected_short = 3 * quick_note_credits + 8 * tutor_short_credits;
    let expected_long = 3 * quick_note_credits + 5 * tutor_long_credits;
    if daily_credits != expected_short || daily_credits != expected_long {
        bail!(
            "daily_free.credits must match exact sizing: 3 quick notes and 8 short (or 5 long) tutor messages (expected {}, got {})",
            expected_short,
            daily_credits
        );
    }
    Ok(())
}

pub fn verify_action_cost_ceiling(
    actions: &[StandardActionCost],
    ceiling_usd_per_credit: f64,
    override_reason: Option<&str>,
) -> Result<()> {
    for a in actions {
        let cpc = a.cost_per_credit();
        if cpc > ceiling_usd_per_credit {
            if override_reason.is_some_and(|r| !r.trim().is_empty()) {
                continue;
            }
            bail!(
                "standard action {} cost per credit {:.6} exceeds protected ceiling {:.6}",
                a.action,
                cpc,
                ceiling_usd_per_credit
            );
        }
    }
    Ok(())
}

pub fn verify_sku_margins(skus: &[SkuEconomics], cost_ceiling_usd_per_credit: f64) -> Result<()> {
    for sku in skus {
        let m = sku.calculate_margin(cost_ceiling_usd_per_credit);
        if m < 70.0 {
            bail!("SKU {} is below 70% margin (got {:.2}%)", sku.sku, m);
        }
    }
    Ok(())
}
