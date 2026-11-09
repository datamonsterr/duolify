use super::*;

#[test]
fn test_reject_wrong_stripe_price_amount() {
    let price = StripePrice {
        id: "price_try".into(),
        active: true,
        currency: "USD".into(),
        amount_minor: 400, // Expected 500
        recurring_type: "one_time".into(),
        interval: None,
        interval_count: None,
        environment: "test".into(),
    };
    let offer = Offer {
        sku: "pack_try".into(),
        currency: "USD".into(),
        amount_minor: 500,
    };
    let err = price.matches("test", &offer, "one_time").unwrap_err();
    assert!(err.to_string().contains("amount"));
}

#[test]
fn test_reject_wrong_stripe_price_currency() {
    let price = StripePrice {
        id: "price_try".into(),
        active: true,
        currency: "EUR".into(),
        amount_minor: 500,
        recurring_type: "one_time".into(),
        interval: None,
        interval_count: None,
        environment: "test".into(),
    };
    let offer = Offer {
        sku: "pack_try".into(),
        currency: "USD".into(),
        amount_minor: 500,
    };
    let err = price.matches("test", &offer, "one_time").unwrap_err();
    assert!(err.to_string().contains("currency"));
}

#[test]
fn test_reject_inactive_stripe_price() {
    let price = StripePrice {
        id: "price_try".into(),
        active: false,
        currency: "USD".into(),
        amount_minor: 500,
        recurring_type: "one_time".into(),
        interval: None,
        interval_count: None,
        environment: "test".into(),
    };
    let offer = Offer {
        sku: "pack_try".into(),
        currency: "USD".into(),
        amount_minor: 500,
    };
    let err = price.matches("test", &offer, "one_time").unwrap_err();
    assert!(err.to_string().contains("inactive"));
}

#[test]
fn test_reject_wrong_recurring_interval() {
    let price = StripePrice {
        id: "price_learn".into(),
        active: true,
        currency: "USD".into(),
        amount_minor: 999,
        recurring_type: "recurring".into(),
        interval: Some("year".into()), // Expected month
        interval_count: Some(1),
        environment: "test".into(),
    };
    let offer = Offer {
        sku: "learn_monthly".into(),
        currency: "USD".into(),
        amount_minor: 999,
    };
    let err = price.matches("test", &offer, "month").unwrap_err();
    assert!(err.to_string().contains("interval"));
}

#[test]
fn test_reject_uneconomic_sku_below_70_percent_margin() {
    // Pack: 1000 credits for $5.00
    // fee: 2.9% + $0.30, refund: 2%
    // cost ceiling: 0.0015
    let sku = SkuEconomics {
        sku: "pack_try_bad".into(),
        sale_price_usd: 5.0,
        credits: 1000,
        fee_rate: 0.029,
        fixed_fee_usd: 0.30,
        refund_rate: 0.02,
    };
    let err = verify_sku_margins(&[sku], 0.0015).unwrap_err();
    assert!(err.to_string().contains("below 70% margin"));
}

#[test]
fn test_action_cost_ceiling_override_rules() {
    let actions = vec![StandardActionCost {
        action: "outline".into(),
        credits: 10,
        protected_cost_usd: 0.08204880, // cost/credit = 0.0082 > 0.0015
    }];
    // Without override -> error
    let err = verify_action_cost_ceiling(&actions, 0.0015, None).unwrap_err();
    assert!(err.to_string().contains("exceeds protected ceiling"));

    // With override -> success
    assert!(verify_action_cost_ceiling(&actions, 0.0015, Some("operator override")).is_ok());
}

#[test]
fn test_exact_daily_allowance_sizing() {
    // 46 = 3*2 + 8*5 = 3*2 + 5*8
    assert!(verify_daily_allowance_sizing(46, 2, 5, 8).is_ok());
    // Inexact: 50
    assert!(verify_daily_allowance_sizing(50, 2, 5, 8).is_err());
}
